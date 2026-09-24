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
	return p.classType(info.TypeOf(expression))
}

func fillDefaults(call *ast.CallExpr, fn *functionDecl, receiverArguments int, transforms ...func(ast.Expr) ast.Expr) bool {
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
		if len(transforms) > 0 {
			// Parse a file wrapper to resolve lambda-local names. ParseExpr alone
			// does not populate resolution objects used by scoped substitution.
			parsed, err := parser.ParseFile(token.NewFileSet(), "default.ghi", "package defaults\nvar ghi_default_placeholder = "+expressionText(value), 0)
			if err == nil {
				clone = parsed.Decls[0].(*ast.GenDecl).Specs[0].(*ast.ValueSpec).Values[0]
			}
			clone = transforms[0](clone)
		}
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
					case *ast.DeferStmt, *ast.GoStmt:
						var call *ast.CallExpr
						switch statement := n.(type) {
						case *ast.DeferStmt:
							call = statement.Call
						case *ast.GoStmt:
							call = statement.Call
						}
						if !p.Wrapped[call] {
							_, asynchronous := n.(*ast.GoStmt)
							signature := nativeErrorSignature(call, info, nativeValues)
							bridgeErrors := signature != nil
							builtin := false
							if asynchronous {
								signature, builtin = scheduledSignature(call, info)
								// Let ordinary call rewriting supply Ghi defaults first.
								if signature != nil && !scheduledArgumentsReady(call, signature, info) {
									signature = nil
								}
							}
							if signature != nil {
								replacement, err := p.captureScheduledCall(call, signature, file, ns, asynchronous, bridgeErrors, builtin)
								if err != nil {
									reject(err.Error())
									return node
								}
								switch statement := n.(type) {
								case *ast.DeferStmt:
									statement.Call = replacement
								case *ast.GoStmt:
									statement.Call = replacement
								}
								changed = true
							}
						}
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
											n.Lhs[0] = &ast.StarExpr{Star: selector.Pos(), X: address}
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
						target, explicitNew := unwrapConstruction(n.Fun)
						n.Fun = target
						base, typeArguments := genericBase(n.Fun)
						text := expressionText(n.Fun)
						if explicitNew && p.classNamed(text, file, ns) == nil {
							reject("new requires a Ghi class, got " + text)
							return node
						}
						baseText := expressionText(base)
						if text == "GhiThrow" {
							fun, _ := parser.ParseExpr(p.runtimeSymbol("Raise", file, ns))
							changed = true
							return &ast.CallExpr{Fun: ast.NewIdent("panic"), Args: []ast.Expr{&ast.CallExpr{Fun: fun, Args: n.Args}}}
						}
						if !p.Wrapped[n] {
							if signature := nativeErrorSignature(n, info, nativeValues); signature != nil {
								p.Wrapped[n] = true
								name := p.ensureErrorHelper(signature.Results().Len() - 1)
								fun, _ := parser.ParseExpr(p.runtimeSymbol(name, file, ns))
								changed = true
								return &ast.CallExpr{Fun: fun, Args: []ast.Expr{n}}
							}
						}
						if fn := p.functionNamed(baseText, file, ns); fn != nil && fillDefaults(n, fn, 0) {
							changed = true
						}
						for _, c := range p.classes() {
							if !c.Interface && baseText == p.classSymbolIfImported(c, "GhiInit_"+c.Name, file, ns) && fillDefaults(n, c.Constructor, 1, p.classDefaultTransform(c, classBindings(c, typeArguments), file, ns)) {
								changed = true
							}
						}
						if c := p.classNamed(text, file, ns); c != nil {
							if c.Interface {
								reject("cannot instantiate interface " + c.Name)
								return node
							}
							constructor := p.classSymbol(c, "GhiNew_"+c.Name, file, ns)
							if qualified, ok := base.(*ast.SelectorExpr); ok && c.Namespace != ns {
								// Keep the actual call-site binding when several aliases
								// import the same namespace in this file.
								constructor = expressionText(qualified.X) + ".GhiNew_" + c.Name
							}
							n.Fun, _ = parser.ParseExpr(constructor + typeArgumentsText(typeArguments))
							fillDefaults(n, c.Constructor, 0, p.classDefaultTransform(c, classBindings(c, typeArguments), file, ns))
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
								n.Fun, _ = parser.ParseExpr(p.classSymbol(method.Owner, bodyName(method), file, ns) + p.ancestorArgumentText(owner, method.Owner))
								fillDefaults(n, method, 0, p.classDefaultTransform(method.Owner, classBindings(method.Owner, p.ancestorArguments(owner, method.Owner)), file, ns))
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
									fillDefaults(n, method, 0, func(value ast.Expr) ast.Expr {
										bindings := p.inheritedCallBindings(c, method.Owner, selector.X, value, info, file, ns)
										return p.classDefaultTransform(method.Owner, bindings, file, ns)(value)
									})
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
