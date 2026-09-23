package compiler

import (
	"fmt"
	"go/ast"
	"go/parser"
	"strings"
)

func (p *program) emitClass(c *classDecl) error {
	var out strings.Builder
	fmt.Fprintf(&out, "package %s\ntype %s interface {\n", c.Namespace.GoName, c.Name)
	if !c.Interface {
		for a := c; a != nil; a = a.Parent {
			fmt.Fprintf(&out, "GhiIs_%s()\n", a.key())
		}
	}
	for _, field := range c.allFields() {
		typ := p.typeText(field.Type, field.Owner, c.File, c.Namespace)
		fmt.Fprintf(&out, "%s() %s\n%s(value %s)\n", fieldGet(field), typ, fieldSet(field), typ)
	}
	for _, m := range c.allMethods() {
		fmt.Fprintf(&out, "GhiM_%s(%s)%s\n", m.Name, p.parameters(m, c.File, c.Namespace), p.results(m, c.File, c.Namespace))
	}
	out.WriteString("}\n")
	if !c.Interface {
		fmt.Fprintf(&out, "type ghiData_%s struct {\n", c.Name)
		for _, f := range c.allFields() {
			fmt.Fprintf(&out, "F_%s_%s %s\n", f.Owner.key(), f.Name, p.typeText(f.Type, f.Owner, c.File, c.Namespace))
		}
		out.WriteString("}\n")
		for a := c; a != nil; a = a.Parent {
			fmt.Fprintf(&out, "func (this *ghiData_%s) GhiIs_%s() {}\n", c.Name, a.key())
		}
		for _, f := range c.allFields() {
			typ := p.typeText(f.Type, f.Owner, c.File, c.Namespace)
			fmt.Fprintf(&out, "func (this *ghiData_%s) %s() %s { return this.F_%s_%s }\n", c.Name, fieldGet(f), typ, f.Owner.key(), f.Name)
			fmt.Fprintf(&out, "func (this *ghiData_%s) %s(value %s) { this.F_%s_%s = value }\n", c.Name, fieldSet(f), typ, f.Owner.key(), f.Name)
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
			fmt.Fprintf(&out, "func (this *ghiData_%s) GhiM_%s(%s)%s { %s%s(this%s) }\n", c.Name, m.Name, p.parameters(m, c.File, c.Namespace), p.results(m, c.File, c.Namespace), ret, p.classSymbol(m.Owner, bodyName(m), c.File, c.Namespace), args)
		}
		ctor := c.Constructor
		args := argumentNames(ctor)
		if args != "" {
			args = ", " + args
		}
		fmt.Fprintf(&out, "func GhiNew_%s(%s) %s { this := &ghiData_%s{}; GhiInit_%s(this%s); return this }\n", c.Name, p.parameters(ctor, c.File, c.Namespace), c.Name, c.Name, c.Name, args)
	}
	parsed, err := parser.ParseFile(p.Fset, c.File.Path+".generated", out.String(), parser.AllErrors|parser.SkipObjectResolution)
	if err != nil {
		return fmt.Errorf("generate class %s: %w", c.Name, err)
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
		m.Node.Type.Params.List = append([]*ast.Field{{Names: []*ast.Ident{ast.NewIdent("this")}, Type: ast.NewIdent(c.Name)}}, m.Node.Type.Params.List...)
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
					call.Fun, _ = parser.ParseExpr(p.classSymbol(c.Parent, "GhiInit_"+c.Parent.Name, c.File, c.Namespace))
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
		fun, _ := parser.ParseExpr(p.classSymbol(c.Parent, "GhiInit_"+c.Parent.Name, c.File, c.Namespace))
		call := &ast.CallExpr{Fun: fun, Args: []ast.Expr{ast.NewIdent("this")}}
		fn.Node.Body.List = append([]ast.Stmt{&ast.ExprStmt{X: call}}, fn.Node.Body.List...)
	}
	return nil
}
