package compiler

import (
	"go/token"
	"strings"
	"testing"
)

func TestNullableTypeSyntax(t *testing.T) {
	source := `namespace main
import models "app.models"
// ?User is a nullable reference; User? is the old spelling.
class Box {
 public owner ?models.User
 constructor(owner ?models.User = nil) { this.owner = owner }
 public func get() ?models.User { return this.owner }
}
func optional(value []?models.User) ?models.User { return nil }
func main() { println("User?") }
`
	_, tree, unit, err := parseFile(token.NewFileSet(), "nullable.ghi", []byte(source))
	if err != nil {
		t.Fatal(err)
	}
	box := unit.Classes[0]
	if got := expressionText(box.Fields[0].Type); got != "*models.User" {
		t.Fatalf("field type %s", got)
	}
	if got := expressionText(box.Constructor.Node.Type.Params.List[0].Type); got != "*models.User" {
		t.Fatalf("parameter type %s", got)
	}
	if got := expressionText(unit.Functions["optional"].Node.Type.Results.List[0].Type); got != "*models.User" {
		t.Fatalf("return type %s", got)
	}
	if got := expressionText(unit.Functions["optional"].Node.Type.Params.List[0].Type); got != "[]*models.User" {
		t.Fatalf("slice type %s", got)
	}
	if len(tree.Comments) == 0 || !strings.Contains(tree.Comments[0].Text(), "User?") {
		t.Fatal("nullable normalization changed comments")
	}
}

func TestNullableNormalizationPreservesLinesAndLiterals(t *testing.T) {
	source := []byte("var u ?pkg.User\nvar list []?User\nprintln(`?User`, \"User?\") // ?\n")
	got, err := normalizeNullable("input.ghi", source)
	if err != nil {
		t.Fatal(err)
	}
	want := "var u *pkg.User\nvar list []*User\nprintln(`?User`, \"User?\") // ?\n"
	if string(got) != want {
		t.Fatalf("normalization: %q", got)
	}
	if len(got) != len(source) {
		t.Fatal("source offsets changed")
	}
	for _, input := range []string{"var x ?", "var x User??", "var x User ?", "var x ?User @", "var x User?", "var x ? User", "var x ??User", "var x ?/*comment*/User"} {
		if _, err := normalizeNullable("bad.ghi", []byte(input)); err == nil {
			t.Errorf("accepted malformed source %q", input)
		}
	}
}
