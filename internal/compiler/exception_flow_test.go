package compiler

import (
	"context"
	"testing"
)

func TestFinallyOverridesReturnAndRethrowsFromCatch(t *testing.T) {
	got := runProgram(t, map[string]string{"main.ghi": `namespace main
import fmt "go:fmt"
func override() int {
 try { return 1 } finally { return 2 }
}
func named() (result int) {
 try { return 10 } finally { result++ }
}
func nested() int {
 try { return 3 } finally {
  try { fmt.Println("inner") } finally { fmt.Println("nested cleanup") }
  fmt.Println("outer")
 }
}
func main() {
 fmt.Println(override(),named(),nested())
 try {
  try { throw new Exception("first") } catch e Exception { throw new Exception("second")
  } finally { fmt.Println("rethrow cleanup") }
 } catch e Exception { fmt.Println(e.message) }
}
`})
	expected := "inner\nnested cleanup\nouter\n2 11 3\nrethrow cleanup\nsecond\n"
	if got != expected {
		t.Fatalf("output %q", got)
	}
}

func TestExceptionsInsideAnonymousFunctions(t *testing.T) {
	got := runProgram(t, map[string]string{"main.ghi": `namespace main
import fmt "go:fmt"
import strconv "go:strconv"
func main() {
 f:=func(value string) int {
  try { return strconv.Atoi(value) } catch e GoError { return -1 }
 }
 fmt.Println(f("12"),f("bad"))
}
`})
	if got != "12 -1\n" {
		t.Fatalf("output %q", got)
	}
}

func TestExceptionsInGlobalCallbacksAndStatementHeaders(t *testing.T) {
	got := runProgram(t, map[string]string{"main.ghi": `namespace main
import fmt "go:fmt"
var global=func()int{try{return 7}finally{}}
func main(){
 if value:=func()int{try{return 1}finally{}}();value==1{fmt.Println(value)}
 for i:=func()int{try{return 0}finally{}}();i<2;i=func()int{try{return i+1}finally{}}(){fmt.Println(i)}
 switch value:=func()int{try{return 3}finally{}}();value {case 3:fmt.Println(value)}
 channel:=make(chan int,1)
 channel<-func()int{try{return 4}finally{}}()
 select {case value:=<-func()chan int{try{return channel}finally{}}():fmt.Println(value)}
 fmt.Println(global())
}
`})
	if got != "1\n0\n1\n3\n4\n7\n" {
		t.Fatalf("output %q", got)
	}
}

func TestImportedFunctionAliasesPreserveErrorBridge(t *testing.T) {
	got := runProgram(t, map[string]string{"main.ghi": `namespace main
import fmt "go:fmt"
import os "go:os"
func main(){
 read:=os.ReadFile
 alias:=read
 try{_=alias("missing-no-file.ghi")}catch err GoError {fmt.Println("caught")}
}

`})
	if got != "caught\n" {
		t.Fatalf("output %q", got)
	}
}

// Calls bridge trailing errors consistently through aliases, parameters and fields.
func TestNativeErrorBridgeFunctionValueBoundaries(t *testing.T) {
	for _, test := range []struct {
		name, declarations, setup, call string
	}{
		{"direct", "", "", "io.ReadFull"},
		{"alias", "", "read := io.ReadFull; alias := read", "alias"},
		{"parameter", `func invoke(read func(io.Reader, []byte) (int, error), text string) {
 buffer := make([]byte, 1)
 try { count := read(strings.NewReader(text), buffer); println("success", count, string(buffer))
 } catch err GoError { println("caught", errors.Is(err.cause, io.EOF)) }
}`, "", ""},
		{"field", `class Reader {
 public read func(io.Reader, []byte) (int, error)
 constructor(read func(io.Reader, []byte) (int, error)) { this.read = read }
}`, "reader := new Reader(io.ReadFull)", "reader.read"},
	} {
		t.Run(test.name, func(t *testing.T) {
			body := test.setup + `
 for _, text := range []string{"x", ""} {
  buffer := make([]byte, 1)
  try { count := ` + test.call + `(strings.NewReader(text), buffer); println("success", count, string(buffer))
  } catch err GoError { println("caught", errors.Is(err.cause, io.EOF)) }
 }`
			if test.name == "parameter" {
				body = `invoke(io.ReadFull, "x"); invoke(io.ReadFull, "")`
			}
			got := runProgram(t, map[string]string{"main.ghi": `namespace main
import io "go:io"
import strings "go:strings"
import errors "go:errors"
` + test.declarations + "\nfunc main() {\n" + body + "\n}\n"})
			want := "success 1 x\ncaught true\n"
			if got != want {
				t.Fatalf("output %q", got)
			}
		})
	}
}
func TestExceptionTypesAndReturnCoverage(t *testing.T) {
	for name, body := range map[string]string{
		"throw primitive":       `func main(){ throw 42 }`,
		"missing return":        `func f() int { try {} finally {} }; func main(){}`,
		"catch unrelated class": `class Item {}; func main(){try {throw new Exception("x")} catch e Item {}}`,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Build(context.Background(), Options{Dir: project(t, map[string]string{"main.ghi": "namespace main\n" + body})})
			if err == nil {
				t.Fatal("invalid exception/control-flow program accepted")
			}
		})
	}
}

