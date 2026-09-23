package compiler

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"strings"
)

func (p *program) expressionClass(expression ast.Expr, info *types.Info) *classDecl {
	if info == nil {
		return nil
	}
	return p.classType(info.TypeOf(expression))
}

func fillDefaults(call *ast.CallExpr, fn *functionDecl, receiverArguments int) bool {
	if fn == nil {
		return false
	}
	count := len(fn.Node.Type.Params.List)
	if fn.Owner != nil && !fn.Owner.Interface {
		count--
	}
	changed := false
	for len(call.Args)-receiverArguments < count {
		value := fn.Defaults[len(call.Args)-receiverArguments]
		if value == nil {
			break
		}
		clone, _ := parser.ParseExpr(expressionText(value))
		call.Args = append(call.Args, clone)
		changed = true
	}
	return changed
}

func (p *program) rewrite(info *types.Info) (bool, error) {
	changed := false
	nativeValues := p.nativeFunctionValues(info)
	var failure error
	for _, ns := range p.Ordered {
		for _, file := range ns.Files {
			owners := map[*ast.FuncDecl]*classDecl{}
			for _, c := range file.Unit.Classes {
				if c.Interface {
					continue
				}
				owners[c.Constructor.Node] = c
				for _, method := range c.Methods {
					owners[method.Node] = c
				}
			}
			for _, decl := range file.Tree.Decls {
				var owner *classDecl
				if fn, ok := decl.(*ast.FuncDecl); ok {
					owner = owners[fn]
				}
				walkNode(decl, func(node ast.Node) ast.Node {
					if failure != nil {
						return node
					}
					reject := func(message string) { failure = fmt.Errorf("%s: %s", p.Fset.Position(node.Pos()), message) }
					switch n := node.(type) {
					case *ast.IncDecStmt:
						if selector, ok := n.X.(*ast.SelectorExpr); ok {
							if c := p.expressionClass(selector.X, info); c != nil && c.field(selector.Sel.Name) != nil {
								op := token.ADD_ASSIGN
								if n.Tok == token.DEC {
									op = token.SUB_ASSIGN
								}
								replacement := &ast.AssignStmt{Lhs: []ast.Expr{n.X}, Tok: op, Rhs: []ast.Expr{&ast.BasicLit{Kind: token.INT, Value: "1"}}}
								// Keep this rewrite separate: the next typecheck annotates the new assignment.
								changed = true
								return replacement
							}
						}
					case *ast.AssignStmt:
						// Writes are lowered before member reads, preserving object identity.
						if len(n.Lhs) == 1 && len(n.Rhs) == 1 {
							if selector, ok := n.Lhs[0].(*ast.SelectorExpr); ok {
								if c := p.expressionClass(selector.X, info); c != nil {
									if field := c.field(selector.Sel.Name); field != nil {
										if !accessible(field.Visibility, field.Owner, owner) {
											reject("field " + field.Name + " is " + field.Visibility)
											return node
										}
										if n.Tok != token.ASSIGN {
											// Taking the field address evaluates the receiver once and
											// lets Go perform the compound operation directly.
											address := &ast.CallExpr{Fun: &ast.SelectorExpr{X: selector.X, Sel: ast.NewIdent(fieldRef(field))}}
											n.Lhs[0] = &ast.StarExpr{X: address}
											changed = true
											return n
										}
										changed = true
										return &ast.ExprStmt{X: &ast.CallExpr{Fun: &ast.SelectorExpr{X: selector.X, Sel: ast.NewIdent(fieldSet(field))}, Args: n.Rhs}}
									}
								}
							}
						}
					case *ast.CallExpr:
						text := expressionText(n.Fun)
						if text == "GhiThrow" {
							fun, _ := parser.ParseExpr(p.runtimeSymbol("Raise", file, ns))
							changed = true
							return &ast.CallExpr{Fun: ast.NewIdent("panic"), Args: []ast.Expr{&ast.CallExpr{Fun: fun, Args: n.Args}}}
						}
						if info != nil && !p.Wrapped[n] {
							var object types.Object
							switch fun := n.Fun.(type) {
							case *ast.Ident:
								object = info.Uses[fun]
							case *ast.SelectorExpr:
								object = info.Uses[fun.Sel]
							}
							fn, isFunction := object.(*types.Func)
							if nativeValues[object] || (isFunction && fn.Pkg() != nil && !strings.HasPrefix(fn.Pkg().Path(), generatedModule)) {
								if signature, ok := functionSignature(info.TypeOf(n.Fun)); ok && signature.Results().Len() > 0 {
									last := signature.Results().Len() - 1
									if types.Identical(signature.Results().At(last).Type(), types.Universe.Lookup("error").Type()) {
										p.Wrapped[n] = true
										name := p.ensureErrorHelper(last)
										fun, _ := parser.ParseExpr(p.runtimeSymbol(name, file, ns))
										changed = true
										return &ast.CallExpr{Fun: fun, Args: []ast.Expr{n}}
									}
								}
							}
						}
						if fn := p.functionNamed(text, file, ns); fn != nil && fillDefaults(n, fn, 0) {
							changed = true
						}
						for _, c := range p.classes() {
							if !c.Interface && text == p.classSymbolIfImported(c, "GhiInit_"+c.Name, file, ns) && fillDefaults(n, c.Constructor, 1) {
								changed = true
							}
						}
						if c := p.classNamed(text, file, ns); c != nil {
							if c.Interface {
								reject("cannot instantiate interface " + c.Name)
								return node
							}
							n.Fun, _ = parser.ParseExpr(p.classSymbol(c, "GhiNew_"+c.Name, file, ns))
							fillDefaults(n, c.Constructor, 0)
							changed = true
							return node
						}
						if id, ok := n.Fun.(*ast.Ident); ok {
							if id.Name == "parent" {
								reject("parent(...) is only allowed as a direct constructor statement")
								return node
							}
							if fn := file.Unit.Functions[id.Name]; fn != nil && fillDefaults(n, fn, 0) {
								changed = true
							}
							for _, c := range file.Unit.Classes {
								if !c.Interface && id.Name == "GhiInit_"+c.Name && fillDefaults(n, c.Constructor, 1) {
									changed = true
								}
							}
						}
						if selector, ok := n.Fun.(*ast.SelectorExpr); ok {
							if id, ok := selector.X.(*ast.Ident); ok && id.Name == "parent" {
								if owner == nil || owner.Parent == nil {
									reject("parent method call requires a parent class")
									return node
								}
								method := owner.Parent.method(selector.Sel.Name)
								if method == nil || !accessible(method.Visibility, method.Owner, owner) {
									reject("parent method is missing or inaccessible")
									return node
								}
								n.Fun, _ = parser.ParseExpr(p.classSymbol(method.Owner, bodyName(method), file, ns))
								fillDefaults(n, method, 0)
								n.Args = append([]ast.Expr{ast.NewIdent("this")}, n.Args...)
								changed = true
								return node
							}
							if c := p.expressionClass(selector.X, info); c != nil {
								if method := c.method(selector.Sel.Name); method != nil {
									if !accessible(method.Visibility, method.Owner, owner) {
										reject("method " + method.Name + " is " + method.Visibility)
										return node
									}
									selector.Sel = ast.NewIdent("GhiM_" + method.Name)
									fillDefaults(n, method, 0)
									changed = true
									return node
								}
							}
						}
					case *ast.SelectorExpr:
						if c := p.expressionClass(n.X, info); c != nil {
							if field := c.field(n.Sel.Name); field != nil {
								if !accessible(field.Visibility, field.Owner, owner) {
									reject("field " + field.Name + " is " + field.Visibility)
									return node
								}
								changed = true
								return &ast.CallExpr{Fun: &ast.SelectorExpr{X: n.X, Sel: ast.NewIdent(fieldGet(field))}}
							}
							if method := c.method(n.Sel.Name); method != nil {
								if !accessible(method.Visibility, method.Owner, owner) {
									reject("method " + method.Name + " is " + method.Visibility)
									return node
								}
								n.Sel = ast.NewIdent("GhiM_" + method.Name)
								changed = true
							}
						}
					}
					return node
				}, false)
				if failure != nil {
					return false, failure
				}
			}
		}
	}
	return changed, nil
}
