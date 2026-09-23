// Package toolchain locates or securely installs a Go toolchain for Ghi.
package toolchain

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"go/version"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

const MinimumVersion = "go1.26.0"

type Archive struct {
	Filename string `json:"filename"`
	OS       string `json:"os"`
	Arch     string `json:"arch"`
	Kind     string `json:"kind"`
	SHA256   string `json:"sha256"`
	Size     int64  `json:"size"`
}
type Release struct {
	Version string    `json:"version"`
	Stable  bool      `json:"stable"`
	Files   []Archive `json:"files"`
}

type Manager struct {
	CacheDir    string
	MetadataURL string
	DownloadURL string
	SkipSystem  bool
	Log         io.Writer
	Client      *http.Client
}

// Env prevents a stale GOROOT or workspace from redirecting the managed toolchain.
func Env() []string {
	env := []string{}
	for _, value := range os.Environ() {
		key, _, _ := strings.Cut(value, "=")
		switch strings.ToUpper(key) {
		case "GOROOT", "GOTOOLCHAIN", "GOWORK", "GO111MODULE":
			continue
		}
		env = append(env, value)
	}
	return append(env, "GOTOOLCHAIN=local", "GOWORK=off", "GO111MODULE=on")
}

func compatible(ctx context.Context, path string) bool {
	probe, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	command := exec.CommandContext(probe, path, "version")
	command.Env = Env()
	out, err := command.Output()
	if err != nil {
		return false
	}
	words := strings.Fields(string(out))
	return len(words) >= 4 && words[0] == "go" && words[1] == "version" && version.IsValid(words[2]) && version.Compare(words[2], MinimumVersion) >= 0 && words[3] == runtime.GOOS+"/"+runtime.GOARCH
}

func (m Manager) Ensure(ctx context.Context) (string, error) {
	if !m.SkipSystem {
		if override := os.Getenv("GHI_GO"); override != "" {
			absolute, err := filepath.Abs(override)
			if err != nil {
				return "", err
			}
			if !compatible(ctx, absolute) {
				return "", fmt.Errorf("GHI_GO does not identify a compatible Go executable (need %s or newer)", MinimumVersion)
			}
			return absolute, nil
		}
		if path, err := exec.LookPath("go"); err == nil && compatible(ctx, path) {
			return path, nil
		}
	}
	if m.CacheDir == "" {
		cache, err := os.UserCacheDir()
		if err != nil {
			return "", err
		}
		m.CacheDir = filepath.Join(cache, "ghi", "toolchains", runtime.GOOS+"-"+runtime.GOARCH)
	}
	if m.MetadataURL == "" {
		m.MetadataURL = "https://go.dev/dl/?mode=json"
	}
	if m.DownloadURL == "" {
		m.DownloadURL = "https://go.dev/dl/"
	}
	if m.Client == nil {
		m.Client = &http.Client{Timeout: 5 * time.Minute}
	}
	executable := "go"
	if runtime.GOOS == "windows" {
		executable += ".exe"
	}
	entries, _ := os.ReadDir(m.CacheDir)
	sort.Slice(entries, func(i, j int) bool { return version.Compare(entries[i].Name(), entries[j].Name()) > 0 })
	for _, entry := range entries {
		if !entry.IsDir() || !version.IsValid(entry.Name()) {
			continue
		}
		candidate := filepath.Join(m.CacheDir, entry.Name(), "go", "bin", executable)
		if compatible(ctx, candidate) {
			return candidate, nil
		}
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	response, err := m.get(ctx, m.MetadataURL)
	if err != nil {
		return "", err
	}
	var releases []Release
	err = json.NewDecoder(io.LimitReader(response.Body, 8<<20)).Decode(&releases)
	response.Body.Close()
	if err != nil {
		return "", fmt.Errorf("read Go release metadata: %w", err)
	}
	sort.Slice(releases, func(i, j int) bool { return version.Compare(releases[i].Version, releases[j].Version) > 0 })
	var selected *Archive
	releaseVersion := ""
	for _, release := range releases {
		if !release.Stable || !version.IsValid(release.Version) || version.Compare(release.Version, MinimumVersion) < 0 {
			continue
		}
		for _, archive := range release.Files {
			if archive.Kind == "archive" && archive.OS == runtime.GOOS && archive.Arch == runtime.GOARCH {
				copy := archive
				selected = &copy
				releaseVersion = release.Version
				break
			}
		}
		if selected != nil {
			break
		}
	}
	if selected == nil {
		return "", fmt.Errorf("no compatible official Go archive for %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	if !filepath.IsLocal(selected.Filename) || strings.ContainsAny(selected.Filename, "/\\:") {
		return "", fmt.Errorf("invalid archive filename in Go metadata")
	}
	expected, err := hex.DecodeString(selected.SHA256)
	if err != nil || len(expected) != sha256.Size {
		return "", fmt.Errorf("invalid Go archive checksum metadata")
	}
	if selected.Size <= 0 || selected.Size > 512<<20 {
		return "", fmt.Errorf("invalid Go archive size")
	}
	if err := os.MkdirAll(m.CacheDir, 0755); err != nil {
		return "", err
	}
	staging, err := os.MkdirTemp(m.CacheDir, ".install-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(staging)
	if m.Log != nil {
		fmt.Fprintf(m.Log, "Installing %s for %s/%s...\n", releaseVersion, runtime.GOOS, runtime.GOARCH)
	}
	response, err = m.get(ctx, m.DownloadURL+selected.Filename)
	if err != nil {
		return "", err
	}
	archivePath := filepath.Join(staging, selected.Filename)
	output, err := os.OpenFile(archivePath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		response.Body.Close()
		return "", err
	}
	hash := sha256.New()
	n, copyErr := io.Copy(io.MultiWriter(output, hash), io.LimitReader(response.Body, selected.Size+1))
	closeErr := output.Close()
	response.Body.Close()
	if copyErr != nil {
		return "", copyErr
	}
	if closeErr != nil {
		return "", closeErr
	}
	if n != selected.Size || !strings.EqualFold(hex.EncodeToString(hash.Sum(nil)), selected.SHA256) {
		return "", fmt.Errorf("Go archive size or SHA-256 checksum mismatch")
	}
	unpacked := filepath.Join(staging, "unpacked")
	if err := extract(archivePath, unpacked); err != nil {
		return "", fmt.Errorf("extract Go: %w", err)
	}
	candidate := filepath.Join(unpacked, "go", "bin", executable)
	if !compatible(ctx, candidate) {
		return "", fmt.Errorf("downloaded Go executable failed version/platform validation")
	}
	destination := filepath.Join(m.CacheDir, releaseVersion)
	if err := os.Rename(unpacked, destination); err != nil {
		// A concurrent installer may have completed this exact version.
		existing := filepath.Join(destination, "go", "bin", executable)
		if compatible(ctx, existing) {
			return existing, nil
		}
		return "", fmt.Errorf("install Go: %w", err)
	}
	return filepath.Join(destination, "go", "bin", executable), nil
}

func (m Manager) get(ctx context.Context, url string) (*http.Response, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	response, err := m.Client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("download Go: %w", err)
	}
	if response.StatusCode != http.StatusOK {
		response.Body.Close()
		return nil, fmt.Errorf("download Go: HTTP %s", response.Status)
	}
	return response, nil
}
