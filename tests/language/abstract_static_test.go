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

func TestAbstractDispatchAndStaticMembers(t *testing.T) {
	source := `namespace main
interface Named { func name() string }
abstract class Base {
 protected value string
 constructor(value string = "base") { this.value = value }
 public abstract func name() string
 public func describe() string { return this.name() + ":" + this.value }
}
abstract class Middle extends Base {}
class Child extends Middle implements Named {
 public override func name() string { return "child" }
}
class Counter {
 public static count int = 2
 private static prefix string = "count"
 public static func next(step int = 1) int { Counter.count += step; return Counter.count }
 public static func label() string { return Counter.prefix }
}
class SubCounter extends Counter {}
func main() {
 var base Base = new Child()
 println(base.describe())
 var named Named = base
 println(named.name())
 println(Counter.next(), SubCounter.next(2), Counter.count)
 Counter.count++
 counter := &Counter.count
 *counter += 2
 println(Counter.count, Counter.label())
 next := Counter.next
 println(next(3))
}
`
	formatted, err := compiler.FormatSource("main.ghi", []byte(source))
	if err != nil {
		t.Fatal(err)
	}
	runMatchSource(t, string(formatted), "child:base\nchild\n3 5 5\n8 count\n11\n")
}

func TestAbstractStaticRejections(t *testing.T) {
	for name, item := range map[string]struct{ source, message string }{
		"abstract new":                {`abstract class A{};func main(){_=new A()}`, "cannot instantiate abstract"},
		"abstract call":               {`abstract class A{};func main(){_=A()}`, "cannot instantiate abstract"},
		"missing implementation":      {`abstract class A{public abstract func read()int};class B extends A{};func main(){}`, "abstract method read"},
		"nonabstract declaration":     {`class A{public abstract func read()int};func main(){}`, "abstract class"},
		"abstract body":               {`abstract class A{public abstract func read()int{return 1}};func main(){}`, "abstract methods cannot have a body"},
		"abstract private":            {`abstract class A{private abstract func read()int};func main(){}`, "abstract methods cannot be private"},
		"abstract static":             {`abstract class A{public static abstract func read()int};func main(){}`, "abstract methods cannot be static"},
		"static constructor":          {`class A{static constructor(){}};func main(){}`, "constructor cannot be static"},
		"static this":                 {`class A{public static func read()int{return this.x};x int};func main(){_=A.read()}`, "this is unavailable"},
		"static instance":             {`class A{public static func read()int{return 1}};func main(){_=new A().read()}`, "static method read requires class-qualified access"},
		"static private":              {`class A{private static value int};func main(){_=A.value}`, "field value is private"},
		"static override":             {`class A{public static func read()int{return 1}};class B extends A{public override func read()int{return 2}};func main(){}`, "static methods cannot be overridden"},
		"abstract parent":             {`abstract class A{public abstract func read()int};class B extends A{public override func read()int{return parent.read()}};func main(){}`, "cannot call abstract parent method"},
		"static method value":         {`class A{public static func read()int{return 1}};func main(){a:=new A();_=a.read}`, "static method read requires class-qualified access"},
		"static parent":               {`class A{public static func read()int{return 1}};class B extends A{public func value()int{return parent.read()}};func main(){}`, "static method read requires class-qualified access"},
		"inherited constraint field":  {`class A[T any]{public static count int};class B[T ~int] extends A[T]{};func main(){_=B[string].count}`, "does not satisfy"},
		"inherited constraint method": {`class A[T any]{public static func read()int{return 1}};class B[T ~int] extends A[T]{};func main(){_=B[string].read()}`, "does not satisfy"},
		"generic static arity":        {`class A[T any]{public static func read()int{return 1}};func main(){_=A.read()}`, "expects 1 type arguments"},
		"generic shared field":        {`class A[T any]{public static value T};func main(){}`, "cannot use class type parameters"},
		"static interface":            {`interface Named{func name()string};class A implements Named{public static func name()string{return "a"}};func main(){}`, "does not implement Named.name"},
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "main.ghi"), []byte("namespace main\n"+item.source), 0600); err != nil {
				t.Fatal(err)
			}
			err := compiler.Check(context.Background(), compiler.Options{Dir: dir})
			if err == nil || !strings.Contains(err.Error(), item.message) {
				t.Fatalf("want %q, got %v", item.message, err)
			}
		})
	}
}

func TestStaticInitializersAbstractDefaultsAndStack(t *testing.T) {
	runMatchSource(t, `namespace main
import strings "go:strings"
abstract class Base {
 public abstract func value(text string = "default") string
}
class Value extends Base {
 public override func value(text string = "child") string { return text }
}
class With_Underscore[T any] {
 private static func initial() int { return 7 }
 public static count int = With_Underscore[int].initial()
 public static func fail_here() { throw new Exception("failure") }
 public static func finish(value int = 0) {}
}
class IntChild[T ~int] extends With_Underscore[T] {}
func main() {
 var value Base = new Value()
 println(value.value(), IntChild[int].count)
 defer With_Underscore[int].finish()
 try { With_Underscore[int].fail_here() } catch err Exception {
  frame := err.stackTrace[0]
  println(strings.HasPrefix(frame.functionName, "main.With_Underscore.fail_here"), strings.HasSuffix(frame.file, ".ghi"))
 }
}
`, "default 7\ntrue true\n")
}

