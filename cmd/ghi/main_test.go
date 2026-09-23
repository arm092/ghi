package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestRunForwardsArgumentsAndExitStatus(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, "ghi")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	if out, err := exec.Command("go", "build", "-o", binary, ".").CombinedOutput(); err != nil {
		t.Fatalf("build CLI: %v %s", err, out)
	}
	project := filepath.Join(dir, "project with spaces")
	if err := os.MkdirAll(project, 0755); err != nil {
		t.Fatal(err)
	}
	source := `namespace main
import fmt "go:fmt"
import os "go:os"
func main() { fmt.Println(os.Args[1]); os.Exit(7) }
`
	if err := os.WriteFile(filepath.Join(project, "main.ghi"), []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(binary, "run", project, "--", "argument with spaces").CombinedOutput()
	exit, ok := err.(*exec.ExitError)
	if !ok || exit.ExitCode() != 7 {
		t.Fatalf("exit: %v; output: %s", err, out)
	}
	if strings.TrimSpace(string(out)) != "argument with spaces" {
		t.Fatalf("output %q", out)
	}
}
