package compiler

import (
	"fmt"
	"go/ast"
	"go/types"
	"strings"
)

// validateNonNull complements Go's assignability rules: Go interfaces accept
// nil, while a Ghi class reference requires an object unless explicitly optional.
func (p *program) validateNonNull(info *types.Info) error {
	var failure error
	reject := func(node ast.Node, message string) {
		if failure == nil {
			failure = fmt.Errorf("%s: %s", p.Fset.Position(node.Pos()), message)
		}
	}
	check := func(value ast.Expr, expected types.Type) {
		if p.classType(expected) == nil {
			return
		}
		typ := info.TypeOf(value)
		if typ != nil {
			if basic, ok := typ.(*types.Basic); ok && basic.Kind() == types.UntypedNil {
				reject(value, "nil requires a nullable type (use '?Type')")
			}
		}
	}
	var visit func(ast.Node, *ast.FieldList)
	visit = func(root ast.Node, results *ast.FieldList) {
		ast.Inspect(root, func(node ast.Node) bool {
			if failure != nil {
				return false
			}
			p.checkCollection(node, info, check, reject)
			switch n := node.(type) {
			case *ast.SendStmt:
				if typ := info.TypeOf(n.Chan); typ != nil {
					if channel, ok := typ.Underlying().(*types.Chan); ok {
						check(n.Value, channel.Elem())
					}
				}
			case *ast.SelectorExpr:
				if p.optionalAggregate(info.TypeOf(n.X)) {
					reject(n, "nullable member access requires a stable nil check")
				}
			case *ast.IndexExpr:
				if p.optionalAggregate(info.TypeOf(n.X)) {
					reject(n, "nullable index access requires a stable nil check")
				}
			case *ast.SliceExpr:
				if p.optionalAggregate(info.TypeOf(n.X)) {
					reject(n, "nullable slice access requires a stable nil check")
				}
			case *ast.RangeStmt:
				if p.optionalAggregate(info.TypeOf(n.X)) {
					reject(n, "nullable iteration requires a stable nil check")
				}
			case *ast.StarExpr:
				if info.Types[n].IsValue() && p.needsInitialization(info.TypeOf(n)) && !p.CheckedDereferences[n] {
					reject(n, "nullable dereference requires a stable nil check")
				}
			case *ast.FuncDecl:
				if n.Body != nil {
					if err := p.validateNamedResults(n.Type, n.Body, info); err != nil {
						failure = err
						return false
					}
					visit(n.Body, n.Type.Results)
				}
				return false
			case *ast.FuncLit:
				if err := p.validateNamedResults(n.Type, n.Body, info); err != nil {
					failure = err
					return false
				}
				visit(n.Body, n.Type.Results)
				return false
			case *ast.ValueSpec:
				for i, name := range n.Names {
					object := info.Defs[name]
					if object == nil {
						continue
					}
					if len(n.Values) == 0 && p.needsInitialization(object.Type()) && !strings.HasPrefix(name.Name, "ghi_") {
						reject(name, "nonnullable variable "+name.Name+" requires an initializer")
					}
					if len(n.Values) == len(n.Names) {
						check(n.Values[i], object.Type())
					}
				}
			case *ast.AssignStmt:
				if len(n.Lhs) == len(n.Rhs) {
					for i, value := range n.Rhs {
						check(value, info.TypeOf(n.Lhs[i]))
					}
				}
			case *ast.CallExpr:
				if info.Types[n.Fun].IsType() {
					for _, arg := range n.Args {
						check(arg, info.TypeOf(n.Fun))
					}
				}
				if signature, ok := functionSignature(info.TypeOf(n.Fun)); ok {
					for i, value := range n.Args {
						index := i
						if index >= signature.Params().Len() {
							index = signature.Params().Len() - 1
						}
						if index < 0 {
							break
						}
						expected := signature.Params().At(index).Type()
						if signature.Variadic() && index == signature.Params().Len()-1 && !n.Ellipsis.IsValid() {
							expected = expected.(*types.Slice).Elem()
						}
						check(value, expected)
					}
				}
			case *ast.ReturnStmt:
				if results != nil {
					index := 0
					for _, field := range results.List {
						count := len(field.Names)
						if count == 0 {
							count = 1
						}
						for j := 0; j < count && index < len(n.Results); j++ {
							check(n.Results[index], info.TypeOf(field.Type))
							index++
						}
					}
				}
			}
			return true
		})
	}
	for _, ns := range p.Ordered {
		for _, file := range ns.Files {
			if file.Unit.Native {
				continue
			}
			authored := map[*ast.FuncDecl]bool{}
			functions := []*functionDecl{}
			for _, fn := range file.Unit.Functions {
				functions = append(functions, fn)
			}
			for _, class := range file.Unit.Classes {
				functions = append(functions, class.Methods...)
				if class.Constructor != nil {
					functions = append(functions, class.Constructor)
				}
			}
			for _, fn := range functions {
				authored[fn.Node] = true
				for index, value := range fn.Defaults {
					offset := 0
					if fn.Owner != nil && !fn.Owner.Interface {
						offset = 1
					}
					param := fn.Node.Type.Params.List[index+offset]
					genericParameter := (&classDecl{TypeParams: fn.Node.Type.TypeParams}).mentionsTypeParameter(param.Type)
					if (p.sourceNeedsInitialization(param.Type, file, ns, map[string]bool{}) || genericParameter) && nilSyntax(value) {
						reject(fn.Node, "nil default requires a nullable parameter type")
					}
				}
			}
			for _, decl := range file.Tree.Decls {
				if fn, ok := decl.(*ast.FuncDecl); ok && !authored[fn] {
					continue
				}
				visit(decl, nil)
			}
		}
	}
	return failure
}

func nilSyntax(expr ast.Expr) bool {
	if paren, ok := expr.(*ast.ParenExpr); ok {
		return nilSyntax(paren.X)
	}
	id, ok := expr.(*ast.Ident)
	return ok && id.Name == "nil"
}

func (p *program) optionalAggregate(typ types.Type) bool {
	if typ == nil {
		return false
	}
	ptr, ok := types.Unalias(typ).(*types.Pointer)
	return ok && p.needsInitialization(ptr.Elem())
}
