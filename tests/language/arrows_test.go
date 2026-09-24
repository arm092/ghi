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

func TestArrowClosuresAndNativeCallbacks(t *testing.T) {
	source := `namespace main
import sort "go:sort"
class Counter {
 public value int
 constructor(value int) { this.value = value }
 public func callback() func(int) int {
  return (n int) int => { this.value += n; return this.value }
 }
}
func main() {
 // Keep => in comments and literals intact.
 text := "() => {}"
 if text != "() => {}" { panic("literal changed") }
 total := 1
 increment := () => { total++ }
 increment()
 factory := (base int) func(int) int => {
  return (n int) int => { return base + n + total }
 }
 println(factory(10)(3))
 counter := new Counter(4)
 callback := counter.callback()
 println(callback(3))
 values := []int{3, 1, 2}
 sort.Slice(values, (i int, j int) bool => { return values[i] < values[j] })
 println(values[0], values[2])
 pair := () (int, string) => { return 5, "pair" }
 a, b := pair()
 println(a, b)
 named := (n int) (result int) => { result = n; return }
 println(named(9))
 variadic := (values ...int) int => {
  result := 0
  for _, value := range values { result += value }
  return result
 }
 println(variadic(1, 2, 3))
 println(((n int) int => { return n * 2 })(6))
 fail := () => { throw new Exception("arrow failed") }
 try { fail() } catch err Exception { println(err.message, err.code) }
 done := make(chan int)
 go (() => { done <- 17 })()
 println(<-done)
 defer (() => { println("deferred") })()
}
`
	formatted, err := compiler.FormatSource("main.ghi", []byte(source))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(formatted, []byte("=>")) || bytes.Contains(formatted, []byte("= >")) {
		t.Fatal("formatter split arrow")
	}
	again, err := compiler.FormatSource("main.ghi", formatted)
	if err != nil || !bytes.Equal(formatted, again) {
		t.Fatalf("format not idempotent: %v", err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.ghi"), formatted, 0600); err != nil {
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
	want := "15\n7\n1 3\n5 pair\n9\n6\n12\narrow failed 0\n17\ndeferred\n"
	if strings.ReplaceAll(string(output), "\r\n", "\n") != want {
		t.Fatalf("got %q, want %q", output, want)
	}
}

func TestArrowInvalidSyntax(t *testing.T) {
	for _, expression := range []string{"(x) => {}", "() => 1", "() = > {}", "() =>", "(x int => {}"} {
		t.Run(expression, func(t *testing.T) {
			_, err := compiler.FormatSource("bad.ghi", []byte("namespace main\nfunc main() {\n f := "+expression+"\n _ = f\n}\n"))
			if err == nil || !strings.Contains(err.Error(), "bad.ghi:3") {
				t.Fatalf("expected located error, got %v", err)
			}
		})
	}
}

func TestArrowTypeErrors(t *testing.T) {
	for _, body := range []string{
		`f := (n int) int => { return n }; println(f("wrong"))`,
		`f := () int => { return "wrong" }; println(f())`,
		`f := () => { return 1 }; f()`,
		`var f func(string) = (n int) => {}; f("wrong")`,
	} {
		t.Run(body, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "bad.ghi"), []byte("namespace main\nfunc main() {\n"+body+"\n}\n"), 0600); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			err := compiler.Check(ctx, compiler.Options{Dir: dir})
			if err == nil || !strings.Contains(err.Error(), "bad.ghi:3") {
				t.Fatalf("expected original source type error, got %v", err)
			}
		})
	}
}