func TestStaticGoErrorsAndScheduling(t *testing.T) {
	runMatchSource(t, `namespace main
import strconv "go:strconv"
class Parse {
 public static func integer(text string = "42") int { return strconv.Atoi(text) }
 public static func send(done chan int, value int = 7) { done <- value }
}
func main() {
 println(Parse.integer())
 parse := Parse.integer
 try { parse("oops") } catch err GoError { println(err.code, err.message != "") }
 done := make(chan int, 1)
 go Parse.send(done)
 println(<-done)
 defer Parse.send(done, 9)
}
`, "42\n0 true\n7\n")
}

func TestStaticExpressionAnalysis(t *testing.T) {
	source := `namespace main
class Factory[T any] {
 public static count int
 public static func identity(value T) T { return value }
}
class IntFactory[T ~int] extends Factory[T] {}
func main() { _ = Factory[int].count; _ = IntFactory[int].count; _ = IntFactory[int].identity }
`
	dir := t.TempDir()
	filename := filepath.Join(dir, "main.ghi")
	if err := os.WriteFile(filename, []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	result := compiler.AnalyzeExpressionTypes(context.Background(), dir, filename, []byte(source))
	if len(result.Diagnostics) != 0 {
		t.Fatal(result.Diagnostics)
	}
	for expression, want := range map[string]string{
		"Factory[int].count":       "int",
		"IntFactory[int].count":    "int",
		"IntFactory[int].identity": "func(value int) int",
	} {
		start := strings.LastIndex(source, "= "+expression) + 2
		found := false
		for _, value := range result.ExpressionTypes {
			if value.Start == start && value.End == start+len(expression) {
				found = value.Type == want
				break
			}
		}
		if !found {
			t.Errorf("missing %s: %s in %v", expression, want, result.ExpressionTypes)
		}
	}
}

func TestGenericAbstractAndStatic(t *testing.T) {
	runMatchSource(t, `namespace main
abstract class Reader[T any] { public abstract func read(value T) T }
class TextReader extends Reader[string] { public override func read(value string) string { return value } }
class Factory[T any] {
 public static count int = 0
 public static func identity(value T) T { Factory[T].count++; return value }
}
class TextFactory extends Factory[string] {}
func main() {
 var reader Reader[string] = new TextReader()
 println(reader.read("hello"))
 println(Factory[int].identity(3), TextFactory.identity("text"), Factory[int].count)
 println(Factory[string].count)
}
`, "hello\n3 text 2\n2\n")
}

func TestModifierNamesRemainOrdinaryIdentifiers(t *testing.T) {
	runMatchSource(t, `namespace main
type Item[T any] struct { value T }
class Value {
 public static int
 public abstract Item[int]
 constructor() { this.static=4;this.abstract=Item[int]{value:7} }
}

func static()int{return 5}
func main(){abstract:=new Value();println(abstract.static,abstract.abstract.value,static())}
`, "4 7 5\n")
}

func TestModifierNamesAsFieldTypes(t *testing.T) {
	runMatchSource(t, `namespace main
type abstract int
type static int
type constructor int
class Value { public static abstract }
class Other { public abstract static }
class ConstructorType { public static constructor }
class Callbacks {
 public static func() int
 public abstract func() int
 constructor() { this.static = func() int { return 9 }; this.abstract = func() int { return 10 } }
}
func main() { println(new Value().static, new Other().abstract, new ConstructorType().static); c := new Callbacks(); println(c.static(), c.abstract()) }
`, "0 0 0\n9 10\n")
}

func TestStaticImportsAndNullableFields(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "library"), 0700); err != nil {
		t.Fatal(err)
	}
	for name, source := range map[string]string{
		"library/value.ghi": `namespace library
class Item {public value string;constructor(value string){this.value=value}}
abstract class Factory {
 public static item ?Item = nil
 public static func make(value string="imported") Item {return new Item(value)}
}
`,
		"main.ghi": `namespace main
import library.Factory as Maker
func main(){println(Maker.item==nil);Maker.item=Maker.make();item:=Maker.item;if item!=nil{println(item.value)}}
`,
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(source), 0600); err != nil {
			t.Fatal(err)
		}
	}
	result, err := compiler.Build(context.Background(), compiler.Options{Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	output, err := exec.Command(result.Executable).CombinedOutput()
	if err != nil || strings.ReplaceAll(string(output), "\r\n", "\n") != "true\nimported\n" {
		t.Fatalf("%v: %s", err, output)
	}
}
