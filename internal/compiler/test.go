package compiler

import (
	"context"
	"fmt"
	"ghi/internal/proctree"
	"ghi/internal/toolchain"
	"go/ast"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

type TestOptions struct {
	Dir          string
	Run          string
	Race         bool
	Bench        string
	BenchTime    string
	BenchMem     bool
	Count        int
	Timeout      time.Duration
	Verbose      bool
	Cover        bool
	CoverProfile string
	Log          io.Writer
}

// ValidateTestOptions rejects invalid test options before starting a watcher.
func ValidateTestOptions(options TestOptions) error {
	if options.Run != "" {
		if _, err := regexp.Compile(options.Run); err != nil {
			return fmt.Errorf("invalid test filter: %w", err)
		}
	}
	if options.Bench != "" {
		if _, err := regexp.Compile(options.Bench); err != nil {
			return fmt.Errorf("invalid benchmark filter: %w", err)
		}
	}
	if options.BenchTime != "" {
		value := options.BenchTime
		if strings.HasSuffix(value, "x") {
			n, err := strconv.ParseInt(strings.TrimSuffix(value, "x"), 10, 64)
			if err != nil || n <= 0 {
				return fmt.Errorf("benchmark time must be a positive duration or iteration count such as 100x")
			}
		} else if d, err := time.ParseDuration(value); err != nil || d <= 0 {
			return fmt.Errorf("benchmark time must be a positive duration or iteration count such as 100x")
		}
	}
	if options.Count < 0 {
		return fmt.Errorf("test count must not be negative")
	}
	if options.Bench == "" && (options.BenchTime != "" || options.BenchMem) {
		return fmt.Errorf("--benchtime and --benchmem require --bench")
	}
	if options.Timeout < 0 {
		return fmt.Errorf("test timeout must not be negative")
	}
	if err := validateCoverageProfile(options.CoverProfile); err != nil {
		return err
	}
	return nil
}

// Test runs Ghi tests from the dedicated tests directory in a temporary workspace.
func Test(ctx context.Context, options TestOptions) error {
	if err := ValidateTestOptions(options); err != nil {
		return err
	}
	var coverage *coveragePlan
	if options.Cover || options.CoverProfile != "" {
		coverage = &coveragePlan{}
	}
	prepared, err := prepareProjectMode(ctx, Options{Dir: options.Dir, Log: options.Log, coverage: coverage}, true)
	if err != nil {
		return err
	}
	defer prepared.close()
	count := 0
	benchmarks := 0
	for _, ns := range prepared.program.Ordered {
		if !prepared.program.TestNamespaces[ns.Name] {
			continue
		}
		var names []string
		for _, file := range ns.Files {
			for name, fn := range file.Unit.Functions {
				benchmark := isBenchmarkName(name)
				if !isTestName(name) && !benchmark {
					continue
				}
				kind := "T"
				if benchmark {
					kind = "B"
				}
				if !testingSignature(file, fn, kind) {
					return fmt.Errorf("%s: %s must accept exactly one *testing.%s parameter and return nothing", prepared.program.Fset.Position(fn.Node.Pos()), name, kind)
				}
				names = append(names, name)
			}
		}
		if len(names) == 0 {
			continue
		}
		sort.Strings(names)
		var source strings.Builder
		fmt.Fprintf(&source, "package %s_test\nimport (\"testing\"; subject %q)\n", ns.GoName, namespacePath(ns))
		if coverage != nil {
			result := filepath.Join(prepared.workspace, fmt.Sprintf("coverage-%d.json", len(coverage.Results)))
			coverage.Results = append(coverage.Results, result)
			fmt.Fprintf(&source, "import (\"encoding/json\"; \"os\"; \"fmt\"; coverage %q)\nfunc TestMain(m *testing.M) { code:=m.Run(); data,err:=json.Marshal(coverage.CoverageSnapshot()); if err==nil {err=os.WriteFile(%q,data,0600)}; if err!=nil {fmt.Fprintln(os.Stderr,err);code=1}; os.Exit(code) }\n", namespacePath(prepared.program.Runtime), result)
		}
		for _, name := range names {
			kind := "T"
			if isBenchmarkName(name) {
				kind = "B"
				benchmarks++
			} else {
				count++
			}
			fmt.Fprintf(&source, "func %s(t *testing.%s) { subject.%s(t) }\n", name, kind, name)
		}
		path := filepath.Join(prepared.workspace, filepath.FromSlash(strings.ReplaceAll(ns.Name, ".", "/")), "ghi_suite_test.go")
		if err := os.WriteFile(path, []byte(source.String()), 0644); err != nil {
			return err
		}
	}
	if count == 0 && (options.Bench == "" || benchmarks == 0) {
		return fmt.Errorf("no runnable Test functions (or Benchmark functions with --bench) found under %s", filepath.Join(prepared.program.Root, "tests"))
	}
	timeout := options.Timeout
	if timeout == 0 {
		timeout = time.Minute
	}
	runs := options.Count
	if runs == 0 {
		runs = 1
	}
	args := []string{"test", "-mod=readonly", "-count", strconv.Itoa(runs), "-timeout", timeout.String()}
	if options.Race {
		args = append(args, "-race")
	}
	if options.Bench != "" {
		args = append(args, "-bench", options.Bench)
	}
	if options.BenchTime != "" {
		args = append(args, "-benchtime", options.BenchTime)
	}
	if options.BenchMem {
		args = append(args, "-benchmem")
	}
	if options.Verbose {
		args = append(args, "-v")
	}
	if options.Run != "" {
		args = append(args, "-run", options.Run)
	}
	args = append(args, "./...")
	cmd := exec.Command(prepared.goPath, args...)
	cmd.Dir = prepared.workspace
	cmd.Env = toolchain.Env()
	cmd.Stdout, cmd.Stderr = options.Log, options.Log
	if err := proctree.Run(ctx, cmd); err != nil {
		return fmt.Errorf("Ghi tests failed: %w", err)
	}
	if coverage != nil {
		return coverage.report(options.Log, options.CoverProfile)
	}
	return nil
}

func isTestName(name string) bool      { return isTestingName(name, "Test") }
func isBenchmarkName(name string) bool { return isTestingName(name, "Benchmark") }

func isTestingName(name, prefix string) bool {
	if !strings.HasPrefix(name, prefix) {
		return false
	}
	if len(name) == len(prefix) {
		return true
	}
	r, _ := utf8.DecodeRuneInString(name[len(prefix):])
	return !unicode.IsLower(r)
}

func testingSignature(file *sourceFile, fn *functionDecl, kind string) bool {
	typ := fn.Node.Type
	if typ.Params.NumFields() != 1 || typ.Results.NumFields() != 0 || typ.TypeParams.NumFields() != 0 || len(fn.Defaults) != 0 {
		return false
	}
	ptr, ok := typ.Params.List[0].Type.(*ast.StarExpr)
	if !ok {
		return false
	}
	sel, ok := ptr.X.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != kind {
		return false
	}
	alias, ok := sel.X.(*ast.Ident)
	if !ok {
		return false
	}
	for _, spec := range file.Tree.Imports {
		path, _ := strconv.Unquote(spec.Path.Value)
		name := "testing"
		if spec.Name != nil {
			name = spec.Name.Name
		}
		if path == "testing" && alias.Name == name {
			return true
		}
	}
	return false
}
