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
	for _, namespace := range []string{"main", "base"} {
		for _, debug := range []bool{false, true} {
			name := namespace
			if debug {
				name += "/debug"
			}
			t.Run(name, func(t *testing.T) { testInheritedFrames(t, namespace, debug) })
		}
	}
}

func testInheritedFrames(t *testing.T, namespace string, debug bool) {
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
	basePath := "base.ghi"
	if namespace != "main" {
		base = strings.Replace(base, "namespace main", "namespace base", 1)
		source = strings.Replace(source, "namespace main", "namespace main\nimport base.Base", 1)
		source = strings.Replace(source, "main.Base.closure.", "base.Base.closure.", 1)
		basePath = "base/base.ghi"
	}
	for name, data := range map[string]string{"main.ghi": source, basePath: base} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	built, err := compiler.Build(context.Background(), compiler.Options{Dir: dir, Debug: debug})
	if err != nil {
		t.Fatal(err)
	}
	output, err := exec.Command(built.Executable).CombinedOutput()
	if err != nil {
		t.Fatalf("%v: %s", err, output)
	}
	want := "BASE 7\n" + namespace + ".Base.fail true 5\ntrue true 6\n"
	if got := strings.ReplaceAll(string(output), "\r\n", "\n"); got != want {
		t.Fatalf("unexpected frames/imports: %s", got)
	}
}

func TestCrossNamespaceReceiverSemantics(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"base/base.ghi": `namespace base
import text "go:strings"
var hidden = 7
class Base {
 private count int
 public func value() int { return 1 }
 public func read() int { this.count++; return this.value() + this.count }
 public func label(input string) string { return text.ToUpper(input) }
 public func alias(other Base) int { copy := this; copy = other; return copy.value() }
 public func rebound(other Base) int { this = other; return this.value() }
 public func later() func() int { return () int => { return this.value() } }
 public func fallback() int { return hidden }
 public func make() Base { return new Base() }
 public func length() int { return len("abc") }
}
`,
		"child/child.ghi": `namespace child
import base.Base
class Child extends Base {
 public override func value() int { return 2 }
 public func viaParent() int { return parent.value() }
}
`,
		"main.ghi": `namespace main
import base.Base
import child.Child
func len(value string) int { return 99 }
class Grandchild extends Child { public override func value() int { return 3 } }
func main() {
 c := new Grandchild()
 println(c.read(), c.read(), c.viaParent(), c.alias(new Base()), c.rebound(new Base()), c.later()())
 println(c.label("ok"), c.fallback(), c.make().value(), c.length())
}
`,
	}
	for name, source := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(source), 0600); err != nil {
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
	if got := strings.ReplaceAll(string(output), "\r\n", "\n"); got != "4 5 1 1 1 3\nOK 7 1 3\n" {
		t.Fatalf("unexpected cross-namespace behavior: %s", got)
	}
}
