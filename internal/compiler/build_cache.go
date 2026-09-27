package compiler

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"ghi/internal/toolchain"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"sync"
)

type buildCache struct {
	dir, workspace, key string
	release             func()
}

type buildCacheState struct {
	Key              string
	Files            map[string]string
	Namespaces       map[string]string
	Helpers          []int
	SpecializedNames map[string]string
}

// The OS releases the nonblocking lock even if the compiler is killed. Busy,
// unavailable or read-only caches fall back to the normal temporary workspace.
func openBuildCache(root string) *buildCache {
	dir := root
	for _, part := range []string{".ghi", "build", "work"} {
		dir = filepath.Join(dir, part)
		if err := os.Mkdir(dir, 0700); err != nil && !os.IsExist(err) {
			return nil
		}
		info, err := os.Lstat(dir)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return nil
		}
	}
	lock := filepath.Join(filepath.Dir(dir), "lock")
	if info, err := os.Lstat(lock); err == nil && !info.Mode().IsRegular() {
		return nil
	}
	release, err := lockBuildCache(lock)
	if err != nil {
		return nil
	}
	return &buildCache{dir: filepath.Dir(dir), workspace: dir, release: release}
}

var compilerCacheIdentity = sync.OnceValue(func() string {
	path, err := os.Executable()
	if err != nil {
		return ""
	}
	file, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer file.Close()
	h := sha256.New()
	if _, err := io.Copy(h, file); err != nil {
		return ""
	}
	return hex.EncodeToString(h.Sum(nil))
})

func (p *program) buildCacheKey(ctx context.Context, workspace, goPath string) string {
	identity := compilerCacheIdentity()
	if identity == "" {
		return ""
	}
	command := exec.CommandContext(ctx, goPath, "env", "-json")
	command.Dir, command.Env = workspace, toolchain.Env()
	output, err := command.Output()
	if err != nil {
		return ""
	}
	var environment map[string]string
	if json.Unmarshal(output, &environment) != nil {
		return ""
	}
	// This is the staging path, not an input. All other resolved Go settings
	// (including GOFLAGS, target, experiments and configured GOENV) participate.
	delete(environment, "GOMOD")
	// Derived from the settings below; includes a fresh Go temporary directory
	// in -ffile-prefix-map on every invocation of go env.
	delete(environment, "GOGCCFLAGS")
	loader := &exportLoader{ctx: ctx, dir: workspace, goPath: goPath, exports: map[string]string{}}
	if loader.prefetch(p) != nil {
		return ""
	}
	p.Exports = loader.exports
	h := sha256.New()
	encoder := json.NewEncoder(h)
	encoder.Encode([]string{"ghi-build-cache-v1", identity, p.Root, goPath})
	encoder.Encode(environment)
	env := toolchain.Env()
	sort.Strings(env)
	encoder.Encode(env)
	// Go validates its own build cache. Export artifact identities also cover
	// local replace modules and build constraints; a changed API invalidates Ghi.
	encoder.Encode(loader.exports)
	for _, name := range []string{"mojave.json", "mojave.lock", "go.mod", "go.sum"} {
		data, err := os.ReadFile(filepath.Join(p.Root, name))
		if err != nil && !os.IsNotExist(err) {
			return ""
		}
		encoder.Encode([]any{name, err == nil, data})
	}
	for _, name := range []string{"go.mod", "go.sum"} {
		data, err := os.ReadFile(filepath.Join(workspace, name))
		if err != nil && !os.IsNotExist(err) {
			return ""
		}
		encoder.Encode([]any{name, err == nil, data})
	}
	if ctx.Err() != nil {
		return ""
	}
	p.CacheEnvironment = hex.EncodeToString(h.Sum(nil))
	for _, ns := range p.Ordered {
		for _, file := range ns.Files {
			encoder.Encode([]string{file.Path, string(file.Source)})
		}
	}
	return hex.EncodeToString(h.Sum(nil))
}

func cacheFiles(dir string) (map[string]string, error) {
	files := map[string]string{}
	err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlink in build cache: %s", path)
		}
		if entry.IsDir() {
			return nil
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("non-file in build cache: %s", path)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		digest := sha256.Sum256(data)
		files[rel] = hex.EncodeToString(digest[:])
		return nil
	})
	return files, err
}

func (c *buildCache) valid() bool {
	state := c.snapshot()
	return state != nil && state.Key == c.key
}

func (c *buildCache) snapshot() *buildCacheState {
	data, err := os.ReadFile(filepath.Join(c.dir, "state.json"))
	if err != nil {
		return nil
	}
	var state buildCacheState
	if json.Unmarshal(data, &state) != nil || len(state.Files) == 0 {
		return nil
	}
	files, err := cacheFiles(c.workspace)
	if err != nil || len(files) != len(state.Files) {
		return nil
	}
	for name, digest := range files {
		if state.Files[name] != digest {
			return nil
		}
	}
	return &state
}

// Publish only after semantic validation. Keep stable filenames and unchanged
// bytes/mtimes so Go can reuse unaffected packages. Invalidate before writing;
// an interrupted sync can never be mistaken for a complete cached generation.
func (c *buildCache) store(staging string, p *program) error {
	files, err := cacheFiles(staging)
	if err != nil {
		return err
	}
	old, err := cacheFiles(c.workspace)
	if err != nil {
		return err
	}
	statePath := filepath.Join(c.dir, "state.json")
	if err := os.Remove(statePath); err != nil && !os.IsNotExist(err) {
		return err
	}
	for name := range old {
		if _, exists := files[name]; !exists {
			if err := os.Remove(filepath.Join(c.workspace, name)); err != nil {
				return err
			}
		}
	}
	for name, digest := range files {
		if old[name] == digest {
			continue
		}
		data, err := os.ReadFile(filepath.Join(staging, name))
		if err != nil {
			return err
		}
		target := filepath.Join(c.workspace, name)
		if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
			return err
		}
		if err := os.WriteFile(target, data, 0600); err != nil {
			return err
		}
	}
	state := buildCacheState{Key: c.key, Files: files, SpecializedNames: p.SpecializedNames}
	if p.Semantic != nil {
		state.Namespaces = p.Semantic.keys
	}
	for count := range p.Helpers {
		state.Helpers = append(state.Helpers, count)
	}
	sort.Ints(state.Helpers)
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	// Verify the snapshot again: source synchronization or disk failures must
	// not publish a mismatched manifest.
	actual, err := cacheFiles(c.workspace)
	if err != nil {
		return err
	}
	want, _ := json.Marshal(files)
	got, _ := json.Marshal(actual)
	if !bytes.Equal(want, got) {
		return fmt.Errorf("build cache changed during generation")
	}
	return os.WriteFile(statePath, data, 0600)
}
