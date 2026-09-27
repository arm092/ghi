package cli_test

import (
	"bytes"
	"context"
	"ghi/internal/watch"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type watchLog struct {
	sync.Mutex
	data bytes.Buffer
}

func (b *watchLog) Write(p []byte) (int, error) { b.Lock(); defer b.Unlock(); return b.data.Write(p) }
func (b *watchLog) text() string                { b.Lock(); defer b.Unlock(); return b.data.String() }

func TestWatchLifecycle(t *testing.T) {
	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "main.ghi")
	source := `namespace main
import app.tests
import fmt "go:fmt"
import net "go:net"
import http "go:net/http"
import os "go:os"
func main() {
 listener := net.Listen("tcp", "127.0.0.1:0")
 os.WriteFile("address", []byte(listener.Addr().String()), 0600)
 http.Serve(listener, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, tests.Value() + os.Args[1]) }))
}

`
	write := func(path, content string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	nested := filepath.Join(dir, "app/tests/value.ghi")
	nestedSource := `namespace app.tests
func Value() string { return "one:" }
`
	write(nested, nestedSource)
	write(sourcePath, source)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var log watchLog
	done := make(chan error, 1)
	go func() {
		done <- watch.Run(ctx, watch.Options{Dir: dir, Args: []string{"argument"}, Stdout: &log, Stderr: &log, Interval: 50 * time.Millisecond, Debounce: 50 * time.Millisecond})
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(15 * time.Second):
			t.Error("watch cleanup timed out")
		}
	})
	client := &http.Client{Timeout: time.Second, Transport: &http.Transport{DisableKeepAlives: true}}
	get := func(address string) string {
		r, err := client.Get("http://" + address)
		if err != nil {
			return ""
		}
		defer r.Body.Close()
		b, _ := io.ReadAll(r.Body)
		return string(b)
	}
	wait := func(predicate func() bool) {
		t.Helper()
		deadline := time.Now().Add(35 * time.Second)
		for time.Now().Before(deadline) {
			if predicate() {
				return
			}
			time.Sleep(30 * time.Millisecond)
		}
		t.Fatalf("watch condition timed out:\n%s", log.text())
	}
	address := ""
	wait(func() bool {
		b, _ := os.ReadFile(filepath.Join(dir, "address"))
		address = string(b)
		return address != "" && get(address) == "one:argument"
	})
	starts := strings.Count(log.text(), "[watch] building")
	write(filepath.Join(dir, "tests/ignored.ghi"), "invalid test source")
	write(filepath.Join(dir, "bin/generated.json"), "{}")
	time.Sleep(300 * time.Millisecond)
	if strings.Count(log.text(), "[watch] building") != starts {
		t.Fatal("ignored output/test files triggered build")
	}
	write(sourcePath, "namespace main\nfunc main(){missing()}\n")
	wait(func() bool { return strings.Contains(log.text(), "[watch] build failed:") })
	if get(address) != "one:argument" {
		t.Fatal("failed build stopped working service")
	}
	write(sourcePath, source)
	write(nested, strings.Replace(nestedSource, "one:", "two:", 1))
	wait(func() bool {
		b, _ := os.ReadFile(filepath.Join(dir, "address"))
		next := string(b)
		if next != "" && next != address && get(next) == "two:argument" {
			address = next
			return true
		}
		return false
	})
	// Preserve timestamp and length: polling must compare content.
	info, err := os.Stat(nested)
	if err != nil {
		t.Fatal(err)
	}
	write(nested, strings.Replace(nestedSource, "one:", "six:", 1))
	if err := os.Chtimes(nested, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	wait(func() bool {
		b, _ := os.ReadFile(filepath.Join(dir, "address"))
		next := string(b)
		if next != "" && next != address && get(next) == "six:argument" {
			address = next
			return true
		}
		return false
	})
	cancel()
	wait(func() bool { return get(address) == "" })
}

func TestWatchTestsLifecycle(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("main.ghi", "namespace main\nfunc main() {}\n")
	write("app/value.ghi", "namespace app\nfunc Value() int {return 1}\n")
	source := "namespace tests.unit\nimport app\nimport testing \"go:testing\"\nfunc TestValue(t *testing.T) {if app.Value() != 1 {t.Fatal(\"wrong value\")}}\nfunc TestExcluded(t *testing.T) {t.Fatal(\"filter ignored\")}\nfunc BenchmarkValue(b *testing.B) {for i:=0;i<b.N;i++ {app.Value()}}\n"
	write("tests/unit/value.ghi", source)
	ctx, cancel := context.WithCancel(context.Background())
	var log watchLog
	done := make(chan error, 1)
	go func() {
		done <- watch.Tests(ctx, watch.TestOptions{Dir: dir, Filter: "^TestValue$", Race: os.Getenv("GHI_RACE_TEST") == "1", Bench: "^BenchmarkValue$", BenchTime: "1x", BenchMem: true, Count: 2, Cover: true, CoverProfile: filepath.Join(dir, "coverage.out"), Timeout: 10 * time.Second, Log: &log, Interval: 50 * time.Millisecond, Debounce: 50 * time.Millisecond})
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Error("test watch did not stop")
		}
	})
	wait := func(token string, count int) {
		t.Helper()
		deadline := time.Now().Add(35 * time.Second)
		for time.Now().Before(deadline) {
			if strings.Count(log.text(), token) >= count {
				return
			}
			time.Sleep(30 * time.Millisecond)
		}
		t.Fatalf("missing %s: %s", token, log.text())
	}
	wait("[test-watch] passed", 1)
	wait("app/value.ghi: 100.0%", 1)
	wait("BenchmarkValue-", 2)
	wait("allocs/op", 2)
	write("tests/unit/value.ghi", strings.Replace(source, "!= 1", "!= 2", 1))
	wait("[test-watch] failed", 1)
	write("app/value.ghi", "namespace app\nfunc Value() int {return 2}\n")
	wait("[test-watch] passed", 2)
	write("tests/unit/value.ghi", "namespace tests.unit\ninvalid syntax\n")
	wait("[test-watch] failed", 2)
	write("tests/unit/value.ghi", strings.Replace(source, "!= 1", "!= 2", 1))
	wait("[test-watch] passed", 3)
}

func TestWatchTestsRejectsInvalidOptions(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "source.ghi")
	if err := os.WriteFile(file, []byte("namespace main"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, options := range []watch.TestOptions{{Dir: dir, Filter: "["}, {Dir: dir, Timeout: -1}, {Dir: file}, {Dir: dir, CoverProfile: file}, {Dir: dir, Bench: "["}, {Dir: dir, Bench: ".", BenchTime: "0x"}, {Dir: dir, Bench: ".", BenchTime: "-1s"}, {Dir: dir, BenchMem: true}, {Dir: dir, Count: -1}} {
		if err := watch.Tests(context.Background(), options); err == nil {
			t.Fatal("invalid options accepted")
		}
	}
}
