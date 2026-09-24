package compiler

import (
	"fmt"
	"go/ast"
	"go/parser"
	"strings"
)

func (p *program) emitClass(c *classDecl) error {
	var out strings.Builder
	parameters, arguments := c.typeParameters(), c.typeArguments()
	receiver := "ghiData_" + c.Name + arguments
	fmt.Fprintf(&out, "package %s\ntype %s%s interface {\n", c.Namespace.GoName, c.Name, parameters)
	if !c.Interface {
		for a := c; a != nil; a = a.Parent {
			fmt.Fprintf(&out, "GhiIs_%s(%s)\n", a.key(), p.ancestorMarkerParameters(c, a))
		}
	}
	for _, field := range c.allFields() {
		typ := p.typeText(field.Type, field.Owner, c.File, c.Namespace, c)
		fmt.Fprintf(&out, "%s() %s\n%s(value %s)\n", fieldGet(field), typ, fieldSet(field), typ)
		fmt.Fprintf(&out, "%s() *%s\n", fieldRef(field), typ)
	}
	for _, m := range c.allMethods() {
		fmt.Fprintf(&out, "GhiM_%s(%s)%s\n", m.Name, p.parameters(m, c.File, c.Namespace, c), p.results(m, c.File, c.Namespace, c))
	}
	out.WriteString("}\n")
	if len(c.InterfaceNames) > 0 {
		fmt.Fprintf(&out, "func ghiVerify_%s%s(value %s%s) {\n", c.Name, parameters, c.Name, arguments)
		for _, name := range c.InterfaceNames {
			fmt.Fprintf(&out, "var _ %s = value\n", name)
		}
		out.WriteString("}\n")
	}
	if !c.Interface {
		fmt.Fprintf(&out, "type ghiData_%s%s struct {\n", c.Name, parameters)
		for _, f := range c.allFields() {
			fmt.Fprintf(&out, "F_%s_%s %s\n", f.Owner.key(), f.Name, p.typeText(f.Type, f.Owner, c.File, c.Namespace, c))
		}
		out.WriteString("}\n")
		for a := c; a != nil; a = a.Parent {
			fmt.Fprintf(&out, "func (this *%s) GhiIs_%s(%s) {}\n", receiver, a.key(), p.ancestorMarkerParameters(c, a))
		}
		for _, f := range c.allFields() {
			typ := p.typeText(f.Type, f.Owner, c.File, c.Namespace, c)
			fmt.Fprintf(&out, "func (this *%s) %s() %s { return this.F_%s_%s }\n", receiver, fieldGet(f), typ, f.Owner.key(), f.Name)
			fmt.Fprintf(&out, "func (this *%s) %s(value %s) { this.F_%s_%s = value }\n", receiver, fieldSet(f), typ, f.Owner.key(), f.Name)
			fmt.Fprintf(&out, "func (this *%s) %s() *%s { return &this.F_%s_%s }\n", receiver, fieldRef(f), typ, f.Owner.key(), f.Name)
		}
		for _, m := range c.allMethods() {
			ret := ""
			if m.Node.Type.Results != nil {
				ret = "return "
			}
			args := argumentNames(m)
			if args != "" {
				args = ", " + args
			}
			fmt.Fprintf(&out, "func (this *%s) GhiM_%s(%s)%s { %s%s%s(this%s) }\n", receiver, m.Name, p.parameters(m, c.File, c.Namespace, c), p.results(m, c.File, c.Namespace, c), ret, p.classSymbol(m.Owner, bodyName(m), c.File, c.Namespace), p.ancestorArgumentText(c, m.Owner), args)
		}
		ctor := c.Constructor
		args := argumentNames(ctor)
		if args != "" {
			args = ", " + args
		}
		metadata := ""
		for _, field := range c.allFields() {
			if field.Owner.Namespace.Name == runtimeNamespace && field.Owner.Name == "Exception" && field.Name == "typeName" {
				metadata = fmt.Sprintf("this.%s(%q); ", fieldSet(field), c.Namespace.Name+"."+c.Name)
			}
		}
		fmt.Fprintf(&out, "func GhiNew_%s%s(%s) %s%s { this := &%s{}; GhiInit_%s%s(this%s); %sreturn this }\n", c.Name, parameters, p.parameters(ctor, c.File, c.Namespace), c.Name, arguments, receiver, c.Name, arguments, args, metadata)
	}
	parsed, err := parser.ParseFile(p.Fset, c.File.Path+".generated", out.String(), parser.AllErrors|parser.SkipObjectResolution)
	if err != nil {
		return fmt.Errorf("generate class %s: %w", c.Name, err)
	}
	// Synthetic declarations belong to the source class, never a nonexistent
	// .generated file. Real method bodies retain their own original positions.
	generatedFile := p.Fset.File(parsed.Pos())
	for _, offset := range generatedFile.Lines() {
		generatedFile.AddLineColumnInfo(offset, c.File.Path, c.Line, 1)
	}
	c.File.Tree.Decls = append(c.File.Tree.Decls, parsed.Decls...)
	if c.Interface {
		return nil
	}
	methods := append([]*functionDecl{}, c.Methods...)
	methods = append(methods, c.Constructor)
	for _, m := range methods {
		name := bodyName(m)
		if m == c.Constructor {
			name = "GhiInit_" + c.Name
		}
		m.Node.Name = ast.NewIdent(name)
		thisType, _ := parser.ParseExpr(c.Name + arguments)
		if c.TypeParams != nil {
			prototype, err := parser.ParseFile(p.Fset, c.File.Path, "package parsed\nfunc F"+parameters+"() {}", parser.SkipObjectResolution)
			if err != nil {
				return err
			}
			m.Node.Type.TypeParams = prototype.Decls[0].(*ast.FuncDecl).Type.TypeParams
		}
		m.Node.Type.Params.List = append([]*ast.Field{{Names: []*ast.Ident{ast.NewIdent("this")}, Type: thisType}}, m.Node.Type.Params.List...)
		c.File.Tree.Decls = append(c.File.Tree.Decls, m.Node)
	}
	return nil
}

