package performance_test

import (
	"context"
	"ghi/internal/compiler"
	"ghi/internal/toolchain"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Opt in: GHI_PERF=1 go test ./tests/performance -run TestComparison -v -count=1.
// Compilation is outside the runtime benchmarks. Both binaries use the same Go.
func TestComparison(t *testing.T) {
	if os.Getenv("GHI_PERF") != "1" {
		t.Skip("set GHI_PERF=1 to run performance comparisons")
	}
	ctx := context.Background()
	goPath, err := (toolchain.Manager{}).Ensure(ctx)
	if err != nil {
		t.Fatal(err)
	}
	command := exec.CommandContext(ctx, goPath, "version")
	version, err := command.CombinedOutput()
	if err != nil {
		t.Fatal(err)
	}
	t.Log(strings.TrimSpace(string(version)))
	driver, err := os.ReadFile("testdata/driver.txt")
	if err != nil {
		t.Fatal(err)
	}
	binaries := map[string]string{}
	for _, language := range []string{"Go", "Ghi"} {
		dir := t.TempDir()
		ext := strings.ToLower(language)
		source, err := os.ReadFile("testdata/main." + ext)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "main."+ext), append(source, driver...), 0600); err != nil {
			t.Fatal(err)
		}
		binary := filepath.Join(dir, "bench.exe")
		started := time.Now()
		if language == "Ghi" {
			_, err = compiler.Build(ctx, compiler.Options{Dir: dir, Output: binary})
		} else {
			command := exec.CommandContext(ctx, goPath, "build", "-o", binary, "main.go")
			command.Dir, command.Env = dir, toolchain.Env()
			var output []byte
			output, err = command.CombinedOutput()
			if err != nil {
				t.Log(string(output))
			}
		}
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("%s build (informational, warm caches not controlled): %s", language, time.Since(started))
		stat, err := os.Stat(binary)
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("%s binary bytes: %d", language, stat.Size())
		binaries[language] = binary
	}
	// Alternate order to reduce systematic thermal/order bias.
	for sample := 0; sample < 5; sample++ {
		order := []string{"Go", "Ghi"}
		if sample%2 == 1 {
			order[0], order[1] = order[1], order[0]
		}
		for _, language := range order {
			command := exec.CommandContext(ctx, binaries[language], "-test.benchtime=150ms")
			output, err := command.CombinedOutput()
			if err != nil {
				t.Fatalf("%s: %v\n%s", language, err, output)
			}
			t.Logf("%s sample %d\n%s", language, sample+1, output)
		}
	}
}
