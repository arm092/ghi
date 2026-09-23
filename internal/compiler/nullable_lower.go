package compiler

import (
	"go/ast"
	"go/parser"
	"go/token"
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
			if class := p.classes()[ns.Name+"."+named.Obj().Name()]; class != nil {
				return class
			}
			return p.definedClass(named.Obj().Name(), ns, map[string]bool{})
		}
	}
	return nil
}

func (p *program) definedClass(name string, ns *namespace, seen map[string]bool) *classDecl {
	key := ns.Name + "." + name
	if seen[key] {
		return nil
	}
	seen[key] = true
	for _, file := range ns.Files {
		for _, decl := range file.Tree.Decls {
			group, ok := decl.(*ast.GenDecl)
			if !ok {
				continue
			}
			for _, spec := range group.Specs {
				definition, ok := spec.(*ast.TypeSpec)
				if !ok || definition.Name.Name != name {
					continue
				}
				if class := p.classNamed(expressionText(definition.Type), file, ns); class != nil {
					return class
				}
				if id, ok := definition.Type.(*ast.Ident); ok {
					return p.definedClass(id.Name, ns, seen)
				}
			}
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
				target := ptr.Elem()
				actual := info.TypeOf(value)
				if !p.needsInitialization(target) || !p.needsInitialization(actual) || !types.AssignableTo(actual, target) {
					return value
				}
				fun, _ := parser.ParseExpr(p.runtimeSymbol("Some", file, ns))
				typ, _ := parser.ParseExpr(types.TypeString(target, func(pkg *types.Package) string {
					if pkg.Path() == namespacePath(ns) {
						return ""
					}
					return p.importAlias(nil, file, pkg.Path(), pkg.Name())
				}))
				changed = true
				return &ast.CallExpr{Fun: &ast.IndexExpr{X: fun, Index: typ}, Args: []ast.Expr{value}}
			}
			var visit func(ast.Node, *ast.FieldList)
			visit = func(root ast.Node, results *ast.FieldList) {
				ast.Inspect(root, func(node ast.Node) bool {
					switch n := node.(type) {
					case *ast.SendStmt:
						if typ := info.TypeOf(n.Chan); typ != nil {
							if channel, ok := typ.Underlying().(*types.Chan); ok {
								n.Value = box(n.Value, channel.Elem())
							}
						}
					case *ast.BinaryExpr:
						if n.Op == token.EQL || n.Op == token.NEQ {
							n.X = box(n.X, info.TypeOf(n.Y))
							n.Y = box(n.Y, info.TypeOf(n.X))
						}
					case *ast.CompositeLit:
						p.convertLiteral(n, info, box)
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
						if signature, ok := functionSignature(info.TypeOf(n.Fun)); ok {
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

func (p *program) convertLiteral(lit *ast.CompositeLit, info *types.Info, convert func(ast.Expr, types.Type) ast.Expr) {
	typ := info.TypeOf(lit)
	if typ == nil {
		return
	}
	for index, entry := range lit.Elts {
		var expected types.Type
		value := entry
		pair, keyed := entry.(*ast.KeyValueExpr)
		if keyed {
			value = pair.Value
		}
		switch t := typ.Underlying().(type) {
		case *types.Array:
			expected = t.Elem()
		case *types.Slice:
			expected = t.Elem()
		case *types.Map:
			expected = t.Elem()
			if keyed {
				pair.Key = convert(pair.Key, t.Key())
			}
		case *types.Struct:
			field := index
			if keyed {
				field = -1
				if id, ok := pair.Key.(*ast.Ident); ok {
					for i := 0; i < t.NumFields(); i++ {
						if t.Field(i).Name() == id.Name {
							field = i
							break
						}
					}
				}
			}
			if field >= 0 && field < t.NumFields() {
				expected = t.Field(field).Type()
			}
		}
		if expected != nil {
			if keyed {
				pair.Value = convert(value, expected)
			} else {
				lit.Elts[index] = convert(value, expected)
			}
		}
	}
}
