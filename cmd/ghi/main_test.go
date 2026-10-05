package main

import (
	"bufio"
	"debug/buildinfo"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
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

func TestRunWatchRestartsAndKeepsProcessOnBuildFailure(t *testing.T) {
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
	main := filepath.Join(project, "main.ghi")
	write := func(source string) {
		t.Helper()
		if err := os.WriteFile(main, []byte(source), 0644); err != nil {
			t.Fatal(err)
		}
	}
	source := func(version string) string {
		return fmt.Sprintf(`namespace main
import fmt "go:fmt"
import os "go:os"
import time "go:time"
func main() {
    for { fmt.Println("%s", os.Getpid(), os.Args[1]); time.Sleep(100 * time.Millisecond) }
}
`, version)
	}
	write(source("first"))
	for _, args := range [][]string{{"build", "--watch", project}, {"run", "--watch", "-o", filepath.Join(dir, "output"), project}} {
		out, err := exec.Command(binary, args...).CombinedOutput()
		exit, ok := err.(*exec.ExitError)
		if !ok || exit.ExitCode() != 2 {
			t.Fatalf("invalid watch arguments %v: %v; %s", args, err, out)
		}
	}
	command := exec.Command(binary, "run", "--watch", "--debug", project, "--", "--watch argument")
	output, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	command.Stderr = command.Stdout
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if runtime.GOOS == "windows" {
			_ = exec.Command("taskkill", "/PID", fmt.Sprint(command.Process.Pid), "/T", "/F").Run()
		} else {
			_ = command.Process.Signal(os.Interrupt)
		}
		_ = command.Wait()
	})
	lines := make(chan string, 128)
	go func() {
		defer close(lines)
		scanner := bufio.NewScanner(output)
		for scanner.Scan() {
			lines <- scanner.Text()
		}
	}()
	waitFor := func(prefix string) string {
		t.Helper()
		timer := time.NewTimer(90 * time.Second)
		defer timer.Stop()
		var seen []string
		for {
			select {
			case line, ok := <-lines:
				if !ok {
					t.Fatalf("watch exited before %q: %s", prefix, strings.Join(seen, "\n"))
				}
				seen = append(seen, line)
				if strings.HasPrefix(line, prefix) {
					return line
				}
			case <-timer.C:
				t.Fatalf("timeout waiting for %q: %s", prefix, strings.Join(seen, "\n"))
			}
		}
	}
	first := waitFor("first ")
	if !strings.HasSuffix(first, " --watch argument") {
		t.Fatalf("program arguments were not forwarded: %s", first)
	}
	write("namespace main\nfunc main(){var value int = \"bad\";println(value)}\n")
	waitFor("[watch] build failed:")
	if heartbeat := waitFor("first "); heartbeat != first {
		t.Fatalf("failed build replaced process: %q; original %q", heartbeat, first)
	}
	write(source("second"))
	second := waitFor("second ")
	if strings.Fields(second)[1] == strings.Fields(first)[1] || !strings.HasSuffix(second, " --watch argument") {
		t.Fatalf("successful edit did not replace process and preserve arguments: first=%q second=%q", first, second)
	}
}
