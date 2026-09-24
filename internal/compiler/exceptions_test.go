package compiler

import (
	"context"
	"os/exec"
	"strings"
	"testing"
)

func TestTypedExceptionsAndFinallyWithReturn(t *testing.T) {
	got := runProgram(t, map[string]string{"main.ghi": `namespace main
import fmt "go:fmt"
class Missing extends Exception {
 constructor(message string) { parent(message) }
}
func value() int {
 try {
  throw Missing("gone")
 } catch err Missing {
  fmt.Println(err.message)
  return 7
 } catch err Exception {
  return 8
 } finally {
  fmt.Println("cleanup")
 }
}
func main() { fmt.Println(value()) }
`})
	if got != "gone\ncleanup\n7\n" {
		t.Fatalf("output %q", got)
	}
}

func TestFinallyPropagatesNestedLoopControl(t *testing.T) {
	got := runProgram(t, map[string]string{"main.ghi": `namespace main
import fmt "go:fmt"
func main() {
 for i:=0;i<4;i++ {
  try {
   try {
    if i==0 { continue }
    if i==2 { break }
    fmt.Println("body",i)
   } finally { fmt.Println("inner",i) }
  } finally { fmt.Println("outer",i) }
 }
}
`})
	expected := "inner 0\nouter 0\nbody 1\ninner 1\nouter 1\ninner 2\nouter 2\n"
	if got != expected {
		t.Fatalf("output %q", got)
	}
}

func TestGoErrorsAutomaticallyThrowAndPreserveCause(t *testing.T) {
	got := runProgram(t, map[string]string{"main.ghi": `namespace main
import fmt "go:fmt"
import os "go:os"
import errors "go:errors"
func main() {
 try {
  data := os.ReadFile("a-file-that-does-not-exist.ghi")
  fmt.Println(string(data))
 } catch err GoError {
  fmt.Println(errors.Is(err.cause,os.ErrNotExist))
  fmt.Println(err.code, err.typeName, len(err.stackTrace)>0)
 } finally { fmt.Println("done") }
}
`})
	if got != "true\n0 ghi.runtime.GoError true\ndone\n" {
		t.Fatalf("output %q", got)
	}
}

func TestRuntimePanicBypassesLanguageCatchButRunsFinally(t *testing.T) {
	dir := project(t, map[string]string{"main.ghi": `namespace main
import fmt "go:fmt"
func main() {
 try {
  values:=[]int{}
  fmt.Println(values[1])
 } catch err Exception { fmt.Println("incorrectly caught")
 } finally { fmt.Println("cleanup") }
}
`})
	result, err := Build(context.Background(), Options{Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(result.Executable).CombinedOutput()
	if err == nil || !strings.Contains(string(out), "cleanup") || strings.Contains(string(out), "incorrectly caught") {
		t.Fatalf("panic handling: %v %s", err, out)
	}
}

func TestExceptionMetadataAndRethrow(t *testing.T) {
	got := runProgram(t, map[string]string{"main.ghi": `namespace main
import fmt "go:fmt"
import strings "go:strings"
class Missing extends Exception {
 constructor(message string, code int = 0) { parent(message,code) }
}
func origin() { throw Missing("gone",404) }
func main() {
 plain:=Exception("default")
 fmt.Println(plain.code,plain.typeName,plain.message,len(plain.stackTrace))
 try {
  try { origin() } catch err Exception {
   frame:=err.stackTrace[0]
   fmt.Println(err.code,err.typeName,err.message,frame.functionName,strings.HasSuffix(frame.file,"main.ghi"),frame.line)
   throw err
  }
 } catch err Missing {
  frame:=err.stackTrace[0]
  fmt.Println(err.code,err.typeName,frame.functionName,frame.line)
 }
}
`})
	expected := "0 ghi.runtime.Exception default 0\n404 main.Missing gone main.origin true 7\n404 main.Missing main.origin 7\n"
	if got != expected {
		t.Fatalf("metadata output %q", got)
	}
}
