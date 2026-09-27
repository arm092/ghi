package cli_test

import (
	"bytes"
	"context"
	"ghi/internal/compiler"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func benchmarkProject(t *testing.T, source string) string {
	t.Helper()
	dir := t.TempDir()
	for name, value := range map[string]string{
		"app/value.ghi":   "namespace app\nfunc Value() int {return 7}\n",
		"tests/bench.ghi": source,
	} {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestNativeBenchmarks(t *testing.T) {
	dir := benchmarkProject(t, `namespace tests
import app
import check "go:testing"
var sink int
func TestExcluded(t *check.T) {t.Fatal("test filter ignored")}
func BenchmarkValue(b *check.B) {for i:=0;i<b.N;i++ {sink=app.Value()}}
func BenchmarkExcluded(b *check.B) {b.Fatal("benchmark filter ignored")}
`)
	var out bytes.Buffer
	profile := filepath.Join(dir, "coverage.out")
	err := compiler.Test(context.Background(), compiler.TestOptions{Dir: dir, Run: "^$", Bench: "^BenchmarkValue$", BenchTime: "3x", BenchMem: true, Count: 2, CoverProfile: profile, Log: &out})
	if err != nil {
		t.Fatalf("%v\n%s", err, &out)
	}
	if strings.Count(out.String(), "BenchmarkValue-") != 2 || !strings.Contains(out.String(), "allocs/op") {
		t.Fatalf("benchmark flags ignored: %s", &out)
	}
	data, err := os.ReadFile(profile)
	if err != nil || !strings.Contains(string(data), "app/value.ghi:") {
		t.Fatalf("benchmark coverage: %v %s", err, data)
	}
	// A benchmark-only package is valid with --bench, including B.Loop.
	dir = benchmarkProject(t, `namespace tests
import testing "go:testing"
var sink int
func BenchmarkOnly(b *testing.B) {for b.Loop() {sink++}}
`)
	out.Reset()
	if err := compiler.Test(context.Background(), compiler.TestOptions{Dir: dir, Bench: ".", BenchTime: "1x", Log: &out}); err != nil {
		t.Fatalf("%v\n%s", err, &out)
	}
	if err := compiler.Test(context.Background(), compiler.TestOptions{Dir: dir, Log: &out}); err == nil {
		t.Fatal("benchmark-only project accepted without --bench")
	}
	for _, source := range []string{"func BenchmarkWrong() {}", "func BenchmarkWrong(b *testing.T) {}"} {
		invalid := benchmarkProject(t, "namespace tests\nimport testing \"go:testing\"\nvar unused *testing.T\n"+source)
		if err := compiler.Test(context.Background(), compiler.TestOptions{Dir: invalid, Bench: "."}); err == nil || !strings.Contains(err.Error(), "*testing.B") {
			t.Fatalf("invalid benchmark signature: %v", err)
		}
	}
}

// This integration requires a Go race-supported platform and a C compiler.
// Explicit opt-in prevents silent reliance on Windows CGO availability.
func TestRaceExecution(t *testing.T) {
	if os.Getenv("GHI_RACE_TEST") != "1" {
		t.Skip("set GHI_RACE_TEST=1 on a race-capable Go/C toolchain")
	}
	dir := benchmarkProject(t, `namespace tests
import app
import sync "go:sync"
import testing "go:testing"
func TestSafe(t *testing.T) {
 var wg sync.WaitGroup
 for i:=0;i<4;i++ {wg.Add(1); go func(){defer wg.Done();for j:=0;j<100;j++ {if app.Value()!=7 {t.Error("wrong")}}}()}
 wg.Wait()
}
func TestRace(t *testing.T) {
 var wg sync.WaitGroup
 value:=0
 for i:=0;i<4;i++ {wg.Add(1);go func(){defer wg.Done();for j:=0;j<10000;j++ {value++}}()}
 wg.Wait()
 t.Log(value)
}
func TestBlock(t *testing.T) {select {}}
`)
	var out bytes.Buffer
	profile := filepath.Join(dir, "coverage.out")
	opts := compiler.TestOptions{Dir: dir, Race: true, Run: "^TestSafe$", CoverProfile: profile, Log: &out}
	if err := compiler.Test(context.Background(), opts); err != nil {
		t.Fatalf("%v\n%s", err, &out)
	}
	previous, err := os.ReadFile(profile)
	if err != nil {
		t.Fatal(err)
	}
	out.Reset()
	opts.Run = "^TestRace$"
	if err := compiler.Test(context.Background(), opts); err == nil || !strings.Contains(out.String(), "DATA RACE") {
		t.Fatalf("race not detected: %v\n%s", err, &out)
	}
	current, _ := os.ReadFile(profile)
	if !bytes.Equal(previous, current) {
		t.Fatal("failed race run replaced coverage")
	}
	out.Reset()
	opts.Run = "^TestBlock$"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	start := time.Now()
	if err := compiler.Test(ctx, opts); err == nil || ctx.Err() == nil {
		t.Fatalf("cancel not propagated: %v", err)
	}
	if time.Since(start) > 15*time.Second {
		t.Fatal("race child did not stop")
	}
	current, _ = os.ReadFile(profile)
	if !bytes.Equal(previous, current) {
		t.Fatal("cancel replaced coverage")
	}
}
