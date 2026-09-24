package cli_test

import (
	"bytes"
	"context"
	"ghi/internal/compiler"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestFormatStdinIsAnAtomicBufferOperation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	dir := t.TempDir()
	binary := filepath.Join(dir, "ghi")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	build := exec.CommandContext(ctx, "go", "build", "-o", binary, "../../cmd/ghi")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("CLI build: %v\n%s", err, output)
	}
	filename := filepath.Join(dir, "unsaved source.ghi")
	onDisk := []byte("not the editor buffer")
	if err := os.WriteFile(filename, onDisk, 0600); err != nil {
		t.Fatal(err)
	}
	source := "namespace main\nfunc main(){println(1)}\n"
	want, err := compiler.FormatSource(filename, []byte(source))
	if err != nil {
		t.Fatal(err)
	}
	invoke := func(input string, args ...string) (string, string, int) {
		t.Helper()
		command := exec.CommandContext(ctx, binary, args...)
		command.Dir = dir
		command.Stdin = strings.NewReader(input)
		var stdout, stderr bytes.Buffer
		command.Stdout, command.Stderr = &stdout, &stderr
		err := command.Run()
		code := 0
		if err != nil {
			if exit, ok := err.(*exec.ExitError); ok {
				code = exit.ExitCode()
			} else {
				t.Fatal(err)
			}
		}
		return stdout.String(), stderr.String(), code
	}
	out, errors, code := invoke(source, "fmt", "--stdin", "--filename", filename)
	if code != 0 || errors != "" || out != string(want) {
		t.Fatalf("format: exit%d stderr=%q stdout=%q", code, errors, out)
	}
	actual, err := os.ReadFile(filename)
	if err != nil || !bytes.Equal(actual, onDisk) {
		t.Fatal("format changed the on-disk buffer")
	}
	out, errors, code = invoke("namespace main\nfunc main( {", "fmt", "--stdin", "--filename", filename)
	if code != 1 || out != "" || !strings.Contains(errors, filename) {
		t.Fatalf("failure was not atomic: %d %q %q", code, out, errors)
	}
	for _, args := range [][]string{{"fmt", "--stdin", "--check"}, {"fmt", "--stdin", dir}, {"fmt", "--filename", filename}} {
		out, _, code := invoke(source, args...)
		if code != 2 || out != "" {
			t.Fatalf("invalid flags accepted: %v => %d %q", args, code, out)
		}
	}
}
