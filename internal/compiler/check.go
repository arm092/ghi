package compiler

import (
	"context"
	"fmt"
	"ghi/internal/toolchain"
	"go/ast"
	"os"
	"path/filepath"
)

type preparedProject struct {
	program   *program
	workspace string
	goPath    string
}

func (p *preparedProject) close() { os.RemoveAll(p.workspace) }

// prepareProject is shared by check and build so both enforce the same rules.
func prepareProject(ctx context.Context, options Options) (*preparedProject, error) {
	return prepareProjectMode(ctx, options, false)
}

func prepareProjectMode(ctx context.Context, options Options, testing bool) (*preparedProject, error) {
	root := options.Dir
	if root == "" {
		root = "."
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(root)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("project path must be a directory: %s", root)
	}
	p, err := loadProjectMode(root, testing)
	if err != nil {
		return nil, err
	}
	// Go permits declarations implemented in assembly; Ghi projects do not.
	// Validate before lowering so generated declarations are not involved.
	for _, ns := range p.Ordered {
		for _, file := range ns.Files {
			for _, decl := range file.Tree.Decls {
				if fn, ok := decl.(*ast.FuncDecl); ok && fn.Body == nil {
					return nil, fmt.Errorf("%s: function %s requires a body", p.Fset.Position(fn.Pos()), fn.Name.Name)
				}
			}
		}
	}
	workspace, err := os.MkdirTemp("", "ghi-build-")
	if err != nil {
		return nil, err
	}
	prepared := &preparedProject{program: p, workspace: workspace}
	prepared.goPath, err = (toolchain.Manager{Log: options.Log}).Ensure(ctx)
	if err != nil {
		prepared.close()
		return nil, err
	}
	if err := p.generate(ctx, workspace, prepared.goPath); err != nil {
		prepared.close()
		return nil, p.sourceError(err)
	}
	if !testing {
		if err := p.validateEntry(); err != nil {
			prepared.close()
			return nil, err
		}
	}
	return prepared, nil
}

func (p *program) validateEntry() error {
	ns := p.Namespaces["main"]
	for _, file := range ns.Files {
		if fn := file.Unit.Functions["main"]; fn != nil && fn.Node != nil {
			typ := fn.Node.Type
			if typ.Params.NumFields() != 0 || typ.Results.NumFields() != 0 || typ.TypeParams.NumFields() != 0 {
				return fmt.Errorf("%s: entry point must be declared as func main()", p.Fset.Position(fn.Node.Pos()))
			}
			return nil
		}
	}
	return fmt.Errorf("%s:1: entry namespace requires func main()", ns.Files[0].Path)
}

// Check validates the production project without linking an application binary.
// Dependency export data may still require Go's compiler and network access.
func Check(ctx context.Context, options Options) error {
	if options.Output != "" {
		return fmt.Errorf("ghi check does not accept an output path")
	}
	prepared, err := prepareProject(ctx, options)
	if err != nil {
		return err
	}
	defer prepared.close()
	return nil
}
