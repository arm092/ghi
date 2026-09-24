package mojave

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
)

type proxyFixture struct {
	mu       sync.Mutex
	files    map[string][]byte
	versions map[string][]string
}

func (p *proxyFixture) add(t *testing.T, path, version, requirements string) {
	t.Helper()
	mod := "module " + path + "\n\ngo 1.26.0\n" + requirements
	var archive bytes.Buffer
	z := zip.NewWriter(&archive)
	for name, value := range map[string]string{"go.mod": mod, "library.go": "package library\nfunc Value() string{return \"locked\"}\n"} {
		w, e := z.Create(path + "@" + version + "/" + name)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = w.Write([]byte(value)); e != nil {
			t.Fatal(e)
		}
	}
	if e := z.Close(); e != nil {
		t.Fatal(e)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	prefix := "/" + path + "/@v/" + version
	p.files[prefix+".mod"] = []byte(mod)
	p.files[prefix+".zip"] = archive.Bytes()
	p.files[prefix+".info"] = []byte(fmt.Sprintf(`{"Version":%q,"Time":"2026-01-01T00:00:00Z"}`, version))
	p.versions[path] = append(p.versions[path], version)
	sort.Strings(p.versions[path])
	p.files["/"+path+"/@v/list"] = []byte(strings.Join(p.versions[path], "\n") + "\n")
	p.files["/"+path+"/@latest"] = p.files[prefix+".info"]
}
func nativeProxy(t *testing.T) *proxyFixture {
	t.Helper()
	p := &proxyFixture{files: map[string][]byte{}, versions: map[string][]string{}}
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p.mu.Lock()
		defer p.mu.Unlock()
		if b, ok := p.files[r.URL.Path]; ok {
			w.Write(b)
		} else {
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(s.Close)
	t.Setenv("GOPROXY", s.URL)
	t.Setenv("GOSUMDB", "off")
	t.Setenv("GOFLAGS", "-modcacherw")
	t.Setenv("GOMODCACHE", t.TempDir())
	return p
}
func TestGoDependenciesLockMVSAndOfflineFiles(t *testing.T) {
	p := nativeProxy(t)
	p.add(t, "example.test/base", "v1.0.0", "")
	p.add(t, "example.test/base", "v1.2.0", "")
	p.add(t, "example.test/first", "v1.0.0", "require example.test/base v1.0.0\n")
	p.add(t, "example.test/second", "v1.0.0", "require example.test/base v1.2.0\n")
	root := t.TempDir()
	ctx := context.Background()
	if e := AddGo(ctx, root, "example.test/first", "v1.0.0"); e != nil {
		t.Fatal(e)
	}
	if e := AddGo(ctx, root, "example.test/second", "v1.0.0"); e != nil {
		t.Fatal(e)
	}
	mod, sum, found, e := GoModuleFiles(root)
	if e != nil || !found {
		t.Fatalf("native files %v %v", found, e)
	}
	if !strings.Contains(string(mod), "example.test/base v1.2.0") || !strings.Contains(string(sum), "example.test/base v1.2.0 h1:") {
		t.Fatalf("MVS/checksums absent: %s\n%s", mod, sum)
	}
	before, _ := os.ReadFile(filepath.Join(root, "mojave.lock"))
	if e = Install(ctx, root); e != nil {
		t.Fatal(e)
	}
	after, _ := os.ReadFile(filepath.Join(root, "mojave.lock"))
	if !bytes.Equal(before, after) {
		t.Fatal("install changed native lock")
	}
	t.Setenv("GOPROXY", "off")
	modAgain, sumAgain, _, e := GoModuleFiles(root)
	if e != nil || !bytes.Equal(mod, modAgain) || !bytes.Equal(sum, sumAgain) {
		t.Fatal("locked generation is not deterministic/offline", e)
	}
	if e = RemoveGo(ctx, root, "example.test/second"); e != nil {
		t.Fatal(e)
	}
	mod, _, _, e = GoModuleFiles(root)
	if e != nil || strings.Contains(string(mod), "example.test/second") {
		t.Fatal("removed module remained in graph", e)
	}
	m, e := readManifest(root)
	if e != nil {
		t.Fatal(e)
	}
	m.GoDependencies["example.test/first"] = "v9.0.0"
	if e = writeJSON(filepath.Join(root, "mojave.json"), m); e != nil {
		t.Fatal(e)
	}
	if _, _, _, e = GoModuleFiles(root); e == nil {
		t.Fatal("manifest mismatch accepted")
	}
}
func TestNativeRequirementsFromGitLibrary(t *testing.T) {
	p := nativeProxy(t)
	p.add(t, "example.test/base", "v1.0.0", "")
	p.add(t, "example.test/base", "v1.2.0", "")
	library := repo(t, "library", nil)
	if e := writeJSON(filepath.Join(library, "mojave.json"), Manifest{Version: 1, GoDependencies: map[string]string{"example.test/base": "v1.2.0"}}); e != nil {
		t.Fatal(e)
	}
	commit(t, library)
	root := t.TempDir()
	if e := AddGo(context.Background(), root, "example.test/base", "v1.0.0"); e != nil {
		t.Fatal(e)
	}
	if e := Add(context.Background(), root, "library", library, "HEAD"); e != nil {
		t.Fatal(e)
	}
	mod, _, found, e := GoModuleFiles(root)
	if e != nil || !found || !strings.Contains(string(mod), "example.test/base v1.2.0") {
		t.Fatalf("library native requirements ignored: %s %v", mod, e)
	}
	if e = RemoveGo(context.Background(), root, "example.test/base"); e != nil {
		t.Fatal(e)
	}
	if _, _, found, e = GoModuleFiles(root); e != nil || !found {
		t.Fatalf("transitive-only native graph lost: %v %v", found, e)
	}
}
func TestGoLockIntegrityAndDownloadChecksum(t *testing.T) {
	p := nativeProxy(t)
	p.add(t, "example.test/base", "v1.0.0", "")
	root := t.TempDir()
	if e := AddGo(context.Background(), root, "example.test/base", "v1.0.0"); e != nil {
		t.Fatal(e)
	}
	var lock Lock
	if e := readJSON(filepath.Join(root, "mojave.lock"), &lock); e != nil {
		t.Fatal(e)
	}
	original := lock.Go.Modules[0].Sum
	lock.Go.Modules[0].Sum = "h1:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="
	if e := writeJSON(filepath.Join(root, "mojave.lock"), lock); e != nil {
		t.Fatal(e)
	}
	if _, _, _, e := GoModuleFiles(root); e == nil {
		t.Fatal("tampered checksum accepted")
	}
	lock.Go.Modules[0].Sum = original
	if e := writeJSON(filepath.Join(root, "mojave.lock"), lock); e != nil {
		t.Fatal(e)
	}
	// Serve a different archive for the pinned version, forcing actual Go checksum verification.
	p.add(t, "example.test/base", "v1.0.0", "// altered module\n")
	t.Setenv("GOMODCACHE", t.TempDir())
	if e := Install(context.Background(), root); e == nil || !strings.Contains(e.Error(), "checksum mismatch") {
		t.Fatalf("bad archive accepted: %v", e)
	}
}
func TestGoLatestChangesOnlyOnUpdate(t *testing.T) {
	p := nativeProxy(t)
	p.add(t, "example.test/base", "v1.0.0", "")
	root := t.TempDir()
	ctx := context.Background()
	if e := AddGo(ctx, root, "example.test/base", "latest"); e != nil {
		t.Fatal(e)
	}
	p.add(t, "example.test/base", "v1.1.0", "")
	if e := Install(ctx, root); e != nil {
		t.Fatal(e)
	}
	mod, _, _, _ := GoModuleFiles(root)
	if strings.Contains(string(mod), "v1.1.0") {
		t.Fatal("install followed latest")
	}
	if e := Update(ctx, root); e != nil {
		t.Fatal(e)
	}
	mod, _, _, e := GoModuleFiles(root)
	if e != nil || !strings.Contains(string(mod), "v1.1.0") {
		t.Fatalf("update did not resolve latest: %s %v", mod, e)
	}
}

func TestGoPrunedGraphRemainsStableWhenFlattened(t *testing.T) {
	p := nativeProxy(t)
	p.add(t, "example.test/fourth", "v1.0.0", "")
	p.add(t, "example.test/third", "v1.0.0", "require example.test/fourth v1.0.0\n")
	p.add(t, "example.test/second", "v1.0.0", "require example.test/third v1.0.0\n")
	p.add(t, "example.test/first", "v1.0.0", "require example.test/second v1.0.0\n")
	root := t.TempDir()
	ctx := context.Background()
	if e := AddGo(ctx, root, "example.test/first", "v1.0.0"); e != nil {
		t.Fatal(e)
	}
	before, e := os.ReadFile(filepath.Join(root, "mojave.lock"))
	if e != nil {
		t.Fatal(e)
	}
	if e = Install(ctx, root); e != nil {
		t.Fatalf("newly resolved graph cannot be installed: %v", e)
	}
	after, e := os.ReadFile(filepath.Join(root, "mojave.lock"))
	if e != nil || !bytes.Equal(before, after) {
		t.Fatal("install changed lock", e)
	}
	mod, _, _, e := GoModuleFiles(root)
	if e != nil || !strings.Contains(string(mod), "example.test/fourth v1.0.0") {
		t.Fatalf("flattened transitive graph incomplete: %s %v", mod, e)
	}
}
