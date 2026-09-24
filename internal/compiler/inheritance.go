package compiler

import (
	"go/ast"
	"go/parser"
	"go/types"
	"strconv"
	"strings"
)

func classParameterNames(c *classDecl) []string {
	var names []string
	if c.TypeParams != nil {
		for _, field := range c.TypeParams.List {
			for _, name := range field.Names {
				names = append(names, name.Name)
			}
		}
	}
	return names
}

func classBindings(c *classDecl, args []ast.Expr) map[string]ast.Expr {
	bindings := map[string]ast.Expr{}
	for i, name := range classParameterNames(c) {
		if i < len(args) {
			bindings[name] = args[i]
		}
	}
	return bindings
}

func specializeDefault(expr ast.Expr, bindings map[string]ast.Expr) ast.Expr {
	return walkNode(expr, func(node ast.Node) ast.Node {
		switch n := node.(type) {
		case *ast.CompositeLit:
			n.Type = substituteType(n.Type, bindings)
		case *ast.CallExpr:
			n.Fun = substituteType(n.Fun, bindings)
			if id, ok := n.Fun.(*ast.Ident); ok && (id.Name == "make" || id.Name == "new") && len(n.Args) > 0 {
				n.Args[0] = substituteType(n.Args[0], bindings)
			}
		case *ast.Field:
			n.Type = substituteType(n.Type, bindings)
		case *ast.ValueSpec:
			n.Type = substituteType(n.Type, bindings)
		case *ast.TypeAssertExpr:
			n.Type = substituteType(n.Type, bindings)
		}
		return node
	}, true).(ast.Expr)
}

func (p *program) classDefaultTransform(owner *classDecl, bindings map[string]ast.Expr, file *sourceFile, ns *namespace) func(ast.Expr) ast.Expr {
	return func(expr ast.Expr) ast.Expr {
		// Relocate only free declaration references. Parser resolution objects
		// keep lambda locals (including a local named T) out of substitution.
		if owner.File != file {
			imports := map[string]string{}
			for _, spec := range owner.File.Tree.Imports {
				path, _ := strconv.Unquote(spec.Path.Value)
				alias := path[strings.LastIndex(path, "/")+1:]
				if spec.Name != nil {
					alias = spec.Name.Name
				}
				imports[alias] = path
			}
			ast.Inspect(expr, func(node ast.Node) bool {
				if sel, ok := node.(*ast.SelectorExpr); ok {
					if id, ok := sel.X.(*ast.Ident); ok && id.Obj == nil && imports[id.Name] != "" {
						id.Name = p.importAlias(owner.File, file, imports[id.Name], id.Name)
					}
				}
				return true
			})
		}
		if owner.Namespace != ns {
			qualified := map[string]string{}
			shadowed := map[string]bool{}
			for _, name := range classParameterNames(owner) {
				shadowed[name] = true
			}
			for name := range namespaceDeclarations(owner.Namespace) {
				qualified[name] = "ghi_default_owner." + name
			}
			used := map[string]bool{}
			expr = qualifySelectedTypes(expr, qualified, shadowed, used).(ast.Expr)
			if len(used) > 0 {
				alias := strings.TrimSuffix(p.classSymbol(owner, "", file, ns), ".")
				ast.Inspect(expr, func(node ast.Node) bool {
					if sel, ok := node.(*ast.SelectorExpr); ok {
						if id, ok := sel.X.(*ast.Ident); ok && id.Name == "ghi_default_owner" {
							id.Name = alias
						}
					}
					return true
				})
			}
		}
		return specializeDefault(expr, bindings)
	}
}

func freeClassParameters(expr ast.Expr, c *classDecl) map[string]bool {
	names := map[string]bool{}
	for _, name := range classParameterNames(c) {
		names[name] = true
	}
	ignored := map[*ast.Ident]bool{}
	ast.Inspect(expr, func(node ast.Node) bool {
		switch n := node.(type) {
		case *ast.SelectorExpr:
			ignored[n.Sel] = true
		case *ast.Field:
			for _, id := range n.Names {
				ignored[id] = true
			}
		case *ast.KeyValueExpr:
			if id, ok := n.Key.(*ast.Ident); ok {
				ignored[id] = true
			}
		}
		return true
	})
	used := map[string]bool{}
	ast.Inspect(expr, func(node ast.Node) bool {
		if id, ok := node.(*ast.Ident); ok && id.Obj == nil && !ignored[id] && names[id.Name] {
			used[id.Name] = true
		}
		return true
	})
	return used
}

