package language_test

import (
	"context"
	"fmt"
	"ghi/internal/compiler"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSourceDiagnostics(t *testing.T) {
	cases := []struct {
		name, source, message string
		line                  int
	}{
		{"generic-constructor", "namespace main\nclass Box[T ~int] { constructor(value T) {} }\nfunc main() {\n _ = new Box[string](\"x\")\n}\n", "string does not satisfy ~int", 4},
		{"override", "namespace main\nclass Base { public func read() int {return 1} }\nclass Child extends Base {\n public override func read() string {return \"x\"}\n}\nfunc main() {}\n", "incompatible override signature", 4},
		{"parent", "namespace main\nclass Child extends Missing {}\nfunc main() {}\n", "unknown parent class Missing", 2},
		{"constructor-arguments", "namespace main\nclass Box { constructor(value int) {} }\nfunc main() {\n _ = new Box()\n}\n", "not enough arguments in call to Box", 4},
		{"method-arguments", "namespace main\nclass Box { public func take(n int) {} }\nfunc main() {\n b := new Box()\n b.take(\"bad\")\n}\n", "argument to b.take", 5},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "main.ghi"), []byte(tc.source), 0600); err != nil {
				t.Fatal(err)
			}
			err := compiler.Check(context.Background(), compiler.Options{Dir: dir})
			if err == nil {
				t.Fatal("invalid source accepted")
			}
			text := err.Error()
			if !strings.Contains(text, fmt.Sprintf("main.ghi:%d:", tc.line)) || !strings.Contains(text, tc.message) {
				t.Fatalf("wrong diagnostic: %s", text)
			}
			for _, hidden := range []string{"GhiM_", "GhiBody_", "GhiNew_", "ghiData_", "ghi_arg_", "ghi.generated"} {
				if strings.Contains(text, hidden) {
					t.Fatalf("generated detail: %s", text)
				}
			}
		})
	}
}

func TestImportedConstructorDiagnosticAlias(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "project with spaces")
	for name, source := range map[string]string{
		"lib/box.ghi": "namespace app.box\nclass Box[T any] { constructor(value T){} }\n",
		"main.ghi":    "namespace main\nimport app.box.Box as Alias\nfunc main(){new Alias[int]()}\n",
	} {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(source), 0600); err != nil {
			t.Fatal(err)
		}
	}
	err := compiler.Check(context.Background(), compiler.Options{Dir: dir})
	if err == nil || !strings.Contains(err.Error(), "main.ghi:3:") || !strings.Contains(err.Error(), "call to Alias[int]") || strings.Contains(err.Error(), "ghi_type_import_") {
		t.Fatalf("import alias diagnostic: %v", err)
	}
	pretty := compiler.FormatDiagnostic(err, dir, nil)
	if !strings.HasPrefix(pretty, err.Error()) || !strings.Contains(pretty, "3 | func main(){new Alias[int]()}") || !strings.Contains(pretty, "^") {
		t.Fatalf("missing source context: %s", pretty)
	}
}

func TestDiagnosticContextUsesOverlayAndByteColumns(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "project with spaces")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "main.ghi")
	if err := os.WriteFile(path, []byte("saved content"), 0600); err != nil {
		t.Fatal(err)
	}
	line := "\t_ = \"Ղ\"; wrong()"
	err := fmt.Errorf("%s:2:%d: cannot use wrong (variable of type string) as int value in argument", path, strings.Index(line, "wrong")+1)
	pretty := compiler.FormatDiagnostic(err, dir, map[string][]byte{path: []byte("namespace main\r\n" + line + "\r\n")})
	for _, expected := range []string{"2 |     _ = \"Ղ\"; wrong()", "  |              ^~~~~", "expected: int; received: variable of type string"} {
		if !strings.Contains(pretty, expected) {
			t.Fatalf("missing %q:\n%s", expected, pretty)
		}
	}
	outside := compiler.FormatDiagnostic(err, filepath.Join(dir, "other"), nil)
	if outside != err.Error() {
		t.Fatal("rendered source outside project")
	}
}
