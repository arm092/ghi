package language_test

import (
	"context"
	"ghi/internal/compiler"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Concrete receiver optimization must preserve the interface type of aliases,
// captured/rebound receivers, generic fields and inherited virtual calls.
func TestReceiverSpecializationSemantics(t *testing.T) {
	runMatchSource(t, `namespace main
class Cell[T any] {
 public value T
 constructor(value T) { this.value = value }
 public func alias(other Cell[T]) T {
  copy := this
  copy = other
  return copy.value
 }
 public func rebound(other Cell[T]) T {
  change := () => { this = other }
  change()
  return this.value
 }
 public func captured() func() T { return () T => { return this.value } }
 public func address(other Cell[T]) T {
  ref := &this
  if ref != nil { *ref = other }
  return this.value
 }
}
class Base {
 public func value() int { return 1 }
 public func read() int { return this.value() }
 public func alias(other Base) int { copy := this; copy = other; return copy.value() }
 public func rebound(other Base) int { this = other; return this.value() }
 public func later() func() int { return () int => { return this.value() } }
}
class Grandchild extends Child { public override func value() int { return 3 } }
class Child extends Base {
 public override func value() int { return 2 }
 public func viaParent() int { return parent.read() }
}
func main() {
 a := new Cell[int](3)
 b := new Cell[int](8)
 println(a.alias(b), a.rebound(b), a.address(b), a.captured()())
 println(new Child().viaParent())
 c := new Grandchild()
 println(c.read(), c.viaParent(), c.alias(new Base()), c.rebound(new Base()), c.later()())
}
`, "8 8 8 3\n2\n3 3 1 1 3\n")
}

func TestInheritedSpecializationSourceFramesAndImports(t *testing.T) {
	dir := t.TempDir()
	base := `namespace main
import text "go:strings"
class Base {
 public func label() string { return text.ToUpper("base") }
 public func fail() { throw new Exception("failure") }
 public func closure() { f := () => { throw new Exception("closure") }; f() }
}
`
	source := `namespace main
import text "go:strconv"
import strings "go:strings"
class Child extends Base {}
func main() {
 c := new Child()
 println(c.label(), text.Itoa(7))
 try { c.fail() } catch err Exception {
  frame := err.stackTrace[0]
  println(frame.functionName, strings.HasSuffix(frame.file, "base.ghi"), frame.line)
 }
 try { c.closure() } catch err Exception {
  frame := err.stackTrace[0]
  println(strings.HasPrefix(frame.functionName, "main.Base.closure."), strings.HasSuffix(frame.file, "base.ghi"), frame.line)
 }
}
`
	for name, data := range map[string]string{"main.ghi": source, "base.ghi": base} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	built, err := compiler.Build(context.Background(), compiler.Options{Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	output, err := exec.Command(built.Executable).CombinedOutput()
	if err != nil {
		t.Fatalf("%v: %s", err, output)
	}
	if got := strings.ReplaceAll(string(output), "\r\n", "\n"); got != "BASE 7\nmain.Base.fail true 5\ntrue true 6\n" {
		t.Fatalf("unexpected frames/imports: %s", got)
	}
}
