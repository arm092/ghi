package compiler

import (
	"bufio"
	"context"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCompiledHTTPBackend(t *testing.T) {
	source, err := os.ReadFile(filepath.Join("..", "..", "examples", "http", "main.ghi"))
	if err != nil {
		t.Fatal(err)
	}
	dir := project(t, map[string]string{"main.ghi": string(source)})
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	result, err := Build(ctx, Options{Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	command := exec.CommandContext(ctx, result.Executable, "127.0.0.1:0")
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	command.Stderr = os.Stderr
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { command.Process.Kill(); command.Wait() }()
	ready := make(chan string, 1)
	go func() {
		scanner := bufio.NewScanner(stdout)
		if scanner.Scan() {
			ready <- scanner.Text()
		} else {
			ready <- ""
		}
	}()
	var address string
	select {
	case address = <-ready:
	case <-ctx.Done():
		t.Fatal("server startup timed out")
	}
	if address == "" {
		t.Fatal("server exited before reporting its listening address")
	}
	client := &http.Client{Timeout: 5 * time.Second}
	for _, tc := range []struct {
		path   string
		status int
		body   string
	}{{"/hello?name=Ada", 200, "Hello, Ada!"}, {"/hello", 400, "name is required"}} {
		response, err := client.Get("http://" + address + tc.path)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(response.Body)
		response.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != tc.status || strings.TrimSpace(string(body)) != tc.body {
			t.Fatalf("%s: %d %q", tc.path, response.StatusCode, body)
		}
	}
}
