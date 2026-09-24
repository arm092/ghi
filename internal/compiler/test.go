package compiler

import (
	"context"
	"fmt"
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
	Dir     string
	Run     string
	Timeout time.Duration
	Verbose bool
	Log     io.Writer
}

// Test runs Ghi tests from the dedicated tests directory in a temporary workspace.
func Test(ctx context.Context, options TestOptions) error {
	if options.Run != "" {
		if _, err := regexp.Compile(options.Run); err != nil {
			return fmt.Errorf("invalid test filter: %w", err)
		}
	}
	if options.Timeout < 0 {
		return fmt.Errorf("test timeout must not be negative")
	}
	prepared, err := prepareProjectMode(ctx, Options{Dir: options.Dir, Log: options.Log}, true)
	if err != nil {
		return err
	}
	defer prepared.close()
	count := 0
	for _, ns := range prepared.program.Ordered {
		if !prepared.program.TestNamespaces[ns.Name] {
			continue
		}
		var names []string
		for _, file := range ns.Files {
			for name, fn := range file.Unit.Functions {
				if !isTestName(name) {
					continue
				}
				if !testSignature(file, fn) {
					return fmt.Errorf("%s: test %s must accept exactly one *testing.T parameter and return nothing", prepared.program.Fset.Position(fn.Node.Pos()), name)
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
		for _, name := range names {
			fmt.Fprintf(&source, "func %s(t *testing.T) { subject.%s(t) }\n", name, name)
			count++
		}
		path := filepath.Join(prepared.workspace, filepath.FromSlash(strings.ReplaceAll(ns.Name, ".", "/")), "ghi_suite_test.go")
		if err := os.WriteFile(path, []byte(source.String()), 0644); err != nil {
			return err
		}
	}
	if count == 0 {
		return fmt.Errorf("no Test functions found under %s", filepath.Join(prepared.program.Root, "tests"))
	}
	timeout := options.Timeout
	if timeout == 0 {
		timeout = time.Minute
	}
	args := []string{"test", "-mod=readonly", "-count=1", "-timeout", timeout.String()}
	if options.Verbose {
		args = append(args, "-v")
	}
	if options.Run != "" {
		args = append(args, "-run", options.Run)
	}
	args = append(args, "./...")
	cmd := exec.CommandContext(ctx, prepared.goPath, args...)
	cmd.Dir = prepared.workspace
	cmd.Env = toolchain.Env()
	cmd.Stdout, cmd.Stderr = options.Log, options.Log
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("Ghi tests failed: %w", err)
	}
	return nil
}

func isTestName(name string) bool {
	if !strings.HasPrefix(name, "Test") {
		return false
	}
	if len(name) == 4 {
		return true
	}
	r, _ := utf8.DecodeRuneInString(name[4:])
	return !unicode.IsLower(r)
}

func testSignature(file *sourceFile, fn *functionDecl) bool {
	typ := fn.Node.Type
	if typ.Params.NumFields() != 1 || typ.Results.NumFields() != 0 || typ.TypeParams.NumFields() != 0 || len(fn.Defaults) != 0 {
		return false
	}
	ptr, ok := typ.Params.List[0].Type.(*ast.StarExpr)
	if !ok {
		return false
	}
	sel, ok := ptr.X.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "T" {
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
