package compiler

import (
	"go/token"
	"strings"
	"testing"
)

func TestMalformedClassClauses(t *testing.T) {
	for _, declaration := range []string{
		"class Broken extends {}",
		"class Broken implements {}",
		"class Broken extends First extends Second {}",
		"class Broken implements First implements Second {}",
	} {
		_, _, _, err := parseFile(token.NewFileSet(), "bad.ghi", []byte("namespace main\n"+declaration))
		if err == nil || !strings.Contains(err.Error(), "bad.ghi:2") {
			t.Errorf("%s: expected located diagnostic, got %v", declaration, err)
		}
	}
}

func TestParserDiagnosticLocations(t *testing.T) {
	for _, source := range []string{
		"namespace main\nclass Broken {",
		"namespace main\nfunc main() { try {} }",
		"namespace main\nfunc main() { try {} catch {} }",
		"namespace main\nfunc main() { try {} catch err {} }",
		"namespace main\nfunc main() { try {} finally }",
	} {
		_, _, _, err := parseFile(token.NewFileSet(), "bad.ghi", []byte(source))
		if err == nil || !strings.Contains(err.Error(), "bad.ghi:2") {
			t.Errorf("%s: expected original file and line, got %v", source, err)
		}
	}
	_, _, _, err := parseFile(token.NewFileSet(), "nested.ghi", []byte("namespace main\nfunc main() {\ntry {\ntry {}\n} finally {}\n}"))
	if err == nil || !strings.Contains(err.Error(), "nested.ghi:4") {
		t.Fatalf("nested exception diagnostic lost original line: %v", err)
	}
}

// Editor buffers are commonly truncated anywhere in a declaration. Every
// prefix must either parse or return a diagnostic, never crash the compiler.
func TestParserTruncatedExtensions(t *testing.T) {
	for _, source := range parserSeeds {
		for end := 0; end <= len(source); end++ {
			t.Run(source[:end], func(t *testing.T) {
				parseFile(token.NewFileSet(), "input.ghi", []byte(source[:end]))
			})
		}
	}
}

var parserSeeds = []string{
	"namespace main\nimport app.users.User as AuthUser\nfunc main(){_=new AuthUser()}",
	"namespace main\nclass Box[T any] extends Base implements Named { public value T; constructor(value T) { this.value = value }; public override func get() T { return this.value } }",
	"namespace main\ninterface Named { func name(value string = \"guest\") string }",
	"namespace main\nfunc main() { try { throw new Exception(\"bad\", 1) } catch e Exception { println(e.message) } finally { println(\"done\") } }",
	"namespace main\nfunc identity[T any](value T) T { return value }",
	"namespace main\nclass User { public owner ?User; constructor(owner ?User = nil) { this.owner = owner } }",
}

func FuzzParseFile(f *testing.F) {
	for _, source := range parserSeeds {
		f.Add([]byte(source))
	}
	f.Fuzz(func(t *testing.T, source []byte) {
		if len(source) > 65536 {
			t.Skip()
		}
		parseFile(token.NewFileSet(), "fuzz.ghi", source)
		// Keep mutations of the namespace from hiding extension-parser bugs.
		parseFile(token.NewFileSet(), "fuzz.ghi", append([]byte("namespace main\n"), source...))
	})
}
