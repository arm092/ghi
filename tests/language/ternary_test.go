package language_test

import (
	"bytes"
	"context"
	"ghi/internal/compiler"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTernaryExecutionAndFormatting(t *testing.T) {
	source := `namespace main
import strconv "go:strconv"
var calls = 0
func condition() bool { calls++; return true }
func forbidden() string { panic("unselected branch") }
func choose[T any](flag bool, a T, b T) T { return flag ? a : b }
func defaults(value int = true ? 7 : 8) int { return value }
class User {
 public name string
 constructor(name string) { this.name = name }
 public func label(active bool) string { return active ? this.name : "off" }
}
func main() {
 println(condition() ? "chosen" : forbidden(), calls)
 println(false ? forbidden() : "fallback")
 println(choose(false, 4, 9), defaults())
 var user ?User = new User("Arman")
 println(user != nil ? user.name : "Guest")
 user = nil
 println(user != nil ? user.name : "Guest")
 println(new User("name").label(true))
 println(true && false ? 1 + 2 : 3 + 4)
 println(false ? "one" : (true ? "two" : "three"))
 xs := true ? []int{1, 2} : []int{}
 println(xs[1])
 cb := true ? () int => { return 5 } : () int => { return 6 }
 println(cb())
 println(true ? strconv.Atoi("42") : 0)
 try { println(false ? 1 : strconv.Atoi("bad")) } catch err Exception { println("caught") }
 println(true ? "literal ? :" : "other") // comment ? :
 if true ? true : false { println("if") }
 for false ? true : false { panic("loop") }
 switch false ? 1 : 2 { case 2: println("switch") }
 println(match 1 { 1 => true ? 8 : 9, default => 0, })
 ch := make(chan bool, 1)
 ch <- true
 println(<-ch ? 10 : 11)
 ch <- false ? true : false
 println(<-ch)
 if x := 1; x == 1 ? true : false { println("init") }
 switch x := 2; x == 2 ? 2 : 3 { case 2: println("init switch") }
 for _, x := range true ? []int{12} : []int{} { println(x) }
 var selected ?User = true ? new User("typed") : nil
 if selected != nil { println(selected.name) }
}
`
	want := "chosen 1\nfallback\n9 7\nArman\nGuest\nname\n7\ntwo\n2\n5\n42\ncaught\nliteral ? :\nif\nswitch\n8\n10\nfalse\ninit\ninit switch\n12\ntyped\n"
	runMatchSource(t, source, want)
	formatted, err := compiler.FormatSource("ternary.ghi", []byte(source))
	if err != nil {
		t.Fatal(err)
	}
	again, err := compiler.FormatSource("ternary.ghi", formatted)
	if err != nil || !bytes.Equal(formatted, again) {
		t.Fatalf("format not stable: %v\n%s", err, formatted)
	}
	if !bytes.Contains(formatted, []byte("user != nil ? user.name : \"Guest\"")) || !bytes.Contains(formatted, []byte("user ?User")) {
		t.Fatalf("operator/type spacing:\n%s", formatted)
	}
	runMatchSource(t, string(formatted), want)
}

func TestTernaryRejectsInvalidExpressions(t *testing.T) {
	for _, expr := range []string{
		`1 ? 2 : 3`, `"yes" ? 2 : 3`, `true ? 1 : "no"`,
		`true ? nil : nil`, `true ? println(1) : println(2)`,
		`true ?: 3`, `true ? 2`, `true ? 2 :`,
		`true ? false ? 1 : 2 : 3`, `true ? 1 : false ? 2 : 3`,
	} {
		t.Run(expr, func(t *testing.T) {
			dir := t.TempDir()
			source := "namespace main\nfunc main() { value := " + expr + "; println(value) }\n"
			if err := os.WriteFile(filepath.Join(dir, "bad.ghi"), []byte(source), 0600); err != nil {
				t.Fatal(err)
			}
			err := compiler.Check(context.Background(), compiler.Options{Dir: dir})
			if err == nil || !strings.Contains(err.Error(), "bad.ghi") || strings.Contains(err.Error(), "ghi_ternary_result") {
				t.Fatalf("expected source diagnostic, got %v", err)
			}
		})
	}
}
