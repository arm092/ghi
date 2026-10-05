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

// Native origin currently survives aliases, while an explicitly typed callback
// parameter or field retains the raw Go signature. Keep both boundaries visible
// until function-value capture semantics are defined for mixed native/Ghi values.
func TestNativeErrorBridgeFunctionValueBoundaries(t *testing.T) {
	for _, test := range []struct {
		name, declarations, setup, call string
	}{
		{"direct", "", "", "io.ReadFull"},
		{"alias", "", "read := io.ReadFull; alias := read", "alias"},
		{"parameter", `func invoke(read func(io.Reader, []byte) (int, error), text string) {
 buffer := make([]byte, 1)
 count, err := read(strings.NewReader(text), buffer)
 println(count, errors.Is(err, io.EOF), string(buffer))
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
			if test.name == "field" {
				body = test.setup + `
 for _, text := range []string{"x", ""} {
  buffer := make([]byte, 1)
  count, err := reader.read(strings.NewReader(text), buffer)
  println(count, errors.Is(err, io.EOF), string(buffer))
 }`
			}
			got := runProgram(t, map[string]string{"main.ghi": `namespace main
import io "go:io"
import strings "go:strings"
import errors "go:errors"
` + test.declarations + "\nfunc main() {\n" + body + "\n}\n"})
			want := "success 1 x\ncaught true\n"
			if test.name == "parameter" || test.name == "field" {
				want = "1 false x\n0 true \x00\n"
			}
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
	if got != "native parameter raw\nghi parameter raw\nnative field raw\nghi field raw\n" {
		t.Fatalf("output %q", got)
	}
}

func TestNativeErrorBridgeRawCallbacksRejectSingleResult(t *testing.T) {
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
			dir := project(t, map[string]string{"main.ghi": `namespace main
import io "go:io"
import strings "go:strings"
` + declarations})
			_, err := Build(context.Background(), Options{Dir: dir})
			if err == nil {
				t.Fatal("raw callback signature unexpectedly yielded one result")
			}
			t.Log(err)
		})
	}
}

// Characterize a confirmed limitation: native provenance is monotonic for an
// alias, so assigning a Ghi function later still bridges its explicit error.
// This is current behavior, not the intended function-value contract.
func TestNativeErrorBridgeReassignedAliasCurrentBehavior(t *testing.T) {
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
