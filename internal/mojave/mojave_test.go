package mojave

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func gitTest(t *testing.T, dir string, args ...string) string {
	t.Helper()
	c := exec.Command("git", args...)
	c.Dir = dir
	c.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1")
	b, e := c.CombinedOutput()
	if e != nil {
		t.Fatalf("git %v: %v: %s", args, e, b)
	}
	return strings.TrimSpace(string(b))
}
func put(t *testing.T, dir, name, value string) {
	t.Helper()
	if e := os.WriteFile(filepath.Join(dir, name), []byte(value), 0644); e != nil {
		t.Fatal(e)
	}
}
func repo(t *testing.T, namespace string, deps map[string]Dependency) string {
	t.Helper()
	d := t.TempDir()
	gitTest(t, d, "init")
	put(t, d, "lib.ghi", "namespace "+namespace+";\n")
	if deps != nil {
		b, _ := json.Marshal(Manifest{Version: 1, Dependencies: deps})
		put(t, d, "mojave.json", string(b))
	}
	commit(t, d)
	gitTest(t, d, "tag", "v1.0.0")
	return d
}
func commit(t *testing.T, d string) string {
	t.Helper()
	gitTest(t, d, "add", ".")
	gitTest(t, d, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "-m", "fixture")
	return gitTest(t, d, "rev-parse", "HEAD")
}

// A moved upstream branch must never change an installed locked dependency.
func TestLockedInstallAndExplicitUpdate(t *testing.T) {
	ctx := context.Background()
	p := t.TempDir()
	r := repo(t, "math", nil)
	if e := Add(ctx, p, "math", r, "HEAD"); e != nil {
		t.Fatal(e)
	}
	lockBefore, e := os.ReadFile(filepath.Join(p, "mojave.lock"))
	if e != nil {
		t.Fatal(e)
	}
	put(t, r, "lib.ghi", "namespace math;\n// next version\n")
	commit(t, r)
	if e = Install(ctx, p); e != nil {
		t.Fatal(e)
	}
	lockAfter, _ := os.ReadFile(filepath.Join(p, "mojave.lock"))
	if string(lockAfter) != string(lockBefore) {
		t.Fatal("install updated lock")
	}
	if e = os.RemoveAll(filepath.Join(p, ".ghi", "packages")); e != nil {
		t.Fatal(e)
	}
	if e = Install(ctx, p); e != nil {
		t.Fatal(e)
	}
	roots, e := SourceRoots(p)
	if e != nil || len(roots) != 1 {
		t.Fatalf("roots %v: %v", roots, e)
	}
	data, _ := os.ReadFile(filepath.Join(roots[0].Path, "lib.ghi"))
	if strings.Contains(string(data), "next version") {
		t.Fatal("install followed branch instead of lock")
	}
	if e = Update(ctx, p); e != nil {
		t.Fatal(e)
	}
	data, _ = os.ReadFile(filepath.Join(roots[0].Path, "lib.ghi"))
	if !strings.Contains(string(data), "next version") {
		t.Fatal("explicit update did not update")
	}
	put(t, roots[0].Path, "lib.ghi", "tampered")
	if _, e = SourceRoots(p); e == nil || !strings.Contains(e.Error(), "integrity") {
		t.Fatalf("tamper accepted: %v", e)
	}
}

func TestTransitiveTagsConflictAndRemove(t *testing.T) {
	ctx := context.Background()
	p := t.TempDir()
	leaf := repo(t, "leaf", nil)
	parent := repo(t, "parent", map[string]Dependency{"leaf": {Repository: leaf, Ref: "v1.0.0"}})
	if e := Add(ctx, p, "parent", parent, "v1.0.0"); e != nil {
		t.Fatal(e)
	}
	roots, e := SourceRoots(p)
	if e != nil || len(roots) != 2 || roots[0].Namespace != "leaf" {
		t.Fatalf("transitive roots %v %v", roots, e)
	}
	if e = Add(ctx, p, "leaf", leaf, "HEAD"); e == nil || !strings.Contains(e.Error(), "conflict") {
		t.Fatalf("expected namespace conflict: %v", e)
	}
	roots, e = SourceRoots(p)
	if e != nil || len(roots) != 2 {
		t.Fatalf("failed add damaged installation: %v %v", roots, e)
	}
	if e = Remove(ctx, p, "parent"); e != nil {
		t.Fatal(e)
	}
	roots, e = SourceRoots(p)
	if e != nil || len(roots) != 0 {
		t.Fatalf("remove left dependencies: %v %v", roots, e)
	}
}