func (p *program) inheritedCallBindings(child, ancestor *classDecl, receiver, value ast.Expr, info *types.Info, file *sourceFile, ns *namespace) map[string]ast.Expr {
	wanted := freeClassParameters(value, ancestor)
	if len(wanted) == 0 {
		return nil
	}
	args := p.ancestorArguments(child, ancestor)
	ancestorNames := classParameterNames(ancestor)
	needed := map[string]bool{}
	for i, arg := range args {
		if wanted[ancestorNames[i]] {
			for name := range freeClassParameters(arg, child) {
				needed[name] = true
			}
		}
	}
	concrete := make([]ast.Expr, len(classParameterNames(child)))
	if info != nil {
		if named, ok := types.Unalias(info.TypeOf(receiver)).(*types.Named); ok {
			for i := 0; i < named.TypeArgs().Len(); i++ {
				if i >= len(concrete) || !needed[classParameterNames(child)[i]] {
					continue
				}
				expr, _ := parser.ParseExpr(types.TypeString(named.TypeArgs().At(i), func(pkg *types.Package) string {
					if pkg.Path() == namespacePath(ns) {
						return ""
					}
					return p.importAlias(nil, file, pkg.Path(), pkg.Name())
				}))
				concrete[i] = expr
			}
		}
	}
	bindings := classBindings(child, concrete)
	for i, arg := range args {
		if !wanted[ancestorNames[i]] {
			args[i] = nil
			continue
		}
		relocated, _ := parser.ParseExpr(p.typeText(arg, child, file, ns))
		args[i] = substituteType(relocated, bindings)
	}
	return classBindings(ancestor, args)
}

// substituteType replaces type parameters, never selector members or field /
// parameter names. Each replacement is cloned, keeping the declaration reusable
// by unrelated descendants with different concrete arguments.
func substituteType(expr ast.Expr, bindings map[string]ast.Expr) ast.Expr {
	fields := func(list *ast.FieldList) {
		if list != nil {
			for _, field := range list.List {
				field.Type = substituteType(field.Type, bindings)
			}
		}
	}
	switch x := expr.(type) {
	case *ast.Ident:
		if x.Obj != nil {
			return x
		}
		if value := bindings[x.Name]; value != nil {
			copy, _ := parser.ParseExpr(expressionText(value))
			return copy
		}
	case *ast.SelectorExpr: // The left side names an imported package, not a type parameter.
	case *ast.IndexExpr:
		x.X = substituteType(x.X, bindings)
		x.Index = substituteType(x.Index, bindings)
	case *ast.IndexListExpr:
		x.X = substituteType(x.X, bindings)
		for i, arg := range x.Indices {
			x.Indices[i] = substituteType(arg, bindings)
		}
	case *ast.ArrayType:
		x.Elt = substituteType(x.Elt, bindings)
	case *ast.StarExpr:
		x.X = substituteType(x.X, bindings)
	case *ast.MapType:
		x.Key = substituteType(x.Key, bindings)
		x.Value = substituteType(x.Value, bindings)
	case *ast.ChanType:
		x.Value = substituteType(x.Value, bindings)
	case *ast.Ellipsis:
		x.Elt = substituteType(x.Elt, bindings)
	case *ast.ParenExpr:
		x.X = substituteType(x.X, bindings)
	case *ast.FuncType:
		fields(x.Params)
		fields(x.Results)
	case *ast.StructType:
		fields(x.Fields)
	case *ast.InterfaceType:
		fields(x.Methods)
	case *ast.UnaryExpr:
		x.X = substituteType(x.X, bindings)
	case *ast.BinaryExpr:
		x.X = substituteType(x.X, bindings)
		x.Y = substituteType(x.Y, bindings)
	}
	return expr
}

// ancestorArguments expresses an ancestor's parameters in the descendant's
// lexical namespace, composing every edge of a possibly generic chain.
func (p *program) ancestorArguments(child, ancestor *classDecl) []ast.Expr {
	if child == ancestor {
		var args []ast.Expr
		for _, name := range classParameterNames(child) {
			args = append(args, ast.NewIdent(name))
		}
		return args
	}
	if child.Parent == nil {
		return nil
	}
	inherited := p.ancestorArguments(child.Parent, ancestor)
	parentExpr, _ := parser.ParseExpr(child.ParentName)
	_, direct := genericBase(parentExpr)
	bindings := map[string]ast.Expr{}
	for i, name := range classParameterNames(child.Parent) {
		bindings[name] = direct[i]
	}
	for i, arg := range inherited {
		relocated, _ := parser.ParseExpr(p.typeText(arg, child.Parent, child.File, child.Namespace))
		inherited[i] = substituteType(relocated, bindings)
	}
	return inherited
}

func (p *program) ancestorArgumentText(child, ancestor *classDecl) string {
	return typeArgumentsText(p.ancestorArguments(child, ancestor))
}

func (p *program) ancestorMarkerParameters(child, ancestor *classDecl) string {
	return strings.TrimSuffix(strings.TrimPrefix(p.ancestorArgumentText(child, ancestor), "["), "]")
}