func (p *program) prepareConstructor(c *classDecl) error {
	if c.Interface {
		return nil
	}
	fn := c.Constructor
	parentCalls := 0
	seenThis := false
	for _, statement := range fn.Node.Body.List {
		if expr, ok := statement.(*ast.ExprStmt); ok {
			if call, ok := expr.X.(*ast.CallExpr); ok {
				if id, ok := call.Fun.(*ast.Ident); ok && id.Name == "parent" {
					if c.Parent == nil {
						return fmt.Errorf("%s: parent constructor used without a parent class", c.Name)
					}
					if seenThis || parentCalls > 0 {
						return fmt.Errorf("%s: parent(...) must execute once before accessing this", c.Name)
					}
					parentCalls++
					for _, argument := range call.Args {
						usesThis := false
						ast.Inspect(argument, func(n ast.Node) bool {
							if id, ok := n.(*ast.Ident); ok && id.Name == "this" {
								usesThis = true
							}
							return true
						})
						if usesThis {
							return fmt.Errorf("%s: this cannot be used in parent constructor arguments", c.Name)
						}
					}
					call.Fun, _ = parser.ParseExpr(p.classSymbol(c.Parent, "GhiInit_"+c.Parent.Name, c.File, c.Namespace) + p.ancestorArgumentText(c, c.Parent))
					call.Args = append([]ast.Expr{ast.NewIdent("this")}, call.Args...)
					continue
				}
			}
		}
		ast.Inspect(statement, func(n ast.Node) bool {
			if id, ok := n.(*ast.Ident); ok && id.Name == "this" {
				seenThis = true
			}
			return true
		})
	}
	if c.Parent != nil && parentCalls == 0 {
		parent := c.Parent.Constructor
		required := len(parent.Node.Type.Params.List) - len(parent.Defaults)
		// Constructors have not yet had their receiver parameter inserted.
		if required > 0 {
			return fmt.Errorf("%s: explicit parent(...) required", c.Name)
		}
		fun, _ := parser.ParseExpr(p.classSymbol(c.Parent, "GhiInit_"+c.Parent.Name, c.File, c.Namespace) + p.ancestorArgumentText(c, c.Parent))
		call := &ast.CallExpr{Fun: fun, Args: []ast.Expr{ast.NewIdent("this")}}
		fn.Node.Body.List = append([]ast.Stmt{&ast.ExprStmt{X: call}}, fn.Node.Body.List...)
	}
	return nil
}
