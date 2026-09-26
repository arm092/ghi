package cli_test

import (
	"context"
	"ghi/internal/compiler"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// A cached successful check must never hide edits, missing files or invalid
// code, nor replace the last working executable after a failed compilation.
func TestIncrementalBuildInvalidation(t *testing.T) {
	dir := t.TempDir()
	write := func(name, data string) {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("main.ghi", "namespace main\nimport model\nfunc main(){println(model.Value())}\n")
	write("model/value.ghi", "namespace model\nfunc Value() int { return 1 }\n")
	options := compiler.Options{Dir: dir, Output: filepath.Join(dir, "result.exe")}
	run := func(want string) {
		t.Helper()
		out, err := exec.Command(options.Output).CombinedOutput()
		if err != nil || strings.TrimSpace(string(out)) != want {
			t.Fatalf("run: %v, %s; want %s", err, out, want)
		}
	}
	build := func(want string) {
		t.Helper()
		if _, err := compiler.Build(context.Background(), options); err != nil {
			t.Fatal(err)
		}
		run(want)
	}
	state := filepath.Join(dir, ".ghi/build/state.json")
	build("1")
	before, err := os.Stat(state)
	if err != nil {
		t.Fatal(err)
	}
	build("1")
	after, err := os.Stat(state)
	if err != nil {
		t.Fatal(err)
	}
	if !before.ModTime().Equal(after.ModTime()) {
		t.Fatal("unchanged build did not reuse validated generation")
	}

	write("model/value.ghi", "namespace model\nfunc Value() int { return 2 }\n")
	build("2")
	write("model/value.ghi", "namespace model\nfunc Value() int { return \"invalid\" }\n")
	if _, err := compiler.Build(context.Background(), options); err == nil {
		t.Fatal("cached check hid a type error")
	}
	run("2")
	write("model/value.ghi", "namespace model\nfunc Value() int { return 2 }\n")
	write("extra/invalid.ghi", "namespace extra\nfunc Invalid() int { return \"invalid\" }\n")
	if err := compiler.Check(context.Background(), compiler.Options{Dir: dir}); err == nil {
		t.Fatal("cache hid an added invalid namespace")
	}
	if err := os.Remove(filepath.Join(dir, "extra/invalid.ghi")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, "model/value.ghi")); err != nil {
		t.Fatal(err)
	}
	if _, err := compiler.Build(context.Background(), options); err == nil {
		t.Fatal("cache hid a deleted dependency")
	}
	write("model/value.ghi", "namespace model\nfunc Value() int { return 2 }\n")
	build("2")
	write(".ghi/build/work/model/ghi_source_0.go", "corrupted generated source")
	build("2")
	write(".ghi/build/state.json", "interrupted metadata")
	build("2")

	// An editor overlay and debug build must not populate or consume a cached
	// production check: their semantic input / metadata requirements differ.
	saved, err := os.ReadFile(state)
	if err != nil {
		t.Fatal(err)
	}
	options.Overlay = map[string][]byte{filepath.Join(dir, "model/value.ghi"): []byte("namespace model\nfunc Value() int { return 3 }\n")}
	build("3")
	options.Overlay = nil
	options.Debug = true
	build("2")
	actual, err := os.ReadFile(state)
	if err != nil || string(saved) != string(actual) {
		t.Fatal("overlay/debug changed production cache")
	}
	options.Debug = false
	t.Setenv("GOFLAGS", "-tags=ghi_cache_test")
	build("2")
	actual, err = os.ReadFile(state)
	if err != nil || string(saved) == string(actual) {
		t.Fatal("changed Go build settings did not invalidate generation")
	}
}

func TestIncrementalConcurrentBuilds(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.ghi"), []byte("namespace main\nfunc main(){println(42)}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	var wait sync.WaitGroup
	for _, name := range []string{"first.exe", "second.exe"} {
		wait.Go(func() {
			result, err := compiler.Build(context.Background(), compiler.Options{Dir: dir, Output: filepath.Join(dir, name)})
			if err != nil {
				t.Error(err)
				return
			}
			out, err := exec.Command(result.Executable).CombinedOutput()
			if err != nil || strings.TrimSpace(string(out)) != "42" {
				t.Errorf("concurrent build: %v: %s", err, out)
			}
		})
	}
	wait.Wait()
}

func TestIncrementalLocalGoDependency(t *testing.T) {
	dir := t.TempDir()
	dep := filepath.Join(dir, "native")
	if err := os.MkdirAll(dep, 0700); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"go.mod":          "module app\ngo 1.26.0\nrequire example.test/native v0.0.0\nreplace example.test/native => ./native\n",
		"native/go.mod":   "module example.test/native\ngo 1.26.0\n",
		"native/value.go": "package native\nfunc Value() int {return 1}\n",
		"main.ghi":        "namespace main\nimport native \"go:example.test/native\"\nfunc main(){ var value int = native.Value(); println(value) }\n",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	options := compiler.Options{Dir: dir}
	for i := 0; i < 2; i++ {
		if err := compiler.Check(context.Background(), options); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dep, "value.go"), []byte("package native\nfunc Value() string {return \"changed API\"}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := compiler.Check(context.Background(), options); err == nil || !strings.Contains(err.Error(), "main.ghi") {
		t.Fatalf("cached native API: %v", err)
	}
}
