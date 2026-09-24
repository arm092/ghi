package cli_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestProjectCLI(t *testing.T) {
	binaries := t.TempDir()
	for _, name := range []string{"ghi"} {
		binary := filepath.Join(binaries, name)
		if runtime.GOOS == "windows" {
			binary += ".exe"
		}
		c := exec.Command("go", "build", "-ldflags=-X main.version=v0.2.1", "-o", binary, "../../cmd/"+name)
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("build: %v\n%s", err, out)
		}
		invoke := func(dir string, args ...string) (string, error) {
			c := exec.Command(binary, args...)
			c.Dir = dir
			b, err := c.CombinedOutput()
			return string(b), err
		}
		empty := t.TempDir()
		for _, flag := range []string{"version", "--version", "-v", "-V"} {
			out, err := invoke(empty, flag)
			if err != nil || strings.TrimSpace(out) != name+" v0.2.1" {
				t.Fatalf("%s %s: %v %q", name, flag, err, out)
			}
		}
		project := filepath.Join(empty, "new project")
		if out, err := invoke(empty, "init", project); err != nil {
			t.Fatalf("init: %v %s", err, out)
		}
		data, err := os.ReadFile(filepath.Join(project, "mojave.json"))
		if err != nil {
			t.Fatal(err)
		}
		var manifest struct {
			Version      int            `json:"version"`
			Dependencies map[string]any `json:"dependencies"`
		}
		if err := json.Unmarshal(data, &manifest); err != nil || manifest.Version != 1 || manifest.Dependencies == nil {
			t.Fatalf("invalid manifest: %s", data)
		}
		if info, err := os.Stat(filepath.Join(project, "tests")); err != nil || !info.IsDir() {
			t.Fatal("missing separate tests directory")
		}
		if out, err := invoke(project, "run"); err != nil || strings.TrimSpace(out) != "Hello from Ghi!" {
			t.Fatalf("run: %v %s", err, out)
		}
		if out, err := invoke(project, "fmt", "--check"); err != nil {
			t.Fatalf("template formatting: %v %s", err, out)
		}
		existing := t.TempDir()
		if err := os.WriteFile(filepath.Join(existing, ".gitignore"), []byte("custom\n"), 0644); err != nil {
			t.Fatal(err)
		}
		if out, err := invoke(existing, "init"); err != nil {
			t.Fatalf("init current directory: %v %s", err, out)
		}
		kept, _ := os.ReadFile(filepath.Join(existing, ".gitignore"))
		if string(kept) != "custom\n" {
			t.Fatal("overwrote existing gitignore")
		}
		if _, err := invoke(project, "init"); err == nil {
			t.Fatal("reinitialization overwrote an existing project")
		}
		for _, conflict := range []string{"main.ghi", "mojave.json", "tests"} {
			dir := t.TempDir()
			path := filepath.Join(dir, conflict)
			if err := os.WriteFile(path, []byte("keep me"), 0644); err != nil {
				t.Fatal(err)
			}
			if _, err := invoke(dir, "init"); err == nil {
				t.Fatalf("accepted conflict: %s", conflict)
			}
			kept, _ := os.ReadFile(path)
			if string(kept) != "keep me" {
				t.Fatalf("overwrote %s", conflict)
			}
			entries, _ := os.ReadDir(dir)
			if len(entries) != 1 {
				t.Fatalf("partial project after conflict %s", conflict)
			}
		}
	}
}

// An editor check must use the buffer for both parsing and source positions,
// while imports still resolve from the actual project and disk stays untouched.
func TestCheckEditorOverlay(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "ghi.exe")
	build := exec.Command("go", "build", "-o", binary, "../../cmd/ghi")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v %s", err, out)
	}
	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "main.ghi")
	saved := "namespace main\nfunc main() {}\n"
	if err := os.WriteFile(sourcePath, []byte(saved), 0600); err != nil {
		t.Fatal(err)
	}
	lib := filepath.Join(dir, "model")
	if err := os.Mkdir(lib, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(lib, "value.ghi"), []byte("namespace model\nfunc Value() int { return 4 }\n"), 0600); err != nil {
		t.Fatal(err)
	}
	invoke := func(input string, args ...string) (string, int) {
		t.Helper()
		c := exec.Command(binary, args...)
		c.Dir = dir
		c.Stdin = strings.NewReader(input)
		out, err := c.CombinedOutput()
		if err == nil {
			return string(out), 0
		}
		if e, ok := err.(*exec.ExitError); ok {
			return string(out), e.ExitCode()
		}
		t.Fatal(err)
		return "", -1
	}
	args := []string{"check", "--stdin", "--filename", sourcePath, dir}
	valid := "namespace main\nimport model\n\nfunc main() {\n println(model.Value())\n}\n"
	if out, code := invoke(valid, args...); code != 0 {
		t.Fatalf("valid overlay: %d %s", code, out)
	}
	invalid := "namespace main\nimport model\n\nfunc main() {\n println(model.Value())\n missing()\n}\n"
	if out, code := invoke(invalid, args...); code != 1 || !strings.Contains(out, sourcePath+":6:2") {
		t.Fatalf("buffer diagnostic: %d %s", code, out)
	}
	if out, code := invoke("", "check", dir); code != 0 {
		t.Fatalf("disk project: %d %s", code, out)
	}
	after, err := os.ReadFile(sourcePath)
	if err != nil || string(after) != saved {
		t.Fatalf("source changed: %v %q", err, after)
	}
	for _, a := range [][]string{
		{"check", "--stdin", dir},
		{"check", "--filename", sourcePath, dir},
		{"check", "--stdin", "--filename", "main.ghi", dir},
	} {
		if out, code := invoke(valid, a...); code != 2 {
			t.Fatalf("arguments %v: %d %s", a, code, out)
		}
	}
	missing := filepath.Join(dir, "new.ghi")
	if out, code := invoke(valid, "check", "--stdin", "--filename", missing, dir); code != 1 || !strings.Contains(out, "existing production") {
		t.Fatalf("missing target: %d %s", code, out)
	}
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Fatalf("created overlay file: %v", err)
	}
}
