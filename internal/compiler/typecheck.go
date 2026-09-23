package compiler

import (
	"context"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/importer"
	"go/types"
	"io"
	"os"
	"os/exec"
	"strings"

	"ghi/internal/toolchain"
)

type exportLoader struct {
	ctx     context.Context
	goPath  string
	dir     string
	exports map[string]string
}

func (loader *exportLoader) open(path string) (io.ReadCloser, error) {
	if file := loader.exports[path]; file != "" {
		return os.Open(file)
	}
	command := exec.CommandContext(loader.ctx, loader.goPath, "list", "-deps", "-export", "-json", path)
	command.Dir = loader.dir
	command.Env = toolchain.Env()
	output, err := command.Output()
	if err != nil {
		if exit, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("load Go package %s: %s", path, exit.Stderr)
		}
		return nil, err
	}
	decoder := json.NewDecoder(strings.NewReader(string(output)))
	for {
		var pkg struct{ ImportPath, Export string }
		err := decoder.Decode(&pkg)
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if pkg.Export != "" {
			loader.exports[pkg.ImportPath] = pkg.Export
		}
	}
	file := loader.exports[path]
	if file == "" {
		return nil, fmt.Errorf("Go package %s has no export data", path)
	}
	return os.Open(file)
}

type packageChecker struct {
	program  *program
	external types.Importer
	packages map[string]*types.Package
	info     *types.Info
	first    error
}

func (checker *packageChecker) Import(path string) (*types.Package, error) {
	if pkg := checker.packages[path]; pkg != nil {
		return pkg, nil
	}
	for _, ns := range checker.program.Ordered {
		if namespacePath(ns) == path {
			return checker.check(ns), nil
		}
	}
	return checker.external.Import(path)
}
func namespacePath(ns *namespace) string {
	if ns.Name == "main" {
		return generatedModule
	}
	return generatedModule + "/" + strings.ReplaceAll(ns.Name, ".", "/")
}
func (checker *packageChecker) check(ns *namespace) *types.Package {
	path := namespacePath(ns)
	if pkg := checker.packages[path]; pkg != nil {
		return pkg
	}
	files := []*ast.File{}
	for _, file := range ns.Files {
		files = append(files, file.Tree)
	}
	config := types.Config{Importer: checker, GoVersion: "go1.26", Error: func(err error) {
		if checker.first == nil {
			checker.first = err
		}
	}}
	pkg, _ := config.Check(path, checker.program.Fset, files, checker.info)
	checker.packages[path] = pkg
	return pkg
}

func (p *program) lower(ctx context.Context, goPath, workspace string) error {
	if err := p.prepareClasses(); err != nil {
		return err
	}
	if err := p.lowerControl(); err != nil {
		return err
	}
	for _, ns := range p.Ordered {
		for _, file := range ns.Files {
			for _, c := range file.Unit.Classes {
				if err := p.prepareConstructor(c); err != nil {
					return err
				}
			}
		}
	}
	for _, ns := range p.Ordered {
		for _, file := range ns.Files {
			for _, c := range file.Unit.Classes {
				if err := p.emitClass(c); err != nil {
					return err
				}
			}
		}
	}
	loader := &exportLoader{ctx: ctx, goPath: goPath, dir: workspace, exports: map[string]string{}}
	external := importer.ForCompiler(p.Fset, "gc", loader.open)
	if _, err := p.rewrite(nil); err != nil {
		return err
	}
	limit := 1
	for _, ns := range p.Ordered {
		for _, file := range ns.Files {
			ast.Inspect(file.Tree, func(node ast.Node) bool {
				if node != nil {
					limit++
				}
				return true
			})
		}
	}
	for iteration := 0; iteration < limit; iteration++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		info := &types.Info{Types: map[ast.Expr]types.TypeAndValue{}, Defs: map[*ast.Ident]types.Object{}, Uses: map[*ast.Ident]types.Object{}, Selections: map[*ast.SelectorExpr]*types.Selection{}}
		checker := &packageChecker{program: p, external: external, packages: map[string]*types.Package{}, info: info}
		for _, ns := range p.Ordered {
			checker.check(ns)
		}
		changed, err := p.rewrite(info)
		if err != nil {
			return err
		}
		if !changed {
			return checker.first
		}
	}
	return fmt.Errorf("compiler lowering did not converge")
}
