package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestDeveloperToolCommands(t *testing.T) {
	root := t.TempDir()
	binary := filepath.Join(root, "ghi")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	if out, err := exec.Command("go", "build", "-o", binary, ".").CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	project := filepath.Join(root, "app")
	if err := os.MkdirAll(filepath.Join(project, "tests"), 0755); err != nil {
		t.Fatal(err)
	}
	main := filepath.Join(project, "main.ghi")
	original := "namespace main\nfunc main(){println(1+2)}\n"
	if err := os.WriteFile(main, []byte(original), 0644); err != nil {
		t.Fatal(err)
	}
	testFile := filepath.Join(project, "tests", "basic.ghi")
	if err := os.WriteFile(testFile, []byte("namespace tests\nimport testing \"go:testing\"\nfunc TestWorks(t *testing.T){if 1+2!=3{t.Fatal(\"bad sum\")}}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	run := func(code int, args ...string) string {
		t.Helper()
		out, err := exec.Command(binary, args...).CombinedOutput()
		actual := 0
		if err != nil {
			exit, ok := err.(*exec.ExitError)
			if !ok {
				t.Fatal(err)
			}
			actual = exit.ExitCode()
		}
		if actual != code {
			t.Fatalf("%v: exit %d want %d: %s", args, actual, code, out)
		}
		return string(out)
	}
	run(1, "fmt", "--check", project)
	data, _ := os.ReadFile(main)
	if string(data) != original {
		t.Fatal("format check wrote source")
	}
	run(0, "fmt", project)
	run(0, "fmt", "--check", project)
	if out := run(0, "test", "-v", project); !strings.Contains(out, "PASS: TestWorks") {
		t.Fatalf("did not execute test: %s", out)
	}
	if _, err := os.Stat(filepath.Join(project, "bin")); !os.IsNotExist(err) {
		t.Fatal("test created production binary")
	}
	if err := os.WriteFile(testFile, []byte("namespace tests\nimport testing \"go:testing\"\nfunc TestFails(t *testing.T){t.Fatal(\"expected failure\")}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if out := run(1, "test", project); !strings.Contains(out, "expected failure") {
		t.Fatalf("missing failure: %s", out)
	}
}
