package debugger_test

import (
	"bytes"
	"context"
	"ghi/internal/compiler"
	"net"
	"net/rpc"
	"net/rpc/jsonrpc"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// CI sets GHI_TEST_DLV to a pinned, real Delve executable on each platform.
func TestDelveGhiSourcesObjectsAndExceptions(t *testing.T) {
	dlv := os.Getenv("GHI_TEST_DLV")
	if dlv == "" {
		t.Skip("set GHI_TEST_DLV to run the real debugger integration")
	}
	dir := t.TempDir()
	source := `namespace main
class Counter {
 public value int
 constructor(value int) { this.value = value }
 public func add(amount int) int {
  this.value += amount
  return this.value
 }
}
func main() {
 counter := new Counter(7)
 answer := counter.add(5)
 println(answer)
 try {
  throw new Exception("debug exception")
 } catch err Exception {
  println(err.message)
  println(err.code)
 }
}
`
	file := filepath.Join(dir, "main.ghi")
	if err := os.WriteFile(file, []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	t.Cleanup(cancel)
	built, err := compiler.Build(ctx, compiler.Options{Dir: dir, Debug: true})
	if err != nil {
		t.Fatal(err)
	}
	client := startDelve(t, ctx, dlv, built.Executable, dir, nil)
	call := func(method string, params, reply any) {
		t.Helper()
		if err := client.Call("RPCServer."+method, params, reply); err != nil {
			t.Fatalf("%s: %v", method, err)
		}
	}
	for _, line := range []int{6, 13, 17} {
		call("CreateBreakpoint", map[string]any{"Breakpoint": map[string]any{"file": filepath.ToSlash(file), "line": line}}, &map[string]any{})
	}
	type location struct {
		File string
		Line int
	}
	type state struct {
		State struct {
			CurrentThread location
			Exited        bool
		}
	}
	resume := func(name string, want int) {
		t.Helper()
		var result state
		call("Command", map[string]any{"name": name}, &result)
		if result.State.Exited || result.State.CurrentThread.Line != want || filepath.Base(result.State.CurrentThread.File) != "main.ghi" {
			t.Fatalf("%s: expected main.ghi:%d, got %+v", name, want, result)
		}
	}
	scope := map[string]any{"GoroutineID": -1, "Frame": 0}
	cfg := map[string]any{"FollowPointers": true, "MaxVariableRecurse": 6, "MaxStringLen": 128, "MaxArrayValues": 20, "MaxStructFields": -1}
	eval := func(expression string) variable {
		t.Helper()
		var result struct{ Variable variable }
		call("Eval", map[string]any{"Scope": scope, "Expr": expression, "Cfg": cfg}, &result)
		return result.Variable
	}
	resume("continue", 6)
	if v := eval("amount"); v.Value != "5" {
		t.Fatalf("argument: %+v", v)
	}
	resume("next", 7)
	if v := eval("this"); !hasValue(v, "F_6d61696e_Counter_value", "12") {
		t.Fatalf("object: %+v", v)
	}
	var stack struct{ Locations []location }
	call("Stacktrace", map[string]any{"Id": -1, "Depth": 8}, &stack)
	if len(stack.Locations) == 0 || stack.Locations[0].Line != 7 {
		t.Fatalf("stack: %+v", stack)
	}
	resume("continue", 13)
	if v := eval("answer"); v.Value != "12" {
		t.Fatalf("local: %+v", v)
	}
	resume("continue", 17)
	caught := eval("err")
	if !hasValue(caught, "F_6768692e72756e74696d65_Exception_message", "debug exception") || !hasValue(caught, "F_6768692e72756e74696d65_Exception_code", "0") {
		t.Fatalf("exception data: %+v", caught)
	}
	if !hasField(caught, "F_6768692e72756e74696d65_Exception_stackTrace") {
		t.Fatalf("exception stack missing: %+v", caught)
	}
	call("Detach", map[string]any{"Kill": true}, &map[string]any{})
}

type variable struct {
	Name, Value, Type string
	Children          []variable
}

func hasValue(v variable, name, value string) bool {
	if v.Name == name && strings.Trim(v.Value, "\"") == value {
		return true
	}
	for _, child := range v.Children {
		if hasValue(child, name, value) {
			return true
		}
	}
	return false
}
func hasField(v variable, name string) bool {
	if v.Name == name {
		return true
	}
	for _, child := range v.Children {
		if hasField(child, name) {
			return true
		}
	}
	return false
}

func startDelve(t *testing.T, ctx context.Context, dlv, executable, dir string, env []string) *rpc.Client {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	listener.Close()
	var output bytes.Buffer
	command := exec.CommandContext(ctx, dlv, "exec", executable, "--headless", "--api-version=2", "--listen="+address)
	command.Dir = dir
	command.Env = append(os.Environ(), env...)
	command.Stdout, command.Stderr = &output, &output
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		command.Process.Kill()
		command.Wait()
		if t.Failed() {
			t.Log(output.String())
		}
	})
	var connection net.Conn
	for until := time.Now().Add(15 * time.Second); time.Now().Before(until); {
		connection, err = net.DialTimeout("tcp", address, 100*time.Millisecond)
		if err == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if connection == nil {
		t.Fatalf("connect to Delve: %v", err)
	}
	t.Cleanup(func() { connection.Close() })
	connection.SetDeadline(time.Now().Add(45 * time.Second))
	client := jsonrpc.NewClient(connection)
	t.Cleanup(func() {
		connection.SetDeadline(time.Now().Add(3 * time.Second))
		_ = client.Call("RPCServer.Detach", map[string]any{"Kill": true}, &map[string]any{})
	})
	return client
}