func TestNativeErrorBridgeMixedOrigins(t *testing.T) {
	got := runProgram(t, map[string]string{"main.ghi": `namespace main
import io "go:io"
import strings "go:strings"
import errors "go:errors"
func explicit(reader io.Reader, buffer []byte) (int, error) { return 9, io.EOF }
func invoke(read func(io.Reader, []byte) (int, error), name string) {
 try { read(strings.NewReader(""), make([]byte, 1)); println(name, "raw")
 } catch err GoError { println(name, "caught", errors.Is(err.cause, io.EOF)) }
}

class Reader {
 public read func(io.Reader, []byte) (int, error)
 constructor(read func(io.Reader, []byte) (int, error)) { this.read = read }
}
func main() {
 invoke(io.ReadFull, "native parameter")
 invoke(explicit, "ghi parameter")
 native := new Reader(io.ReadFull)
 ghi := new Reader(explicit)
 try { native.read(strings.NewReader(""), make([]byte, 1)); println("native field raw")
 } catch err GoError { println("native field caught") }
 try { ghi.read(strings.NewReader(""), make([]byte, 1)); println("ghi field raw")
 } catch err GoError { println("ghi field caught") }
}
`})
	if got != "native parameter caught true\nghi parameter caught true\nnative field caught\nghi field caught\n" {
		t.Fatalf("output %q", got)
	}
}

func TestTrailingErrorCallbacksYieldSingleResult(t *testing.T) {
	for name, declarations := range map[string]string{
		"parameter": `func invoke(read func(io.Reader, []byte) (int, error)) int {
 return read(strings.NewReader("x"), make([]byte, 1))
}
func main() { println(invoke(io.ReadFull)) }`,
		"field": `class Reader {
 public read func(io.Reader, []byte) (int, error)
 constructor(read func(io.Reader, []byte) (int, error)) { this.read = read }
}
func main() {
 reader := new Reader(io.ReadFull)
 count := reader.read(strings.NewReader("x"), make([]byte, 1))
 println(count)
}`,
	} {
		t.Run(name, func(t *testing.T) {
			got := runProgram(t, map[string]string{"main.ghi": `namespace main
import io "go:io"
import strings "go:strings"
` + declarations})
			if got != "1\n" {
				t.Fatalf("output %q", got)
			}
		})
	}
}

func TestTrailingErrorBridgeReassignedAlias(t *testing.T) {
	got := runProgram(t, map[string]string{"main.ghi": `namespace main
import io "go:io"
import strings "go:strings"
import errors "go:errors"
func explicit(reader io.Reader, buffer []byte) (int, error) { return 9, io.EOF }
func main() {
 read := io.ReadFull
 read = explicit
 try { read(strings.NewReader(""), make([]byte, 1)); println("raw")
 } catch err GoError { println("caught", errors.Is(err.cause, io.EOF)) }
}
`})
	if got != "caught true\n" {
		t.Fatalf("output %q", got)
	}
}

