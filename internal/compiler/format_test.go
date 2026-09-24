package compiler

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFormatSourcePreservesLanguageAndLiterals(t *testing.T) {
	source := "namespace main\r\n\r\n// keep ?Type\r\nclass Box {\r\n    private value ?string\r\n    constructor(value string = \"a  ? b\") { this.value=value }\r\n    public func get() ?string { return this.value }\r\n}\r\nfunc main() {\r\n    raw := `line  one\r\n  line two`\r\n    println(raw) // exact comment\r\n    try { throw \"error\" } catch err Exception { println(err) } finally { println(\"done\") }\r\n}\r\n"
	got, err := FormatSource("main.ghi", []byte(source))
	if err != nil {
		t.Fatal(err)
	}
	for _, literal := range []string{"`line  one\r\n  line two`", "\"a  ? b\"", "// exact comment", "// keep ?Type", "\tprivate value ?string", "this.value = value"} {
		if !bytes.Contains(got, []byte(literal)) {
			t.Fatalf("missing preserved/formatted %q:\n%s", literal, got)
		}
	}
	again, err := FormatSource("main.ghi", got)
	if err != nil || !bytes.Equal(got, again) {
		t.Fatalf("not idempotent: %v\n%s", err, again)
	}
}

func TestFormatProjectValidatesBeforeWritingAndIncludesTests(t *testing.T) {
	root := t.TempDir()
	original := []byte("namespace main\nfunc main(){println(1+2)}\n")
	main := filepath.Join(root, "main.ghi")
	if err := os.WriteFile(main, original, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "tests"), 0755); err != nil {
		t.Fatal(err)
	}
	test := filepath.Join(root, "tests", "case.ghi")
	if err := os.WriteFile(test, []byte("invalid"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := FormatProject(context.Background(), root, false); err == nil {
		t.Fatal("accepted invalid file")
	}
	got, _ := os.ReadFile(main)
	if !bytes.Equal(got, original) {
		t.Fatal("partially changed project")
	}
	if err := os.WriteFile(test, []byte("namespace tests\nfunc test(){println(1)}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "vendor"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "vendor", "bad.ghi"), []byte("invalid"), 0644); err != nil {
		t.Fatal(err)
	}
	changed, err := FormatProject(context.Background(), root, true)
	if err != nil || len(changed) != 2 {
		t.Fatalf("check: %v %v", changed, err)
	}
	got, _ = os.ReadFile(main)
	if !bytes.Equal(got, original) {
		t.Fatal("check modified file")
	}
	changed, err = FormatProject(context.Background(), root, false)
	if err != nil || len(changed) != 2 {
		t.Fatalf("write: %v %v", changed, err)
	}
	changed, err = FormatProject(context.Background(), root, true)
	if err != nil || len(changed) != 0 {
		t.Fatalf("second check: %v %v", changed, err)
	}
}

func TestFormatSourceRejectsInvalidInput(t *testing.T) {
	for _, source := range []string{"namespace main\nfunc main( {", "namespace main\nclass Broken { public", "namespace main\nvar x = \"unterminated"} {
		got, err := FormatSource("invalid.ghi", []byte(source))
		if err == nil || len(got) != 0 || !strings.Contains(err.Error(), "invalid.ghi") {
			t.Fatalf("invalid input: %q %v", got, err)
		}
	}
}

func TestFormatPreservesExecutableBehavior(t *testing.T) {
	source := `namespace main
import fmt "go:fmt"
interface Labelled { func label() string }
class Base {
 protected name string
 constructor(name string = "default") { this.name=name }
 public func label() string { return this.name }
}
class Item extends Base implements Labelled {
 constructor(name string = "  literal  ") { parent(name) }
 public override func label() string { return parent.label()+"!" }
}
func main() {
 item:=Item()
 var optional ?Item = item
 if optional != nil { fmt.Println(optional.label()) }
 values:=[]int{1,2,3}
 for i:=0;i<len(values);i++ { fmt.Println(-values[i]) }
 try { throw Exception("failure") } catch err Exception { fmt.Println(err.message) } finally { fmt.Println("done") }
}
`
	formatted, err := FormatSource("main.ghi", []byte(source))
	if err != nil {
		t.Fatal(err)
	}
	before := runProgram(t, map[string]string{"main.ghi": source})
	after := runProgram(t, map[string]string{"main.ghi": string(formatted)})
	if before != after {
		t.Fatalf("changed execution: before=%q after=%q", before, after)
	}
}

func TestFormatCommentsAndSemicolons(t *testing.T) {
	for _, source := range []string{
		"namespace main\nfunc main(){\nprintln(1 + /* keep\nline */ 2)\n}\n",
		"namespace main\nfunc main(){\nprintln(1) /* trailing\ncomment */\nprintln(2)\n}\n",
		"namespace main\nfunc main(){ for i:=0;i<2;i++ { println(i) }; println(3) } // end",
		"namespace main\n/* keep\r\n raw\r\n*/\nfunc main() {}\n",
	} {
		formatted, err := FormatSource("main.ghi", []byte(source))
		if err != nil {
			t.Fatal(err)
		}
		again, err := FormatSource("main.ghi", formatted)
		if err != nil || !bytes.Equal(formatted, again) {
			t.Fatalf("not idempotent: %v\n%s", err, again)
		}
	}
}
