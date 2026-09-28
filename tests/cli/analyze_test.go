package cli_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"ghi/internal/compiler"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestSyntaxAnalysisOriginalByteContract(t *testing.T) {
	source := []byte("\ufeffnamespace app\r\n// Ղ { }\r\nclass Box {\r\n public value ?string\r\n constructor(value ?string = nil) { this.value = value }\r\n}\r\nfunc text() string {\r\n x := `first\r\nsecond {}`\r\n /* ( Ղ ) */ return x\r\n}\r\n")
	result := compiler.AnalyzeSource("source.ghi", source)
	if len(result.Diagnostics) != 0 || result.Namespace != "app" || result.SchemaVersion != 1 {
		t.Fatalf("analysis: %+v", result)
	}
	if result.SHA256 != fmt.Sprintf("%x", sha256.Sum256(source)) {
		t.Fatal("digest does not cover original bytes")
	}
	seenRaw, seenComment, seenNullable := false, false, false
	for i, item := range result.Tokens {
		if item.Start < 0 || item.End < item.Start || item.End > len(source) {
			t.Fatalf("invalid span: %+v", item)
		}
		if item.Implicit {
			if item.Start != item.End || item.Text != "\n" || item.Kind != ";" {
				t.Fatalf("invalid implicit token: %+v", item)
			}
		} else if string(source[item.Start:item.End]) != item.Text {
			t.Fatalf("token changed original source bytes: %+v", item)
		}
		prefix := source[:item.Start]
		line := bytes.Count(prefix, []byte("\n")) + 1
		column := item.Start - bytes.LastIndexByte(prefix, '\n')
		if item.Line != line || item.Column != column {
			t.Fatalf("wrong byte location: %+v; want %d:%d", item, line, column)
		}
		if item.Matching >= 0 && (item.Matching >= len(result.Tokens) || result.Tokens[item.Matching].Matching != i) {
			t.Fatalf("asymmetric delimiter pair: %+v", item)
		}
		seenRaw = seenRaw || (item.Kind == "STRING" && strings.Contains(item.Text, "\r\n"))
		seenComment = seenComment || item.Kind == "COMMENT"
		seenNullable = seenNullable || item.Kind == "?"
	}
	if !seenRaw || !seenComment || !seenNullable {
		t.Fatal("missing raw string, comment or nullable token")
	}
	for _, bad := range [][]byte{
		[]byte("namespace main\nclass Bad { func broken( }"),
		[]byte("namespace main\nfunc f() {"),
		[]byte("namespace app\nclass Box extends 1 + {}"),
		[]byte("namespace app\nclass Box extends = Other {}"),
		[]byte("namespace app\nclass Box implements A + B {}"),
		[]byte("namespace app\nclass Box { value A + B\n}"),
		{0xff},
	} {
		failed := compiler.AnalyzeSource("bad.ghi", bad)
		if len(failed.Diagnostics) == 0 || len(failed.Tokens) != 0 || len(failed.Capabilities) != 0 {
			t.Fatalf("invalid source exposed partial analysis: %+v", failed)
		}
	}
}

func TestAnalyzeCLIIsReadOnlyAndToolchainIndependent(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, "ghi")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	if output, err := exec.Command("go", "build", "-o", binary, "../../cmd/ghi").CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, output)
	}
	file := filepath.Join(dir, "buffer.ghi")
	original := []byte("namespace app\nfunc f() {}\n")
	if err := os.WriteFile(file, original, 0600); err != nil {
		t.Fatal(err)
	}
	invoke := func(source string, args ...string) ([]byte, int) {
		t.Helper()
		cmd := exec.Command(binary, args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "PATH=", "GHI_GO=missing-toolchain")
		cmd.Stdin = strings.NewReader(source)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		out, err := cmd.Output()
		if err == nil {
			return out, 0
		}
		if exit, ok := err.(*exec.ExitError); ok {
			return out, exit.ExitCode()
		}
		t.Fatal(err)
		return nil, -1
	}
	for _, args := range [][]string{{"analyze", "--json", file}, {"analyze", "--json", "--stdin", "--filename", file}} {
		out, code := invoke(string(original), args...)
		var result compiler.SyntaxAnalysis
		if code != 0 || json.Unmarshal(out, &result) != nil || result.Namespace != "app" || len(result.Tokens) == 0 {
			t.Fatalf("analysis: %d %s", code, out)
		}
	}
	out, code := invoke("invalid", "analyze", "--json", "--stdin", "--filename", file)
	var failed compiler.SyntaxAnalysis
	if code != 1 || json.Unmarshal(out, &failed) != nil || len(failed.Diagnostics) != 1 || len(failed.Tokens) != 0 {
		t.Fatalf("invalid source protocol: %d %s", code, out)
	}
	for _, args := range [][]string{{"analyze", file}, {"analyze", "--json"}, {"analyze", "--json", "--stdin", file}, {"analyze", "--json", "--filename", file, file}} {
		out, code := invoke("", args...)
		if code != 2 || len(out) != 0 {
			t.Fatalf("invalid arguments accepted: %v => %d %s", args, code, out)
		}
	}
	actual, err := os.ReadFile(file)
	if err != nil || !bytes.Equal(actual, original) {
		t.Fatal("analysis changed the source file")
	}
	if _, err := os.Stat(filepath.Join(dir, ".ghi")); !os.IsNotExist(err) {
		t.Fatal("analysis created a project workspace")
	}
}
