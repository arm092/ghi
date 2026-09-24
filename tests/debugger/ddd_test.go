package debugger_test

import (
	"context"
	"ghi/internal/compiler"
	"net"
	"net/http"
	"net/rpc"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDelveDDDController(t *testing.T) {
	dlv, dir := os.Getenv("GHI_TEST_DLV"), os.Getenv("GHI_TEST_DDD")
	if dlv == "" || dir == "" {
		t.Skip("set GHI_TEST_DLV and GHI_TEST_DDD for the DDD debugger integration")
	}
	dir, err := filepath.Abs(dir)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	t.Cleanup(cancel)
	built, err := compiler.Build(ctx, compiler.Options{Dir: dir, Output: filepath.Join(t.TempDir(), "ddd-debug"), Debug: true})
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	listener.Close()
	client := startDelve(t, ctx, dlv, built.Executable, dir, []string{"GHI_DDD_ADDR=" + address, "GHI_DDD_DB=" + filepath.Join(t.TempDir(), "debug.db"), "GHI_DDD_REQUEST_TIMEOUT=1m"})
	file := filepath.Join(dir, "presentation/httpapi/users.ghi")
	source, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	line := 0
	for i, text := range strings.Split(string(source), "\n") {
		if strings.Contains(text, "item := this.service.Create(") {
			line = i + 1
			break
		}
	}
	if line == 0 {
		t.Fatal("controller breakpoint statement missing")
	}
	if err := client.Call("RPCServer.CreateBreakpoint", map[string]any{"Breakpoint": map[string]any{"file": filepath.ToSlash(file), "line": line}}, &map[string]any{}); err != nil {
		t.Fatal(err)
	}
	var stop struct {
		State struct {
			CurrentThread struct {
				File string
				Line int
			}
		}
	}
	running := client.Go("RPCServer.Command", map[string]any{"name": "continue"}, &stop, make(chan *rpc.Call, 1))
	httpClient := &http.Client{Timeout: 20 * time.Second}
	ready := false
	for until := time.Now().Add(15 * time.Second); time.Now().Before(until); {
		response, err := httpClient.Get("http://" + address + "/health")
		if err == nil {
			response.Body.Close()
			if response.StatusCode == 200 {
				ready = true
				break
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !ready {
		t.Fatal("DDD server did not become ready under Delve")
	}
	requestDone := make(chan int, 1)
	go func() {
		response, err := httpClient.Post("http://"+address+"/api/v1/users/", "application/json", strings.NewReader(`{"name":"Debug User","email":"debug@example.com"}`))
		if err != nil {
			requestDone <- 0
			return
		}
		defer response.Body.Close()
		requestDone <- response.StatusCode
	}()
	select {
	case result := <-running.Done:
		if result.Error != nil {
			t.Fatal(result.Error)
		}
	case <-ctx.Done():
		t.Fatal("controller breakpoint timed out")
	}
	if stop.State.CurrentThread.Line != line || !strings.HasSuffix(filepath.ToSlash(stop.State.CurrentThread.File), "presentation/httpapi/users.ghi") {
		t.Fatalf("controller source stop: %+v", stop)
	}
	var input struct{ Variable variable }
	if err := client.Call("RPCServer.Eval", map[string]any{"Scope": map[string]int{"GoroutineID": -1, "Frame": 0}, "Expr": "input.Name", "Cfg": map[string]any{"MaxStringLen": 128}}, &input); err != nil {
		t.Fatal(err)
	}
	if strings.Trim(input.Variable.Value, "\"") != "Debug User" {
		t.Fatalf("request local: %+v", input)
	}
	if err := client.Call("RPCServer.Command", map[string]any{"name": "next"}, &stop); err != nil {
		t.Fatal(err)
	}
	if stop.State.CurrentThread.Line != line+1 {
		t.Fatalf("step over service/repository: %+v", stop)
	}
	client.Go("RPCServer.Command", map[string]any{"name": "continue"}, &map[string]any{}, make(chan *rpc.Call, 1))
	select {
	case status := <-requestDone:
		if status != 201 {
			t.Fatalf("POST status after debugger resume: %d", status)
		}
	case <-ctx.Done():
		t.Fatal("request did not finish after resume")
	}
	// Stop while the server is running, not suspended at a breakpoint. Closing
	// Delve alone can leave the traced server and its inherited pipes alive.
	if err := client.Call("RPCServer.Command", map[string]any{"name": "halt"}, &map[string]any{}); err != nil {
		t.Fatal(err)
	}
	if err := client.Call("RPCServer.Detach", map[string]any{"Kill": true}, &map[string]any{}); err != nil {
		t.Fatal(err)
	}
	for until := time.Now().Add(3 * time.Second); ; {
		connection, err := net.DialTimeout("tcp", address, 100*time.Millisecond)
		if err != nil {
			break
		}
		connection.Close()
		if time.Now().After(until) {
			t.Fatal("DDD HTTP server remains alive after debugger Stop")
		}
		time.Sleep(50 * time.Millisecond)
	}
}
