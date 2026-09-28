package cli_test

import (
	"bytes"
	"context"
	"fmt"
	"ghi/internal/compiler"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Original statement identities must survive exception lowering and inherited
// method specialization, and results from separate test executables must merge.
func TestGhiSourceCoverage(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "project with spaces")
	source := `namespace app
class Base {
 protected value int
 constructor(value int) { this.value = value } // constructor
 public func Read() int { return this.value } // inherited
 public func Unused() int { return 99 } // unused-method
}
class Child extends Base { constructor() { parent(7) } }
func Value(positive bool) int {
 if positive {
  return 10 // positive
 }
 return 20 // negative
}
func Recover() int {
 value := 0
 try {
  throw new Exception("expected") // throw
 } catch err Exception {
  value = 3 // catch
 } finally {
  value++ // finally
 }
 return value
}
func Unused() int { return 42 } // unused-function
func Arrow() int {
 used := () int => { return 7 } // arrow-used
 unused := () int => { return 9 } // arrow-unused
 _ = unused
 return used()
}
func Matched(n int) int { result := match n { 1 => 11, default => 12, }; return result } // after-match
func Ternary(flag bool) int { result := flag ? 1 : 2; return result } // after-ternary
func Labelled() int {
 goto first
first:
 goto result // chained-goto
result:
 return 7 // labelled-return
}
func LabelledLoop() int {
 value := 0
 goto loop
loop:
 for value < 2 { // labelled-loop
  value++
  continue loop
 }
 return value
}
`
	files := map[string]string{
		"main.ghi":        "namespace main\nfunc main(){println(1)}\n",
		"app/value.ghi":   source,
		"other/value.ghi": "namespace other\nfunc Unused() int {return 5}\n",
		"tests/alpha/check.ghi": `namespace tests.alpha
import app
import testing "go:testing"
func TestAlpha(t *testing.T) {
 if app.Value(true)!=10 || new app.Child().Read()!=7 || app.Recover()!=4 || app.Arrow()!=7 || app.Matched(1)!=11 || app.Labelled()!=7 || app.LabelledLoop()!=2 || app.Ternary(true)!=1 || app.Ternary(false)!=2 {t.Fatal("wrong value")}
}
`,
		"tests/beta/check.ghi": `namespace tests.beta
import app
import testing "go:testing"
func TestBeta(t *testing.T) {if app.Value(false)!=20 {t.Fatal("wrong value")}}
func TestFailure(t *testing.T) {t.Fatal("expected failure")}
`,
	}
	for name, source := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(source), 0600); err != nil {
			t.Fatal(err)
		}
	}
	profile := filepath.Join(dir, "coverage.out")
	run := func(filter string) string {
		t.Helper()
		var log bytes.Buffer
		if err := compiler.Test(context.Background(), compiler.TestOptions{Dir: dir, Run: filter, CoverProfile: profile, Log: &log}); err != nil {
			t.Fatalf("%v\n%s", err, &log)
		}
		data, err := os.ReadFile(profile)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(log.String(), "Ghi statement coverage:") || !strings.Contains(log.String(), "other/value.ghi: 0.0%") {
			t.Fatalf("missing coverage summary: %s", &log)
		}
		return string(data)
	}
	expect := func(profile, marker string, want int) {
		t.Helper()
		for i, line := range strings.Split(source, "\n") {
			if strings.Contains(line, "// "+marker) {
				prefix := fmt.Sprintf("app/value.ghi:%d.", i+1)
				found := false
				for _, row := range strings.Split(profile, "\n") {
					if strings.HasPrefix(row, prefix) {
						found = true
						if !strings.HasSuffix(row, fmt.Sprintf(" 1 %d", want)) {
							t.Fatalf("%s: %s", marker, row)
						}
					}
				}
				if !found {
					t.Fatalf("missing %s in profile:\n%s", marker, profile)
				}
				return
			}
		}
		t.Fatal("bad marker")
	}
	all := run("^Test(Alpha|Beta)$")
	for _, marker := range []string{"arrow-used", "arrow-unused", "after-match", "after-ternary"} {
		for i, line := range strings.Split(source, "\n") {
			if strings.Contains(line, "// "+marker) {
				prefix := fmt.Sprintf("app/value.ghi:%d.", i+1)
				hits := []string{}
				for _, row := range strings.Split(all, "\n") {
					if strings.HasPrefix(row, prefix) {
						hits = append(hits, row)
					}
				}
				if len(hits) != 2 {
					t.Fatalf("%s: expected 2 original statements, got %v", marker, hits)
				}
				want := 1
				if marker == "arrow-unused" {
					want = 0
				}
				if !strings.HasSuffix(hits[1], fmt.Sprintf(" 1 %d", want)) {
					t.Fatalf("%s: bad inner/second statement %v", marker, hits)
				}
			}
		}
	}
	for _, marker := range []string{"constructor", "inherited", "positive", "negative", "throw", "catch", "finally", "labelled-return", "labelled-loop", "chained-goto"} {
		expect(all, marker, 1)
	}
	for _, marker := range []string{"unused-method", "unused-function"} {
		expect(all, marker, 0)
	}
	if strings.Contains(all, "tests/") || strings.Contains(all, "ghi_source") || strings.Contains(all, "runtime") {
		t.Fatalf("generated/test source in profile: %s", all)
	}
	selected := run("^TestAlpha$")
	expect(selected, "negative", 0)
	before, _ := os.ReadFile(profile)
	if err := compiler.Test(context.Background(), compiler.TestOptions{Dir: dir, Run: "TestFailure", CoverProfile: profile}); err == nil {
		t.Fatal("test failure swallowed")
	}
	after, _ := os.ReadFile(profile)
	if !bytes.Equal(before, after) {
		t.Fatal("failed suite replaced prior coverage")
	}
	for _, name := range []string{"main.ghi", "mojave.json", "notes.txt"} {
		path := filepath.Join(dir, name)
		if name != "main.ghi" {
			os.WriteFile(path, []byte("keep"), 0600)
		}
		old, _ := os.ReadFile(path)
		if err := compiler.Test(context.Background(), compiler.TestOptions{Dir: dir, CoverProfile: path}); err == nil {
			t.Fatal("unsafe profile accepted")
		}
		current, _ := os.ReadFile(path)
		if !bytes.Equal(old, current) {
			t.Fatal("profile overwrote project input")
		}
	}
}
