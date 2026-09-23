package toolchain

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"go/version"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

func TestToolchainMustMatchCompilerExportFormat(t *testing.T) {
	minor := version.Lang(runtime.Version())
	if !supportedVersion(minor + ".3") {
		t.Fatal("compiler's own stable Go branch rejected")
	}
	if supportedVersion("go9.99.0") {
		t.Fatal("future Go export format accepted")
	}
}

func zipData(t *testing.T, name string, data []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	h := &zip.FileHeader{Name: name, Method: zip.Deflate}
	h.SetMode(0755)
	f, err := w.CreateHeader(h)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.Write(data); err != nil {
		t.Fatal(err)
	}
	if err = w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestMissingToolchainDownloadVerifyAndReuseOffline(t *testing.T) {
	// A small real native executable makes the version probe exercise process
	// execution on both Windows and macOS rather than accepting a marker file.
	dir := t.TempDir()
	src := filepath.Join(dir, "probe.go")
	if err := os.WriteFile(src, []byte("package main\nimport \"fmt\"\nfunc main(){fmt.Println(\"go version go1.26.3 "+runtime.GOOS+"/"+runtime.GOARCH+"\")}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(dir, "probe")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	if out, err := exec.Command("go", "build", "-o", binary, src).CombinedOutput(); err != nil {
		t.Fatalf("fixture: %v %s", err, out)
	}
	data, err := os.ReadFile(binary)
	if err != nil {
		t.Fatal(err)
	}
	exe := "go"
	if runtime.GOOS == "windows" {
		exe += ".exe"
	}
	archive := zipData(t, "go/bin/"+exe, data)
	checksum := fmt.Sprintf("%x", sha256.Sum256(archive))
	var server *httptest.Server
	downloads := 0
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/releases" {
			json.NewEncoder(w).Encode([]Release{{Version: "go1.26.3", Stable: true, Files: []Archive{{Filename: "go1.26.3.zip", OS: runtime.GOOS, Arch: runtime.GOARCH, Kind: "archive", SHA256: checksum, Size: int64(len(archive))}}}})
		} else {
			downloads++
			w.Write(archive)
		}
	}))
	defer server.Close()
	m := Manager{CacheDir: filepath.Join(dir, "cache"), MetadataURL: server.URL + "/releases", DownloadURL: server.URL + "/", SkipSystem: true}
	path, err := m.Ensure(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(path) != exe || downloads != 1 {
		t.Fatalf("result %s, downloads %d", path, downloads)
	}
	server.Close()
	reused, err := m.Ensure(context.Background())
	if err != nil || reused != path {
		t.Fatalf("offline reuse: %s, %v", reused, err)
	}
}

func TestRejectChecksumMismatch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/releases" {
			json.NewEncoder(w).Encode([]Release{{Version: "go1.26.3", Stable: true, Files: []Archive{{Filename: "go.zip", OS: runtime.GOOS, Arch: runtime.GOARCH, Kind: "archive", SHA256: fmt.Sprintf("%064d", 0), Size: 3}}}})
		} else {
			w.Write([]byte("bad"))
		}
	}))
	defer server.Close()
	_, err := (&Manager{CacheDir: t.TempDir(), MetadataURL: server.URL + "/releases", DownloadURL: server.URL + "/", SkipSystem: true}).Ensure(context.Background())
	if err == nil {
		t.Fatal("unverified archive accepted")
	}
}

func TestArchiveCannotEscapeDestination(t *testing.T) {
	for _, name := range []string{"go/../../escaped", "go/../escaped", "go/C:/escaped", "go/..\\escaped"} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			archive := filepath.Join(dir, "input.zip")
			os.WriteFile(archive, zipData(t, name, []byte("bad")), 0644)
			if err := extract(archive, filepath.Join(dir, "out")); err == nil {
				t.Fatal("unsafe archive accepted")
			}
		})
	}
}

func TestExtractMacOSArchive(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "input.tar.gz")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	tw.WriteHeader(&tar.Header{Name: "go/bin/go", Mode: 0755, Size: 4, Typeflag: tar.TypeReg})
	tw.Write([]byte("test"))
	tw.Close()
	gz.Close()
	f.Close()
	if err := extract(path, filepath.Join(dir, "out")); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "out", "go", "bin", "go"))
	if err != nil || string(data) != "test" {
		t.Fatalf("extraction: %q %v", data, err)
	}
}
