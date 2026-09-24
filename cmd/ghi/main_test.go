package main

import (
	"debug/buildinfo"
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
	out, err := exec.Command(binary, "check", project).CombinedOutput()
	if err != nil || strings.TrimSpace(string(out)) != "Check passed" {
		t.Fatalf("check: %v; output: %s", err, out)
	}
	if _, err := os.Stat(filepath.Join(project, "bin")); !os.IsNotExist(err) {
		t.Fatalf("check created executable directory: %v", err)
	}
	for _, args := range [][]string{{"check", "-o", "output", project}, {"check", project, "--", "argument"}} {
		out, err := exec.Command(binary, args...).CombinedOutput()
		exit, ok := err.(*exec.ExitError)
		if !ok || exit.ExitCode() != 2 {
			t.Fatalf("invalid check arguments: %v; %s", err, out)
		}
	}
	debugBinary := filepath.Join(dir, "debug-program")
	if runtime.GOOS == "windows" {
		debugBinary += ".exe"
	}
	out, err = exec.Command(binary, "build", "--debug", "-o", debugBinary, project).CombinedOutput()
	if err != nil {
		t.Fatalf("debug build: %v; %s", err, out)
	}
	info, err := buildinfo.ReadFile(debugBinary)
	if err != nil {
		t.Fatal(err)
	}
	debugFlags := ""
	for _, setting := range info.Settings {
		if setting.Key == "-gcflags" {
			debugFlags = setting.Value
		}
	}
	if debugFlags != "all=-N -l" {
		t.Fatalf("debug build flags: %q", debugFlags)
	}
	out, err = exec.Command(binary, "run", project, "--", "argument with spaces").CombinedOutput()
	exit, ok := err.(*exec.ExitError)
	if !ok || exit.ExitCode() != 7 {
		t.Fatalf("exit: %v; output: %s", err, out)
	}
	if strings.TrimSpace(string(out)) != "argument with spaces" {
		t.Fatalf("output %q", out)
	}
	if err := os.WriteFile(filepath.Join(project, "main.ghi"), []byte("namespace main\nfunc main(){var value int = \"bad\";println(value)}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	out, err = exec.Command(binary, "check", project).CombinedOutput()
	exit, ok = err.(*exec.ExitError)
	if !ok || exit.ExitCode() != 1 || !strings.Contains(string(out), "main.ghi:2:") {
		t.Fatalf("bad source check: %v; %s", err, out)
	}
}
