package compiler

import (
	"fmt"
	"go/ast"
	"go/token"
	"strconv"
)

type initializedFields map[string]bool

func (s initializedFields) clone() initializedFields {
	r := initializedFields{}
	for k, v := range s {
		r[k] = v
	}
	return r
}
func intersectFields(a, b initializedFields) initializedFields {
	r := initializedFields{}
	for k := range a {
		if b[k] {
			r[k] = true
		}
	}
	return r
}

type constructorCheck struct {
	program  *program
	class    *classDecl
	required map[string]bool
	err      error
}

func (c *constructorCheck) reject(node ast.Node, message string) {
	if c.err == nil {
		c.err = fmt.Errorf("%s: constructor %s: %s", c.program.Fset.Position(node.Pos()), c.class.Name, message)
	}
}
func (c *constructorCheck) complete(node ast.Node, state initializedFields) {
	for name := range c.required {
		if !state[name] {
			if name == "@parent" {
				c.reject(node, "parent constructor must run on every normal exit")
				continue
			}
			c.reject(node, "nonnullable field "+name+" is not initialized on every normal exit")
		}
	}
}
func thisField(expr ast.Expr) (string, bool) {
	s, ok := expr.(*ast.SelectorExpr)
	if !ok {
		return "", false
	}
	id, ok := s.X.(*ast.Ident)
	return s.Sel.Name, ok && id.Name == "this"
}
func (c *constructorCheck) read(expr ast.Expr, state initializedFields) {
	if expr == nil {
		return
	}
	ast.Inspect(expr, func(node ast.Node) bool {
		switch n := node.(type) {
		case *ast.SelectorExpr:
			if name, ok := thisField(n); ok {
				if c.class.field(name) == nil {
					c.reject(n, "virtual methods cannot be called or captured during construction")
				}
				if c.required[name] && !state[name] {
					c.reject(n, "field "+name+" is read before initialization")
				}
				return false
			}
			if id, ok := n.X.(*ast.Ident); ok && id.Name == "parent" {
				c.reject(n, "parent methods cannot be called during construction")
				return false
			}
		case *ast.Ident:
			if n.Name == "this" {
				c.reject(n, "this cannot escape during construction; pass initialized fields instead")
			}
		}
		return true
	})
}
func (p *program) validateConstruction() error {
	for _, ns := range p.Ordered {
		for _, file := range ns.Files {
			for _, class := range file.Unit.Classes {
				if class.Interface {
					continue
				}
				check := &constructorCheck{program: p, class: class, required: map[string]bool{}}
				if class.Parent != nil {
					for _, stmt := range class.Constructor.Node.Body.List {
						if expr, ok := stmt.(*ast.ExprStmt); ok {
							if _, ok := callNamed(expr.X, "parent"); ok {
								check.required["@parent"] = true
							}
						}
					}
				}
				for _, field := range class.Fields {
					if p.sourceNeedsInitialization(field.Type, file, ns, map[string]bool{}) {
						check.required[field.Name] = true
					}
				}
				for _, exit := range check.paths(class.Constructor.Node.Body.List, []constructorExit{{state: initializedFields{}}}) {
					if exit.flow == token.ILLEGAL || exit.flow == token.RETURN {
						check.complete(class.Constructor.Node, exit.state)
					}
				}
				if check.err != nil {
					return check.err
				}
			}
		}
	}
	return nil
}

func (p *program) sourceNeedsInitialization(expr ast.Expr, file *sourceFile, ns *namespace, seen map[string]bool) bool {
	if p.classNamed(expressionText(expr), file, ns) != nil {
		return true
	}
	switch typ := expr.(type) {
	case *ast.ParenExpr:
		return p.sourceNeedsInitialization(typ.X, file, ns, seen)
	case *ast.ArrayType:
		if typ.Len == nil {
			return false
		}
		if literal, ok := typ.Len.(*ast.BasicLit); ok && literal.Value == "0" {
			return false
		}
		return p.sourceNeedsInitialization(typ.Elt, file, ns, seen)
	case *ast.StructType:
		for _, field := range typ.Fields.List {
			if p.sourceNeedsInitialization(field.Type, file, ns, seen) {
				return true
			}
		}
	case *ast.Ident:
		key := ns.Name + "." + typ.Name
		if seen[key] {
			return false
		}
		seen[key] = true
		for _, source := range ns.Files {
			for _, decl := range source.Tree.Decls {
				if group, ok := decl.(*ast.GenDecl); ok {
					for _, spec := range group.Specs {
						if definition, ok := spec.(*ast.TypeSpec); ok && definition.Name.Name == typ.Name {
							return p.sourceNeedsInitialization(definition.Type, source, ns, seen)
						}
					}
				}
			}
		}
	case *ast.SelectorExpr:
		if alias, ok := typ.X.(*ast.Ident); ok {
			for _, spec := range file.Tree.Imports {
				path, _ := strconv.Unquote(spec.Path.Value)
				for _, target := range p.Ordered {
					if namespacePath(target) != path {
						continue
					}
					name := target.GoName
					if spec.Name != nil {
						name = spec.Name.Name
					}
					if name == alias.Name {
						return p.sourceNeedsInitialization(ast.NewIdent(typ.Sel.Name), target.Files[0], target, seen)
					}
				}
			}
		}
	}
	return false
}
