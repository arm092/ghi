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
		{"ternary-condition", "namespace main\nfunc main() {\n _ = 1 ? 2 : 3\n}\n", "ternary condition must be bool", 3},
		{"ternary-branches", "namespace main\nfunc main() {\n _ = true ? 1 : \"no\"\n}\n", "in ternary branch", 3},
		{"nullable-argument", "namespace main\nclass User {}\nfunc take(user User) {}\nfunc main() {\n var user ?User = nil\n take(user)\n}\n", "(variable of type ?User) as User value in argument to take", 6},
		{"native-pointer", "namespace main\nfunc take(value int) {}\nfunc main() {\n value := new(int)\n take(value)\n}\n", "(variable of type *int) as int value", 5},
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
			for _, hidden := range []string{"GhiM_", "GhiBody_", "GhiNew_", "ghiData_", "ghi_arg_", "ghi.generated", "pointer to interface"} {
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
	source := "namespace main\nimport app.box.Box as Alias\nfunc take(value Alias[int]) {}\nfunc main(){var value ?Alias[int] = nil; take(value)}\n"
	if err := os.WriteFile(filepath.Join(dir, "main.ghi"), []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	err = compiler.Check(context.Background(), compiler.Options{Dir: dir})
	if err == nil || !strings.Contains(err.Error(), "?Alias[int]") || strings.Contains(err.Error(), "pointer to interface") || strings.Contains(err.Error(), "ghi_type_import_") {
		t.Fatalf("nullable alias diagnostic: %v", err)
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
	for _, tc := range []struct{ message, hint string }{
		{"non-boolean condition in if statement", "Use a bool condition"},
		{"ternary nested expressions require parentheses", "Parenthesize the nested expression"},
		{"ternary requires a condition and two values", "both values are required"},
		{"ternary result type cannot be inferred; every arm must produce one typed value", "two untyped nil values"},
		{"undefined: missing", ""},
	} {
		err := fmt.Errorf("%s:2:2: %s", path, tc.message)
		pretty := compiler.FormatDiagnostic(err, dir, map[string][]byte{path: []byte("namespace main\n" + line + "\n")})
		if !strings.HasPrefix(pretty, err.Error()+"\n") {
			t.Fatalf("changed diagnostic first line: %s", pretty)
		}
		if tc.hint == "" && strings.Contains(pretty, "hint:") || tc.hint != "" && !strings.Contains(pretty, tc.hint) {
			t.Fatalf("unexpected hint: %s", pretty)
		}
	}
}

func TestTernaryDiagnosticSourceColumns(t *testing.T) {
	for _, tc := range []struct{ line, needle string }{
		{`    label := 1 ? "yes" : "no"; println(label)`, "1"},
		{`    label := true ? 1 : "no"; println(label)`, `"no"`},
		{`    label := true ? "Ղ" : "no"; println(missing)`, "missing"},
		{`    label := true ? "yes" : (2 ? "nested" : "no"); println(label)`, "2"},
	} {
		t.Run(tc.needle+tc.line, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "main.ghi")
			if err := os.WriteFile(path, []byte("namespace main\nfunc main() {\n"+tc.line+"\n}\n"), 0600); err != nil {
				t.Fatal(err)
			}
			err := compiler.Check(context.Background(), compiler.Options{Dir: dir})
			want := fmt.Sprintf("main.ghi:3:%d:", strings.Index(tc.line, tc.needle)+1)
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("expected %s, got %v", want, err)
			}
			if pretty := compiler.FormatDiagnostic(err, dir, nil); !strings.Contains(pretty, "^") {
				t.Fatalf("missing source pointer: %s", pretty)
			}
		})
	}
}
