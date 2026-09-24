package compiler

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"testing"
)

func TestDiagnosticLocationsThroughExceptions(t *testing.T) {
	for _, source := range []string{
		`namespace main
func main(){
 try {
  throw Exception("failed")
 } catch e Exception {
  println(missing)
 }
}`,
		`namespace main
func main(){
 try {
  println("ok")
 } finally {
  println("done")
 }
 println(missing)
}`,
		`namespace main
class User {
 public func value() {
  try {
   throw Exception("bad")
  } catch e Exception {
   println(missing)
  }
 }
}
func main(){User().value()}`,
	} {
		line := strings.Count(source[:strings.Index(source, "missing")], "\n") + 1
		dir := project(t, map[string]string{"main.ghi": source})
		err := Check(context.Background(), Options{Dir: dir})
		if err == nil || !strings.Contains(err.Error(), fmt.Sprintf("main.ghi:%d:", line)) {
			t.Errorf("expected source line %d, got %v", line, err)
		}
	}
}

func TestRuntimeDiagnosticUsesGhiFrames(t *testing.T) {
	source := `namespace main
class Worker {
 public func execute() {
  throw Exception("failure from worker") // explain failure
 }
}
func main(){Worker().execute()}
`
	dir := project(t, map[string]string{"main.ghi": source})
	result, err := Build(context.Background(), Options{Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(result.Executable).CombinedOutput()
	if err == nil {
		t.Fatal("uncaught exception succeeded")
	}
	text := string(out)
	if !strings.Contains(text, "failure from worker") || !strings.Contains(text, "main.ghi:4") || !strings.Contains(text, "Worker.execute") {
		t.Fatalf("missing source trace: %s", text)
	}
	if strings.Contains(text, "GhiBody_") || strings.Contains(text, ".generated") {
		t.Fatalf("generated implementation leaked: %s", text)
	}
}
