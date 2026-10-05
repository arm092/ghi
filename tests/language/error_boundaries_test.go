package language_test

import (
	"context"
	"ghi/internal/compiler"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDefersKeepUserFunctionScopeAcrossExceptionBlocks(t *testing.T) {
	runMatchSource(t, `namespace main
import io "go:io"
import strconv "go:strconv"
import errors "go:errors"
func argument(value string) string { println("capture",value); return value }
func record(value string = "default") error { println("deferred",value); return nil }
func pair() (string,string) { println("pair"); return "tuple","args" }
func records(values ...string) error { println("deferred",values[0],values[1]); return nil }
func fail() error { println("failure"); return io.EOF }
func scope() {
 defer record("outside first")
 try {
  alias:=record
  defer alias(argument("alias"))
  alias=func(value string) error { println("wrong"); return nil }
  defer record()
  defer records(pair())
  values:=[]string{"slice","before"}
  defer records(values...)
  values[1]="after"
  defer fail()
  println("try body")
  throw new Exception("caught")
 } catch e Exception {
  defer record("catch")
  println("caught")
 } finally {
  defer record("finally")
  println("finally")
 }
 defer record("outside last")
 println("after try")
}
func named() (result int) {
 try { defer func() { result++ }(); return 4 } finally { println("named finally") }
}
func nativeFailure() {
 try { defer strconv.Atoi("invalid"); println("native try") } catch e GoError { println("wrong catch") }
 println("native after")
}
func nested() {
 try {
  defer record("outer")
  func() {
   try { defer record("inner"); println("inner body") } finally {}
   println("inner after")
  }()
  println("outer body")
 } finally {}
 println("outer after")
}
func main() {
 try { scope() } catch e GoError { println("caller",errors.Is(e.cause,io.EOF)) }
 println("named",named())
 try { nativeFailure() } catch e GoError { println("native caller") }
 nested()
}
`, "capture alias\npair\ntry body\ncaught\nfinally\nafter try\ndeferred outside last\ndeferred finally\ndeferred catch\nfailure\ndeferred slice after\ndeferred tuple args\ndeferred default\ndeferred alias\ndeferred outside first\ncaller true\nnamed finally\nnamed 5\nnative try\nnative after\nnative caller\ninner body\ninner after\ndeferred inner\nouter body\nouter after\ndeferred outer\n")
}

func TestScopedDefersPreserveZeroArgumentRecover(t *testing.T) {
	runMatchSource(t, `namespace main
func fail() {
 defer func() { println("recovered",recover()!=nil) }()
 try { defer println("last"); panic("failure") } finally { println("finally") }
}
func main() { fail(); println("alive") }
`, "finally\nlast\nrecovered true\nalive\n")
}

func TestScopedDefersPreserveArgumentTakingRecover(t *testing.T) {
	runMatchSource(t, `namespace main
func recovery(label string) { println(label,recover()!=nil) }
func fail() {
 defer recovery("recovered")
 try { defer println("last"); println("try") } finally { println("finally") }
 panic("failure")
}
func main() { fail(); println("alive") }
`, "try\nfinally\nlast\nrecovered true\nalive\n")
}

func TestNativeAsynchronousCallbackReportsOriginalExceptionMetadata(t *testing.T) {
	dir := t.TempDir()
	source := `namespace main
import time "go:time"
class Failure extends Exception { constructor() { parent("callback failure",73) } }
func origin() { throw new Failure() }
func main() {
 time.AfterFunc(time.Millisecond,func() {
  try { origin() } catch e Exception { throw e }
 })
 time.Sleep(time.Second)
}
`
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
	if err == nil {
		t.Fatal("uncaught callback succeeded")
	}
	if failure, ok := err.(*exec.ExitError); !ok || failure.ExitCode() != 2 {
		t.Fatalf("callback exit: %v\n%s", err, output)
	}
	for _, want := range []string{"main.Failure (code 73): callback failure", "at main.origin (", "main.ghi:4)"} {
		if !strings.Contains(string(output), want) {
			t.Fatalf("missing %q in %s", want, output)
		}
	}
}

func TestNativeSynchronousCallbackStillPropagatesToGhiCatch(t *testing.T) {
	runMatchSource(t, `namespace main
import sync "go:sync"
func main() {
 once:=&sync.Once{}
 try { once.Do(func() { throw new Exception("synchronous",17) }) }
 catch e Exception { println(e.message,e.code) }
 println("alive")
}
`, "synchronous 17\nalive\n")
}
