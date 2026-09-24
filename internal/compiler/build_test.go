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

func TestCheckWithoutExecutableAndWithSeparateTests(t *testing.T) {
	dir := project(t, map[string]string{
		"main.ghi":         "namespace main\nfunc main(){}\n",
		"tests/broken.ghi": "not valid source; tests must not enter a production check",
	})
	if err := Check(context.Background(), Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "bin")); !os.IsNotExist(err) {
		t.Fatalf("check created an output directory: %v", err)
	}
	output := filepath.Join(dir, "existing.exe")
	if err := os.WriteFile(output, []byte("previous executable"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := Check(context.Background(), Options{Dir: dir, Output: output}); err == nil {
		t.Fatal("check accepted an output path")
	}
	data, err := os.ReadFile(output)
	if err != nil || string(data) != "previous executable" {
		t.Fatalf("check modified existing output: %s, %v", data, err)
	}
	if _, err := Build(context.Background(), Options{Dir: dir}); err != nil {
		t.Fatalf("production build included tests: %v", err)
	}
}

func TestCheckRejectsInvalidProductionCode(t *testing.T) {
	for name, files := range map[string]map[string]string{
		"bodyless main":               {"main.ghi": "namespace main\nfunc main()"},
		"bodyless helper":             {"main.ghi": "namespace main\nfunc helper()\nfunc main(){}"},
		"missing main":                {"main.ghi": "namespace main\nfunc other(){}"},
		"invalid main":                {"main.ghi": "namespace main\nfunc main(value int){}"},
		"unused namespace":            {"main.ghi": "namespace main\nfunc main(){}", "users/user.ghi": "namespace app.users\nvar value int = \"bad\""},
		"test import":                 {"main.ghi": "namespace main\nimport helpers \"tests.helpers\"\nfunc main(){helpers.Help()}", "tests/helpers/helper.ghi": "namespace tests.helpers\nfunc Help(){}"},
		"nested production directory": {"main.ghi": "namespace main\nfunc main(){}", "app/tests/model.ghi": "namespace app.tests\nvar value int = \"bad\""},
	} {
		t.Run(name, func(t *testing.T) {
			if err := Check(context.Background(), Options{Dir: project(t, files)}); err == nil {
				t.Fatal("invalid production project passed check")
			}
		})
	}
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

func TestSourceFilenamesDoNotBecomeGoBuildConstraints(t *testing.T) {
	got := runProgram(t, map[string]string{
		"main.ghi":           "namespace main\nfunc main(){ println(value()) }",
		"helpers-darwin.ghi": "namespace main\nfunc value() int { return number() }",
		"feature_darwin.ghi": "namespace main\nfunc number() int { return 42 }",
	})
	if got != "42\n" {
		t.Fatalf("output %q", got)
	}
}
