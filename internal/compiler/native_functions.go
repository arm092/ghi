package compiler

import (
	"go/ast"
	"go/types"
	"strings"
)

// Track imported function and bound-method values through ordinary aliases.
// This retains the Go error bridge after f := imported.Function or f := g.
func (p *program) nativeFunctionValues(info *types.Info) map[types.Object]bool {
	values := map[types.Object]bool{}
	if info == nil {
		return values
	}
	var origin func(ast.Expr) bool
	origin = func(expr ast.Expr) bool {
		var object types.Object
		switch e := unparen(expr).(type) {
		case *ast.Ident:
			object = info.ObjectOf(e)
		case *ast.SelectorExpr:
			object = info.Uses[e.Sel]
		case *ast.CallExpr:
			if _, ok := functionSignature(info.TypeOf(e)); ok {
				return origin(e.Fun)
			}
		}
		if values[object] {
			return true
		}
		fn, ok := object.(*types.Func)
		return ok && fn.Pkg() != nil && !strings.HasPrefix(fn.Pkg().Path(), generatedModule)
	}
	type binding struct {
		target types.Object
		value  ast.Expr
	}
	bindings := []binding{}
	for _, ns := range p.Ordered {
		for _, file := range ns.Files {
			if file.Unit.Native {
				continue
			}
			ast.Inspect(file.Tree, func(node ast.Node) bool {
				switch n := node.(type) {
				case *ast.AssignStmt:
					if len(n.Lhs) == len(n.Rhs) {
						for i, lhs := range n.Lhs {
							if id, ok := lhs.(*ast.Ident); ok {
								bindings = append(bindings, binding{info.ObjectOf(id), n.Rhs[i]})
							}
						}
					}
				case *ast.ValueSpec:
					if len(n.Names) == len(n.Values) {
						for i, name := range n.Names {
							bindings = append(bindings, binding{info.Defs[name], n.Values[i]})
						}
					}
				}
				return true
			})
		}
	}
	for changed := true; changed; {
		changed = false
		for _, binding := range bindings {
			if binding.target != nil && !values[binding.target] && origin(binding.value) {
				values[binding.target] = true
				changed = true
			}
		}
	}
	return values
}
