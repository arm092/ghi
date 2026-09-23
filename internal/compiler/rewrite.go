package compiler

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
)

func (p *program) expressionClass(expression ast.Expr, info *types.Info) *classDecl {
	if info == nil {
		return nil
	}
	typ := info.TypeOf(expression)
	if typ == nil {
		return nil
	}
	named, ok := types.Unalias(typ).(*types.Named)
	if !ok || named.Obj().Pkg() == nil {
		return nil
	}
	path := named.Obj().Pkg().Path()
	for _, ns := range p.Ordered {
		if namespacePath(ns) == path {
			return p.classes()[ns.Name+"."+named.Obj().Name()]
		}
	}
	return nil
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
	mutationID := 0
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
											operators := map[token.Token]token.Token{token.ADD_ASSIGN: token.ADD, token.SUB_ASSIGN: token.SUB, token.MUL_ASSIGN: token.MUL, token.QUO_ASSIGN: token.QUO, token.REM_ASSIGN: token.REM, token.AND_ASSIGN: token.AND, token.OR_ASSIGN: token.OR, token.XOR_ASSIGN: token.XOR, token.SHL_ASSIGN: token.SHL, token.SHR_ASSIGN: token.SHR, token.AND_NOT_ASSIGN: token.AND_NOT}
											op, ok := operators[n.Tok]
											if !ok {
												reject("invalid field assignment")
												return node
											}
											mutationID++
											name := fmt.Sprintf("ghi_receiver_%d", mutationID)
											get := &ast.CallExpr{Fun: &ast.SelectorExpr{X: ast.NewIdent(name), Sel: ast.NewIdent(fieldGet(field))}}
											set := &ast.CallExpr{Fun: &ast.SelectorExpr{X: ast.NewIdent(name), Sel: ast.NewIdent(fieldSet(field))}, Args: []ast.Expr{&ast.BinaryExpr{X: get, Op: op, Y: n.Rhs[0]}}}
											changed = true
											return &ast.BlockStmt{List: []ast.Stmt{&ast.AssignStmt{Lhs: []ast.Expr{ast.NewIdent(name)}, Tok: token.DEFINE, Rhs: []ast.Expr{selector.X}}, &ast.ExprStmt{X: set}}}
										}
										changed = true
										return &ast.ExprStmt{X: &ast.CallExpr{Fun: &ast.SelectorExpr{X: selector.X, Sel: ast.NewIdent(fieldSet(field))}, Args: n.Rhs}}
									}
								}
							}
						}
					case *ast.CallExpr:
						text := expressionText(n.Fun)
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
