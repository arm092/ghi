package compiler

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGhiRunnerSeparateSuites(t *testing.T) {
	dir := project(t, map[string]string{
		"calc/calc.ghi":            "namespace app.calc\nfunc Add(a int,b int) int{return a+b}",
		"tests/support/helper.ghi": "namespace tests.support\nfunc Expected() int{return 5}",
		"tests/unit/calc.ghi": `namespace tests.unit
import testing "go:testing"
import calc "app.calc"
import support "tests.support"
func TestAdd(t *testing.T){if calc.Add(2,3)!=support.Expected(){t.Fatal("wrong sum")}}
func TestFailure(t *testing.T){t.Fatal("deliberate failure")}
`,
	})
	var out bytes.Buffer
	if err := Test(context.Background(), TestOptions{Dir: dir, Run: "^TestAdd$", Verbose: true, Log: &out}); err != nil {
		t.Fatalf("%v\n%s", err, &out)
	}
	if !strings.Contains(out.String(), "PASS: TestAdd") {
		t.Fatalf("tests did not run: %s", &out)
	}
	if _, err := os.Stat(filepath.Join(dir, "bin")); !os.IsNotExist(err) {
		t.Fatal("test wrote production executable")
	}
	out.Reset()
	if err := Test(context.Background(), TestOptions{Dir: dir, Log: &out}); err == nil {
		t.Fatal("failing test returned success")
	}
	if !strings.Contains(out.String(), "deliberate failure") {
		t.Fatalf("missing failure output: %s", &out)
	}
}

func TestGhiRunnerRejectsMissingAndInvalidTests(t *testing.T) {
	for _, source := range []string{"func helper(){}", "func TestWrong(){}"} {
		dir := project(t, map[string]string{"tests/test.ghi": "namespace tests\n" + source})
		if err := Test(context.Background(), TestOptions{Dir: dir}); err == nil {
			t.Fatalf("accepted %s", source)
		}
	}
}
