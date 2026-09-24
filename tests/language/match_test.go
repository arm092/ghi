package language_test

import (
	"bytes"
	"context"
	"ghi/internal/compiler"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMatchExecution(t *testing.T) {
	source := `namespace main
import strconv "go:strconv"
var calls = 0
func subject() int { calls++; return 2 }
func forbidden() string { panic("unselected arm ran") }
func choose[T comparable](x T, y T, result string) string {
 return match x { y => result, default => "other", }
}
class Label {
 public text string
 constructor(text string) { this.text = text }
 public func describe(code int) string {
  return match code { 2 => this.text, default => "other", }
 }
}
func main() {
 value := match subject() {
  0, 1 => forbidden(),
  2 => match true { false => forbidden(), default => "chosen", },
  default => forbidden(),
 }
 println(value, calls)
 println(choose(3, 3, "generic"))
 println(new Label("class").describe(2))
 values := match 1 { 1 => []int{3, 4}, default => []int{}, }
 println(values[0], len(values))
 cb := match true { true => () int => { return 7 }, default => () int => { return 8 }, }
 println(cb())
 n := match true { true => strconv.Atoi("42"), default => 0, }
 println(n)
 try {
  n := match false { true => 0, default => strconv.Atoi("invalid"), }
  println(n)
 } catch err Exception { println("caught") }
 println(match 9 { 0 => "no", default => "fallback", })
}
`
	runMatchSource(t, source, "chosen 1\ngeneric\nclass\n3 2\n7\n42\ncaught\nfallback\n")
	formatted, err := compiler.FormatSource("main.ghi", []byte(source))
	if err != nil {
		t.Fatal(err)
	}
	again, err := compiler.FormatSource("main.ghi", formatted)
	if err != nil || !bytes.Equal(formatted, again) {
		t.Fatalf("not idempotent: %v", err)
	}
	runMatchSource(t, string(formatted), "chosen 1\ngeneric\nclass\n3 2\n7\n42\ncaught\nfallback\n")
}

func runMatchSource(t *testing.T, source, want string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.ghi"), []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	result, err := compiler.Build(ctx, compiler.Options{Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	output, err := exec.CommandContext(ctx, result.Executable).CombinedOutput()
	if err != nil {
		t.Fatalf("run: %v\n%s", err, output)
	}
	if strings.ReplaceAll(string(output), "\r\n", "\n") != want {
		t.Fatalf("got %q, want %q", output, want)
	}
}

func TestMatchFormatting(t *testing.T) {
	source := "namespace main\nfunc main(){x := match 2 {1, 2 => []int{1, 2}, /* fallback */ default => []int{}, }; println(x[0], len(x))}\n"
	formatted, err := compiler.FormatSource("main.ghi", []byte(source))
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{"match 2 {\n\t\t1, 2 => []int{\n", "\n\t\tdefault => []int{},\n\t}", "/* fallback */"} {
		if !bytes.Contains(formatted, []byte(fragment)) {
			t.Fatalf("missing %q in:\n%s", fragment, formatted)
		}
	}
	again, err := compiler.FormatSource("main.ghi", formatted)
	if err != nil || !bytes.Equal(formatted, again) {
		t.Fatalf("not idempotent: %v\n%s\n%s", err, formatted, again)
	}
	runMatchSource(t, string(formatted), "1 2\n")
}

func TestMatchDefaultArgumentFormatting(t *testing.T) {
	source := "namespace main\nfunc value(n int = match true { true => 3, default => 4, }, suffix int = 2) int { return n + suffix }\nfunc main() { println(value()) }\n"
	formatted, err := compiler.FormatSource("main.ghi", []byte(source))
	if err != nil {
		t.Fatal(err)
	}
	again, err := compiler.FormatSource("main.ghi", formatted)
	if err != nil || !bytes.Equal(formatted, again) {
		t.Fatalf("not idempotent: %v", err)
	}
	runMatchSource(t, string(formatted), "5\n")
}

func TestMatchRejections(t *testing.T) {
	for _, body := range []string{
		`x := match 1 { 1 => 2, }; println(x)`,
		`x := match 1 { default => 2, default => 3, }; println(x)`,
		`x := match 1 { default => 2, 1 => 3, }; println(x)`,
		`x := match 1 { 1 => 2, default => "wrong", }; println(x)`,
		`x := match 1 { "wrong" => 2, default => 3, }; println(x)`,
		`x := match 1 { 1 => 2, default => 3 }; println(x)`,
		`x := match 1 { 1 => {}, default => 3, }; println(x)`,
		`x := match 1 { 1 => println(1), default => println(2), }; println(x)`,
		`x := match 1 { 1 => nil, default => nil, }; println(x)`,
		`x := match 1 { 1 => missing(), default => 0, }; println(x)`,
		`x := match 1 { 1 => 0, 1 => 1, default => 2, }; println(x)`,
	} {
		t.Run(body, func(t *testing.T) {
			dir := t.TempDir()
			source := "namespace main\nfunc main() {\n" + body + "\n}\n"
			if err := os.WriteFile(filepath.Join(dir, "bad.ghi"), []byte(source), 0600); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			err := compiler.Check(ctx, compiler.Options{Dir: dir})
			if err == nil || !strings.Contains(err.Error(), "bad.ghi:3") || strings.Contains(err.Error(), "ghi_match_result") {
				t.Fatalf("expected original source error, got %v", err)
			}
		})
	}
}

func TestMatchTypesAndEvaluationOrder(t *testing.T) {
	source := `namespace main
import time "go:time"
class User { public name string; constructor(name string) { this.name = name } }
var visited = ""
func candidate(name string, value int) int { visited += name; return value }
func main() {
 n := match 2 { candidate("a", 1), candidate("b", 2) => 5, candidate("c", 3) => 6, default => 0, }
 println(n, visited)
 duration := match true { true => 0, default => time.Second, }
 println(duration == time.Duration(0))
 var maybe ?User = new User("nullable")
 user := match false { true => new User("first"), default => maybe, }
 if user != nil { println(user.name) }
 u := match false { true => new User("first"), default => nil, }
 println(u == nil)
 value := match true { true => 1, default => 2.5, }
 println(value == 1.0)
 numbers := match false { true => []int{1}, default => nil, }
	println(len(numbers))
	users := map[string]User{}
	lookedUp := match true { true => users["absent"], default => new User("default"), }
	println(lookedUp == nil)
	channel := make(chan User)
	close(channel)
	received := match true { true => <-channel, default => new User("default"), }
	println(received == nil)
 // Parentheses disambiguate a composite subject, as in Go switch headers.
 println(match (struct{ N int }{N: 1}) { (struct{ N int }{N: 1}) => 9, default => 0, })
}
`
	runMatchSource(t, source, "5 ab\ntrue\nnullable\ntrue\ntrue\n0\ntrue\ntrue\n9\n")
}
