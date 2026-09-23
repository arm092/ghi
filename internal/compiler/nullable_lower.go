package compiler

import (
	"go/ast"
	"go/parser"
	"go/types"
)

func (p *program) classType(typ types.Type) *classDecl {
	if typ == nil {
		return nil
	}
	named, ok := types.Unalias(typ).(*types.Named)
	if !ok || named.Obj().Pkg() == nil {
		return nil
	}
	for _, ns := range p.Ordered {
		if namespacePath(ns) == named.Obj().Pkg().Path() {
			return p.classes()[ns.Name+"."+named.Obj().Name()]
		}
	}
	return nil
}

// Nullable references are pointers to nominal class interfaces. Explicit type
// arguments preserve the declared parent/interface when boxing a child value.
func (p *program) boxNullable(info *types.Info) bool {
	changed := false
	for _, ns := range p.Ordered {
		for _, file := range ns.Files {
			if file.Unit.Native {
				continue
			}
			box := func(value ast.Expr, expected types.Type) ast.Expr {
				if expected == nil {
					return value
				}
				ptr, ok := types.Unalias(expected).(*types.Pointer)
				if !ok {
					return value
				}
				target := p.classType(ptr.Elem())
				actual := info.TypeOf(value)
				if target == nil || p.classType(actual) == nil || !types.AssignableTo(actual, ptr.Elem()) {
					return value
				}
				fun, _ := parser.ParseExpr(p.runtimeSymbol("Some", file, ns))
				typ, _ := parser.ParseExpr(p.classSymbol(target, target.Name, file, ns))
				changed = true
				return &ast.CallExpr{Fun: &ast.IndexExpr{X: fun, Index: typ}, Args: []ast.Expr{value}}
			}
			var visit func(ast.Node, *ast.FieldList)
			visit = func(root ast.Node, results *ast.FieldList) {
				ast.Inspect(root, func(node ast.Node) bool {
					switch n := node.(type) {
					case *ast.FuncDecl:
						if n.Body != nil {
							visit(n.Body, n.Type.Results)
						}
						return false
					case *ast.FuncLit:
						visit(n.Body, n.Type.Results)
						return false
					case *ast.ValueSpec:
						if len(n.Values) == len(n.Names) {
							for i, value := range n.Values {
								if object := info.Defs[n.Names[i]]; object != nil {
									n.Values[i] = box(value, object.Type())
								}
							}
						}
					case *ast.AssignStmt:
						if len(n.Lhs) == len(n.Rhs) {
							for i, value := range n.Rhs {
								n.Rhs[i] = box(value, info.TypeOf(n.Lhs[i]))
							}
						}
					case *ast.CallExpr:
						if signature, ok := info.TypeOf(n.Fun).(*types.Signature); ok {
							for i, value := range n.Args {
								if i >= signature.Params().Len() {
									break
								}
								if signature.Variadic() && i == signature.Params().Len()-1 {
									break
								}
								n.Args[i] = box(value, signature.Params().At(i).Type())
							}
						}
					case *ast.ReturnStmt:
						if results != nil {
							index := 0
							for _, result := range results.List {
								count := len(result.Names)
								if count == 0 {
									count = 1
								}
								for j := 0; j < count && index < len(n.Results); j++ {
									n.Results[index] = box(n.Results[index], info.TypeOf(result.Type))
									index++
								}
							}
						}
					}
					return true
				})
			}
			// Imports can be added during boxing; iterate over a stable declaration list.
			for _, decl := range append([]ast.Decl(nil), file.Tree.Decls...) {
				visit(decl, nil)
			}
		}
	}
	return changed
}
