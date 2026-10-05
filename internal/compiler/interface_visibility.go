package compiler

import (
	"fmt"
	"go/ast"
	"go/types"
	"sort"
	"strings"
)

// Embedded nominal class bounds retain their internal method set. Only
// structural requirements introduce a public implementation requirement.
func (p *program) publicInterfaceRequirements(typ types.Type) []string {
	seen := map[types.Type]bool{}
	names := map[string]bool{}
	var visit func(types.Type)
	visit = func(typ types.Type) {
		if typ == nil || seen[typ] {
			return
		}
		seen[typ] = true
		if parameter, ok := types.Unalias(typ).(*types.TypeParam); ok {
			visit(parameter.Constraint())
			return
		}
		if class := p.classType(typ); class != nil && !class.Interface {
			return
		}
		if iface, ok := typ.Underlying().(*types.Interface); ok {
			for i := 0; i < iface.NumExplicitMethods(); i++ {
				if name, generated := strings.CutPrefix(iface.ExplicitMethod(i).Name(), "GhiM_"); generated {
					names[name] = true
				}
			}
			for i := 0; i < iface.NumEmbeddeds(); i++ {
				visit(iface.EmbeddedType(i))
			}
		}
	}
	visit(typ)
	result := make([]string, 0, len(names))
	for name := range names {
		result = append(result, name)
	}
	sort.Strings(result)
	return result
}

// Backend class interfaces include implementation methods for internal dispatch.
// Ghi structural conversions must still require a public implementation.
func (p *program) validateInterfaceVisibility(info *types.Info) error {
	var failure error
	check := func(node ast.Node, actual, expected types.Type) {
		if failure != nil || actual == nil || expected == nil || types.Identical(actual, expected) {
			return
		}
		class := p.classType(actual)
		if class == nil {
			class, _ = p.constraintClass(actual, func(bound *classDecl) bool { return !bound.Interface })
		}
		if class == nil || class.Interface {
			return
		}
		if _, ok := expected.Underlying().(*types.Interface); !ok || p.classType(expected) == class {
			return
		}
		// Nominal class upcasts expose protected/private members internally and
		// must retain their existing visibility checks at the eventual access.
		if target := p.classType(expected); target != nil && !target.Interface {
			return
		}
		for _, name := range p.publicInterfaceRequirements(expected) {
			if method := class.method(name); method != nil && method.Visibility != "public" {
				failure = fmt.Errorf("%s: type %s cannot satisfy interface: method %s is %s", p.Fset.Position(node.Pos()), class.Name, name, method.Visibility)
				return
			}
		}
	}
	values := func(expressions []ast.Expr, expected []types.Type) {
		if len(expressions) == 1 {
			if tuple, ok := info.TypeOf(expressions[0]).(*types.Tuple); ok {
				for i := 0; i < tuple.Len() && i < len(expected); i++ {
					check(expressions[0], tuple.At(i).Type(), expected[i])
				}
				return
			}
		}
		for i, expression := range expressions {
			if i < len(expected) {
				check(expression, info.TypeOf(expression), expected[i])
			}
		}
	}
	var visit func(ast.Node, *types.Tuple)
	visit = func(root ast.Node, results *types.Tuple) {
		ast.Inspect(root, func(node ast.Node) bool {
			if failure != nil {
				return false
			}
			switch n := node.(type) {
			case *ast.FuncDecl:
				if object := info.Defs[n.Name]; object != nil && n.Body != nil {
					if signature, ok := functionSignature(object.Type()); ok {
						visit(n.Body, signature.Results())
					}
				}
				return false
			case *ast.FuncLit:
				if signature, ok := functionSignature(info.TypeOf(n)); ok {
					visit(n.Body, signature.Results())
				}
				return false
			case *ast.ValueSpec:
				expected := make([]types.Type, len(n.Names))
				for i, name := range n.Names {
					if object := info.Defs[name]; object != nil {
						expected[i] = object.Type()
					}
				}
				values(n.Values, expected)
			case *ast.AssignStmt:
				expected := make([]types.Type, len(n.Lhs))
				for i, lhs := range n.Lhs {
					expected[i] = info.TypeOf(lhs)
				}
				values(n.Rhs, expected)
			case *ast.ReturnStmt:
				if results != nil {
					expected := make([]types.Type, results.Len())
					for i := range expected {
						expected[i] = results.At(i).Type()
					}
					values(n.Results, expected)
				}
			case *ast.CompositeLit:
				p.convertLiteral(n, info, func(value ast.Expr, expected types.Type) ast.Expr {
					check(value, info.TypeOf(value), expected)
					return value
				})
			case *ast.SendStmt:
				if typ := info.TypeOf(n.Chan); typ != nil {
					if channel, ok := typ.Underlying().(*types.Chan); ok {
						check(n.Value, info.TypeOf(n.Value), channel.Elem())
					}
				}
			case *ast.CallExpr:
				if value := info.Types[n.Fun]; value.IsType() && len(n.Args) == 1 {
					check(n.Args[0], info.TypeOf(n.Args[0]), value.Type)
				} else if id, ok := n.Fun.(*ast.Ident); ok && info.Uses[id] == types.Universe.Lookup("append") && len(n.Args) > 1 && !n.Ellipsis.IsValid() {
					if slice, ok := info.TypeOf(n.Args[0]).Underlying().(*types.Slice); ok {
						for _, argument := range n.Args[1:] {
							check(argument, info.TypeOf(argument), slice.Elem())
						}
					}
				} else if signature, ok := functionSignature(info.TypeOf(n.Fun)); ok {
					count := len(n.Args)
					if count == 1 {
						if tuple, ok := info.TypeOf(n.Args[0]).(*types.Tuple); ok {
							count = tuple.Len()
						}
					}
					expected := make([]types.Type, max(count, signature.Params().Len()))
					for i := range expected {
						index := i
						if signature.Variadic() && index >= signature.Params().Len()-1 {
							index = signature.Params().Len() - 1
							if n.Ellipsis.IsValid() {
								expected[i] = signature.Params().At(index).Type()
							} else {
								expected[i] = signature.Params().At(index).Type().(*types.Slice).Elem()
							}
						} else if index < signature.Params().Len() {
							expected[i] = signature.Params().At(index).Type()
						}
					}
					values(n.Args, expected)
				}
			}
			return true
		})
	}
	for _, ns := range p.Ordered {
		for _, file := range ns.Files {
			if !file.Unit.Native {
				visit(file.Tree, nil)
			}
		}
	}
	return failure
}
