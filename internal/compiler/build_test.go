package compiler

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func project(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func runProgram(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := project(t, files)
	output := filepath.Join(dir, "program")
	if runtime.GOOS == "windows" {
		output += ".exe"
	}
	result, err := Build(context.Background(), Options{Dir: dir, Output: output})
	if err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(result.Executable).CombinedOutput()
	if err != nil {
		t.Fatalf("run: %v\n%s", err, out)
	}
	return strings.ReplaceAll(string(out), "\r\n", "\n")
}

func TestBuildNamespacesAndRunExecutable(t *testing.T) {
	got := runProgram(t, map[string]string{
		"main.ghi": `namespace main
import fmt "go:fmt"
import greeting "app.greeting"
func main() { fmt.Println(greeting.Message("Ghi")) }
`,
		"greeting/message.ghi": `namespace app.greeting
func Message(name string) string { return prefix() + name }
`,
		"greeting/prefix.ghi": `namespace app.greeting
func prefix() string { return "Hello, " }
`,
	})
	if got != "Hello, Ghi\n" {
		t.Fatalf("output: %q", got)
	}
}

func TestInvalidTypesDoNotReplaceExistingOutput(t *testing.T) {
	dir := project(t, map[string]string{"main.ghi": "namespace main\nfunc main() { var n int = \"wrong\"; println(n) }\n"})
	target := filepath.Join(dir, "previous-binary")
	if err := os.WriteFile(target, []byte("keep"), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := Build(context.Background(), Options{Dir: dir, Output: target})
	if err == nil {
		t.Fatal("invalid types accepted")
	}
	contents, _ := os.ReadFile(target)
	if string(contents) != "keep" {
		t.Fatal("failed build replaced existing output")
	}
}

func TestInvalidNamespaces(t *testing.T) {
	for name, files := range map[string]map[string]string{
		"mixed directory": {"a.ghi": "namespace main\nfunc main() {}", "b.ghi": "namespace other\n"},
		"cycle":           {"a.ghi": "namespace main\nimport b \"b\"\nfunc main() {}", "b/b.ghi": "namespace b\nimport a \"main\"\n"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Build(context.Background(), Options{Dir: project(t, files)})
			if err == nil {
				t.Fatal("invalid namespace project accepted")
			}
		})
	}
}
