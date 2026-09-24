package compiler

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/types"
)

// Infer only after all arms have single, known result types. Native error
// bridging and nested matches may require earlier lowering iterations first.
func (p *program) lowerMatchResults(info *types.Info) bool {
	changed := false
	for _, ns := range p.Ordered {
		for _, file := range ns.Files {
			if file.Unit.Native {
				continue
			}
			ast.Inspect(file.Tree, func(node ast.Node) bool {
				fn, ok := node.(*ast.FuncLit)
				if !ok || fn.Type.Results == nil || len(fn.Type.Results.List) != 1 {
					return true
				}
				marker, ok := fn.Type.Results.List[0].Type.(*ast.Ident)
				if !ok || marker.Name != matchResultMarker {
					return true
				}
				var result types.Type
				complete := true
				hasNil := false
				ast.Inspect(fn.Body, func(n ast.Node) bool {
					if _, ok := n.(*ast.FuncLit); ok {
						return false
					}
					ret, ok := n.(*ast.ReturnStmt)
					if !ok {
						return true
					}
					if len(ret.Results) != 1 {
						complete = false
						return false
					}
					typ := info.TypeOf(ret.Results[0])
					if typ == nil {
						complete = false
						return false
					}
					if _, ok := typ.(*types.Tuple); ok {
						complete = false
						return false
					}
					basic, untyped := typ.(*types.Basic)
					if untyped && basic.Kind() == types.Invalid {
						complete = false
						return false
					}
					if untyped && basic.Kind() == types.UntypedNil {
						hasNil = true
						return false
					}
					if result == nil {
						result = typ
					} else {
						old, oldBasic := result.(*types.Basic)
						if oldBasic && old.Info()&types.IsUntyped != 0 {
							if !untyped || basic.Info()&types.IsUntyped == 0 || (old.Info()&types.IsNumeric != 0 && basic.Info()&types.IsNumeric != 0 && basic.Kind() > old.Kind()) {
								result = typ
							}
						} else if types.AssignableTo(result, typ) && !types.AssignableTo(typ, result) {
							result = typ
						} else if ptr, ok := types.Unalias(typ).(*types.Pointer); ok && p.needsInitialization(ptr.Elem()) && types.AssignableTo(result, ptr.Elem()) {
							result = typ
						}
					}
					return false
				})
				if !complete || result == nil {
					return true
				}
				result = types.Default(result)
				if hasNil && p.needsInitialization(result) {
					result = types.NewPointer(result)
				}
				expr, err := parser.ParseExpr(types.TypeString(result, func(pkg *types.Package) string {
					if pkg.Path() == namespacePath(ns) {
						return ""
					}
					return p.importAlias(nil, file, pkg.Path(), pkg.Name())
				}))
				if err != nil {
					return true
				}
				fn.Type.Results.List[0].Type = expr
				changed = true
				return true
			})
		}
	}
	return changed
}

func (p *program) unresolvedMatch() error {
	var failure error
	for _, ns := range p.Ordered {
		for _, file := range ns.Files {
			ast.Inspect(file.Tree, func(n ast.Node) bool {
				if id, ok := n.(*ast.Ident); ok && id.Name == matchResultMarker && failure == nil {
					failure = fmt.Errorf("%s: match result type cannot be inferred; every arm must produce one typed value", p.Fset.Position(id.Pos()))
				}
				return failure == nil
			})
		}
	}
	return failure
}
