package compiler

import (
	"archive/zip"
	"bytes"
	"context"
	"github.com/arm092/mojave/pkg/mojave"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestProjectGoDependenciesAndRelativeReplacement(t *testing.T) {
	module := `module example.test/backend

go 1.26.0

require example.test/greeting v0.0.0
replace example.test/greeting => "./local dependency"
`
	root := project(t, map[string]string{
		"go.mod": module,
		"main.ghi": `namespace main
import fmt "go:fmt"
import greeting "go:example.test/greeting"
func main(){fmt.Println(greeting.Message("Ghi"))}
`,
		"local dependency/go.mod": "module example.test/greeting\n\ngo 1.26.0\n",
		"local dependency/greeting.go": `package greeting
func Message(name string)(string,error){return "external "+name,nil}
`,
	})
	built, err := Build(context.Background(), Options{Dir: root})
	if err != nil {
		t.Fatal(err)
	}
	output, err := exec.Command(built.Executable).CombinedOutput()
	if err != nil {
		t.Fatalf("run: %v: %s", err, output)
	}
	if strings.TrimSpace(string(output)) != "external Ghi" {
		t.Fatalf("output %q", output)
	}
	original, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	if string(original) != module {
		t.Fatal("build modified the project's go.mod")
	}
	if _, err := os.Stat(filepath.Join(root, "go.sum")); !os.IsNotExist(err) {
		t.Fatal("build modified the project's lockfile")
	}
}

func TestInvalidDependencyManifestDoesNotReplaceExecutable(t *testing.T) {
	root := project(t, map[string]string{"main.ghi": "namespace main\nfunc main(){}", "go.mod": "this is not a module manifest\n"})
	output := filepath.Join(root, "existing.bin")
	if err := os.WriteFile(output, []byte("previous executable"), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := Build(context.Background(), Options{Dir: root, Output: output})
	if err == nil {
		t.Fatal("malformed go.mod accepted")
	}
	bytes, readErr := os.ReadFile(output)
	if readErr != nil || string(bytes) != "previous executable" {
		t.Fatal("failed dependency loading damaged the executable")
	}
}

func TestDependencyDownloadVerifiesProjectChecksum(t *testing.T) {
	var archive bytes.Buffer
	writer := zip.NewWriter(&archive)
	for path, content := range map[string]string{
		"go.mod":     "module example.test/library\n\ngo 1.26.0\n",
		"library.go": "package library\nfunc Value() string{return \"verified\"}\n",
	} {
		file, err := writer.Create("example.test/library@v1.0.0/" + path)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := file.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/example.test/library/@v/v1.0.0.info":
			w.Write([]byte(`{"Version":"v1.0.0","Time":"2026-01-01T00:00:00Z"}`))
		case "/example.test/library/@v/v1.0.0.mod":
			w.Write([]byte("module example.test/library\n\ngo 1.26.0\n"))
		case "/example.test/library/@v/v1.0.0.zip":
			w.Write(archive.Bytes())
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	t.Setenv("GOPROXY", server.URL)
	t.Setenv("GOSUMDB", "off")
	// Go normally makes extracted modules read-only; keep this disposable test
	// cache writable so testing.TempDir can remove it on Unix hosts.
	t.Setenv("GOFLAGS", "-modcacherw")
	t.Setenv("GOMODCACHE", t.TempDir())
	root := project(t, map[string]string{
		"go.mod":   "module example.test/app\n\ngo 1.26.0\n\nrequire example.test/library v1.0.0\n",
		"main.ghi": "namespace main\nimport library \"go:example.test/library\"\nfunc main(){println(library.Value())}\n",
	})
	if _, err := Build(context.Background(), Options{Dir: root}); err != nil {
		t.Fatal(err)
	}
	// Mojave projects compile the same native dependency without root Go files.
	managedRoot := project(t, map[string]string{
		"mojave.json": `{"version":1,"goDependencies":{"example.test/library":"v1.0.0"}}`,
		"main.ghi":    "namespace main\nimport library \"go:example.test/library\"\nfunc main(){println(library.Value())}\n",
	})
	if err := mojave.Install(context.Background(), managedRoot); err != nil {
		t.Fatal(err)
	}
	locked, err := os.ReadFile(filepath.Join(managedRoot, "mojave.lock"))
	if err != nil {
		t.Fatal(err)
	}
	built, err := Build(context.Background(), Options{Dir: managedRoot})
	if err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(built.Executable).CombinedOutput()
	if err != nil || strings.TrimSpace(string(out)) != "verified" {
		t.Fatalf("managed binary: %v %s", err, out)
	}
	for _, name := range []string{"go.mod", "go.sum"} {
		if _, err := os.Stat(filepath.Join(managedRoot, name)); !os.IsNotExist(err) {
			t.Fatalf("build created root %s", name)
		}
	}
	after, err := os.ReadFile(filepath.Join(managedRoot, "mojave.lock"))
	if err != nil || !bytes.Equal(locked, after) {
		t.Fatal("build changed Mojave lock")
	}
	if err := os.WriteFile(filepath.Join(managedRoot, "go.mod"), []byte("module conflicting\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := Build(context.Background(), Options{Dir: managedRoot}); err == nil || !strings.Contains(err.Error(), "remove the legacy go.mod") {
		t.Fatalf("ambiguous manifests accepted: %v", err)
	}
	// A fresh cache forces the second build to verify the downloaded archive.
	t.Setenv("GOMODCACHE", t.TempDir())
	sums := "example.test/library v1.0.0 h1:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=\n"
	if err := os.WriteFile(filepath.Join(root, "go.sum"), []byte(sums), 0644); err != nil {
		t.Fatal(err)
	}
	_, err = Build(context.Background(), Options{Dir: root})
	if err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("expected checksum rejection, got %v", err)
	}
	unchanged, readErr := os.ReadFile(filepath.Join(root, "go.sum"))
	if readErr != nil || string(unchanged) != sums {
		t.Fatal("dependency verification changed the project's lockfile")
	}
}
