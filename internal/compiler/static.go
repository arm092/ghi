package compiler

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
)

func staticField(f *fieldDecl) string     { return "GhiStatic_" + f.Owner.Name + "_field_" + f.Name }
func staticRef(f *fieldDecl) string       { return "GhiStaticRef_" + f.Owner.Name + "_" + f.Name }
func staticMethod(m *functionDecl) string { return "GhiStatic_" + m.Owner.Name + "_method_" + m.Name }

// Keep constraints on the selected descendant even when the member is declared
// on a less constrained ancestor. The identity helper is inlined by Go.
func (p *program) staticChecked(value ast.Expr, class, owner *classDecl, args []ast.Expr, file *sourceFile, ns *namespace) ast.Expr {
	if class == owner || class.TypeParams == nil {
		return value
	}
	fun, _ := parser.ParseExpr(p.classSymbol(class, "GhiStaticType_"+class.Name, file, ns))
	call := &ast.CallExpr{Fun: staticIndexed(fun, args), Args: []ast.Expr{value}}
	p.Wrapped[call] = true
	return call
}

func (p *program) staticClass(expr ast.Expr, info *types.Info, file *sourceFile, ns *namespace) (*classDecl, []ast.Expr) {
	base, args := genericBase(expr)
	if id, ok := base.(*ast.Ident); ok && id.Obj != nil && id.Obj.Kind != ast.Typ {
		return nil, nil
	}
	if info != nil {
		if value, ok := info.Types[expr]; ok && value.Type != nil && !value.IsType() {
			return nil, nil
		}
	}
	return p.classNamed(expressionText(expr), file, ns), args
}

func (p *program) staticArguments(class, owner *classDecl, args []ast.Expr) []ast.Expr {
	if class == owner {
		return args
	}
	bindings := classBindings(class, args)
	result := p.ancestorArguments(class, owner)
	for i, arg := range result {
		result[i] = substituteType(arg, bindings)
	}
	return result
}

func staticIndexed(expr ast.Expr, args []ast.Expr) ast.Expr {
	if len(args) == 1 {
		return &ast.IndexExpr{X: expr, Index: args[0]}
	}
	if len(args) > 1 {
		return &ast.IndexListExpr{X: expr, Indices: args}
	}
	return expr
}

func (p *program) staticSelector(n *ast.SelectorExpr, caller *classDecl, info *types.Info, file *sourceFile, ns *namespace) (ast.Expr, bool, error) {
	c, args := p.staticClass(n.X, info, file, ns)
	if c == nil {
		return nil, false, nil
	}
	if len(args) != len(classParameterNames(c)) {
		return nil, true, fmt.Errorf("class %s expects %d type arguments, got %d", c.Name, len(classParameterNames(c)), len(args))
	}
	if field := c.field(n.Sel.Name); field != nil && field.Static {
		if !accessible(field.Visibility, field.Owner, caller) {
			return nil, true, fmt.Errorf("field %s is %s", field.Name, field.Visibility)
		}
		fun, _ := parser.ParseExpr(p.classSymbol(field.Owner, staticRef(field), file, ns))
		call := &ast.CallExpr{Fun: staticIndexed(fun, p.staticArguments(c, field.Owner, args))}
		p.Wrapped[call] = true
		star := &ast.StarExpr{Star: n.Pos(), X: p.staticChecked(call, c, field.Owner, args, file, ns)}
		if p.CheckedDereferences == nil {
			p.CheckedDereferences = map[*ast.StarExpr]bool{}
		}
		p.CheckedDereferences[star] = true
		p.recordStaticAnalysis(n, star)
		return star, true, nil
	}
	if method := c.method(n.Sel.Name); method != nil && method.Static {
		if !accessible(method.Visibility, method.Owner, caller) {
			return nil, true, fmt.Errorf("method %s is %s", method.Name, method.Visibility)
		}
		fun, _ := parser.ParseExpr(p.classSymbol(method.Owner, staticMethod(method), file, ns))
		ast.Inspect(fun, func(node ast.Node) bool {
			if id, ok := node.(*ast.Ident); ok {
				id.NamePos = n.Pos()
			}
			return true
		})
		value := p.staticChecked(staticIndexed(fun, p.staticArguments(c, method.Owner, args)), c, method.Owner, args, file, ns)
		p.recordStaticAnalysis(n, value)
		p.recordAnalysisMethod(n, method)
		return value, true, nil
	}
	return nil, false, nil
}

func (p *program) recordStaticAnalysis(source *ast.SelectorExpr, checked ast.Expr) {
	if !p.AnalyzeTypes {
		return
	}
	if p.AnalysisFieldReads == nil {
		p.AnalysisFieldReads = map[*ast.SelectorExpr]ast.Expr{}
	}
	p.AnalysisFieldReads[source] = checked
}

// Preserve positions on synthetic globals instead of attributing diagnostics to
// the short var wrapper used to parse a class field.
func staticFieldPosition(fset *token.FileSet, tree *ast.File, filename string, source []byte, name lexeme) {
	file := fset.File(tree.Pos())
	if file == nil {
		return
	}
	prefix := string(source[:name.Start])
	column := name.Start
	for i := len(prefix) - 1; i >= 0; i-- {
		if prefix[i] == '\n' {
			column = name.Start - i - 1
			break
		}
	}
	spec := tree.Decls[0].(*ast.GenDecl).Specs[0].(*ast.ValueSpec)
	offset := file.Offset(spec.Names[0].Pos())
	file.AddLineColumnInfo(offset, filename, name.Line, column+1)
}
