package compiler

import (
	"context"
	"ghi/internal/mojave"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func mojaveGit(t *testing.T, root string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull)
	if b, e := cmd.CombinedOutput(); e != nil {
		t.Fatalf("git %v: %v: %s", args, e, b)
	}
}
func mojaveLibrary(t *testing.T, source string) string {
	t.Helper()
	root := project(t, map[string]string{"library.ghi": source, "tests/broken.ghi": "deliberately invalid package test syntax"})
	mojaveGit(t, root, "init")
	mojaveGit(t, root, "add", ".")
	mojaveGit(t, root, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "-m", "library")
	mojaveGit(t, root, "tag", "v1.0.0")
	return root
}

func TestMojaveLockedLibraryBuildAndTamperRefusal(t *testing.T) {
	ctx := context.Background()
	library := mojaveLibrary(t, "namespace acme.lib\nfunc Message() string{return \"hello from package\"}")
	root := project(t, map[string]string{"main.ghi": `namespace main
import fmt "go:fmt"
import acme.lib
func main(){fmt.Println(lib.Message())}
`, "tests/broken.ghi": "deliberately invalid application test syntax"})
	if e := mojave.Add(ctx, root, "acme.lib", library, "v1.0.0"); e != nil {
		t.Fatal(e)
	}
	built, e := Build(ctx, Options{Dir: root})
	if e != nil {
		t.Fatal(e)
	}
	output, e := exec.Command(built.Executable).CombinedOutput()
	if e != nil || strings.TrimSpace(string(output)) != "hello from package" {
		t.Fatalf("package executable: %v %s", e, output)
	}
	before, e := os.ReadFile(built.Executable)
	if e != nil {
		t.Fatal(e)
	}
	roots, e := mojave.SourceRoots(root)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(roots[0].Path, "library.ghi"), []byte("namespace acme.lib\nfunc Message() string{return \"tampered\"}"), 0644); e != nil {
		t.Fatal(e)
	}
	if _, e = Build(ctx, Options{Dir: root}); e == nil || !strings.Contains(e.Error(), "integrity") {
		t.Fatalf("compiler accepted tampered package: %v", e)
	}
	after, e := os.ReadFile(built.Executable)
	if e != nil || string(before) != string(after) {
		t.Fatal("failed package verification replaced existing executable")
	}
}

func TestMojaveRejectsNamespaceEscapeAndProjectCollision(t *testing.T) {
	for _, ns := range []string{"main", "other", "acme.lib"} {
		t.Run(ns, func(t *testing.T) {
			library := mojaveLibrary(t, "namespace "+ns+"\nfunc Value() int{return 1}")
			files := map[string]string{"main.ghi": "namespace main\nfunc main(){}"}
			if ns == "acme.lib" {
				files["local/local.ghi"] = "namespace acme.lib\nfunc Other(){}"
			}
			root := project(t, files)
			if e := mojave.Add(context.Background(), root, "acme.lib", library, "v1.0.0"); e != nil {
				t.Fatal(e)
			}
			if _, e := Build(context.Background(), Options{Dir: root}); e == nil || !strings.Contains(e.Error(), "namespace") {
				t.Fatalf("accepted namespace escape/collision: %v", e)
			}
		})
	}
}