func TestInvalidInputsAndCancellation(t *testing.T) {
	p := t.TempDir()
	if roots, e := SourceRoots(p); e != nil || len(roots) != 0 {
		t.Fatal(roots, e)
	}
	for _, n := range []string{"../escape", "main", "ghi.net", "a/b", "A..B"} {
		if e := Add(context.Background(), p, n, "/missing", "HEAD"); e == nil {
			t.Fatalf("accepted namespace %q", n)
		}
	}
	for _, r := range []string{"--upload-pack=evil", "ext::evil"} {
		if e := Add(context.Background(), p, "valid", r, "HEAD"); e == nil {
			t.Fatalf("accepted repository %q", r)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if e := Add(ctx, p, "valid", "/missing", "HEAD"); e == nil {
		t.Fatal("ignored cancellation")
	}
}

func TestLockMismatchAndMissingCacheIntegrity(t *testing.T) {
	ctx := context.Background()
	p := t.TempDir()
	r := repo(t, "lib", nil)
	if e := Add(ctx, p, "lib", r, "v1.0.0"); e != nil {
		t.Fatal(e)
	}
	var l Lock
	if e := readJSON(filepath.Join(p, "mojave.lock"), &l); e != nil {
		t.Fatal(e)
	}
	l.Packages[0].Integrity = strings.Repeat("0", 64)
	b, _ := json.Marshal(l)
	put(t, p, "mojave.lock", string(b))
	if e := os.RemoveAll(filepath.Join(p, ".ghi", "packages")); e != nil {
		t.Fatal(e)
	}
	if e := Install(ctx, p); e == nil || !strings.Contains(e.Error(), "integrity") {
		t.Fatalf("download integrity mismatch accepted: %v", e)
	}
	put(t, p, "mojave.json", `{"version":1,"dependencies":{"lib":{"repository":"missing","ref":"HEAD"}}}`)
	if e := Install(ctx, p); e == nil || !strings.Contains(e.Error(), "does not match") {
		t.Fatalf("install resolved changed manifest silently: %v", e)
	}
}

func TestRejectsGitSymlinkWithoutChangingProject(t *testing.T) {
	ctx := context.Background()
	p := t.TempDir()
	r := repo(t, "lib", nil)
	put(t, r, "link.txt", "../../outside")
	blob := gitTest(t, r, "hash-object", "-w", "link.txt")
	gitTest(t, r, "update-index", "--add", "--cacheinfo", "120000,"+blob+",escape")
	gitTest(t, r, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "-m", "symlink")
	if e := Add(ctx, p, "lib", r, "HEAD"); e == nil || !strings.Contains(e.Error(), "symlinks") {
		t.Fatalf("Git symlink accepted: %v", e)
	}
	if _, e := os.Stat(filepath.Join(p, "mojave.json")); !os.IsNotExist(e) {
		t.Fatal("failed add changed manifest")
	}
}

func TestRejectsPortablePathCollision(t *testing.T) {
	r := repo(t, "lib", nil)
	blob := gitTest(t, r, "hash-object", "lib.ghi")
	gitTest(t, r, "-c", "core.protectNTFS=false", "update-index", "--add", "--cacheinfo", "100644,"+blob+",NUL.ghi")
	gitTest(t, r, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "-m", "reserved file")
	if e := Add(context.Background(), t.TempDir(), "lib", r, "HEAD"); e == nil {
		t.Fatalf("reserved archive filename accepted: %v", e)
	}
}