func TestTrailingErrorBridgeGhiOperationsAndSyntheticAccess(t *testing.T) {
	got := runProgram(t, map[string]string{"main.ghi": `namespace main
import io "go:io"
import errors "go:errors"
func result[T any](value T, fail bool) (T, error) {
 if fail { return value, io.EOF }
 return value, nil
}
func errorData(fail bool) (error, error) {
 if fail { return io.EOF, io.ErrUnexpectedEOF }
 return io.EOF, nil
}
func finish(fail bool) error { if fail { return io.EOF }; return nil }
func protected() (int, error) {
 try { return 5, io.EOF } finally { println("finally") }
}
func later() func() error { return func() error { return io.EOF } }
class Operation {
 public cause error
 constructor() { this.cause = io.EOF }
 public func value(fail bool) (int, error) {
  if fail { return 0, this.cause }
  return 8, nil
 }
}
class Inherited extends Operation { constructor() { parent() } }
func main() {
 println(result(7, false), result("x", false))
 selected := match true { true => io.EOF, default => nil, }
 conditional := true ? io.EOF : nil
 println("error data", errors.Is(selected, io.EOF), errors.Is(conditional, io.EOF))
 println("tuple data", errors.Is(errorData(false), io.EOF))
 try { errorData(true) } catch err GoError { println("tuple error", errors.Is(err.cause, io.ErrUnexpectedEOF)) }
 finish(false)
 try { result(0, true) } catch err GoError { println("generic", errors.Is(err.cause, io.EOF)) }
 try { finish(true) } catch err GoError { println("error only", errors.Is(err.cause, io.EOF)) }
 try { protected() } catch err GoError { println("protected", errors.Is(err.cause, io.EOF)) }
 op := new Inherited()
 println(op.value(false), errors.Is(op.cause, io.EOF))
 try { op.value(true) } catch err GoError { println("method", errors.Is(err.cause, io.EOF)) }
 bound := op.value
 try { bound(true) } catch err GoError { println("bound", errors.Is(err.cause, io.EOF)) }
 try { later()() } catch err GoError { println("returned callback", errors.Is(err.cause, io.EOF)) }
 callbacks := []func() error{func() error { return io.EOF }}
 try { callbacks[0]() } catch err GoError { println("indexed callback", errors.Is(err.cause, io.EOF)) }
 try { (func() error { return io.EOF })() } catch err GoError { println("literal callback", errors.Is(err.cause, io.EOF)) }
}
`})
	want := "7 x\nerror data true true\ntuple data true\ntuple error true\ngeneric true\nerror only true\nfinally\nprotected true\n8 true\nmethod true\nbound true\nreturned callback true\nindexed callback true\nliteral callback true\n"
	if got != want {
		t.Fatalf("output %q", got)
	}
}

func TestTrailingErrorBridgeGhiDeferredCapture(t *testing.T) {
	got := runProgram(t, map[string]string{"main.ghi": `namespace main
import io "go:io"
import errors "go:errors"
var trace string
func cleanup(value int = 7) error { println("default", value); return nil }
func fail() error { println("deferred fail"); return io.EOF }
func errorData() (error, error) { println("deferred error data"); return io.EOF, nil }
func argument() int { trace += "arg "; return 2 }
func variadic(values ...int) error { println("variadic", values[0]); return nil }
class Operation {
 public value int
 constructor(value int) { this.value = value }
 public func cleanup(value int) error { println("method", this.value, value); return nil }
}
func deferred() {
 op := new Operation(3)
 defer fail()
 defer errorData()
 defer cleanup()
 values := []int{1}
 defer variadic(values...)
 defer op.cleanup(argument())
 op = new Operation(9)
 values[0] = 4
 trace += "body"
 println(trace)
}
func main() {
 try { deferred() } catch err GoError { println("caught", errors.Is(err.cause, io.EOF)) }
}
`})
	want := "arg body\nmethod 3 2\nvariadic 4\ndefault 7\ndeferred error data\ndeferred fail\ncaught true\n"
	if got != want {
		t.Fatalf("output %q", got)
	}
}

func TestTrailingErrorBridgeNativeRawBodies(t *testing.T) {
	got := runProgram(t, map[string]string{
		"go.mod":     "module example.test/bridge\n\ngo 1.26.0\n\nrequire example.test/raw v0.0.0\nreplace example.test/raw => ./raw\n",
		"raw/go.mod": "module example.test/raw\n\ngo 1.26.0\n",
		"raw/reader.go": `package raw
import "io"
import "strings"
type Reader struct{}
func (Reader) Failure() error { return io.EOF }
func (Reader) ReferenceFailure() error { return io.EOF }
func RawResults() (int, error) {
 // Native Go source keeps the original result tuple internally.
 count, err := io.ReadFull(strings.NewReader("x"), make([]byte, 1))
 return count, err
}
`,
		"main.ghi": `namespace main
import raw "go:example.test/raw"
import io "go:io"
import errors "go:errors"
func main() {
 println(raw.RawResults())
 reader := raw.Reader{}
 try { reader.Failure() } catch err GoError { println("native method", errors.Is(err.cause, io.EOF)) }
 try { reader.ReferenceFailure() } catch err GoError { println("native reference method", errors.Is(err.cause, io.EOF)) }
}
`,
	})
	if got != "1\nnative method true\nnative reference method true\n" {
		t.Fatalf("output %q", got)
	}
}
