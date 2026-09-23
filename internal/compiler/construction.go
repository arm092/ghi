package compiler

import (
	"fmt"
	"go/ast"
	"go/token"
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
func (c *constructorCheck) block(body *ast.BlockStmt, state initializedFields) (initializedFields, bool) {
	for _, stmt := range body.List {
		var live bool
		state, live = c.statement(stmt, state)
		if !live {
			return state, false
		}
	}
	return state, true
}
func (c *constructorCheck) statement(stmt ast.Stmt, state initializedFields) (initializedFields, bool) {
	switch s := stmt.(type) {
	case *ast.AssignStmt:
		for _, value := range s.Rhs {
			c.read(value, state)
		}
		for _, lhs := range s.Lhs {
			if name, ok := thisField(lhs); ok && s.Tok == token.ASSIGN {
				state[name] = true
			} else {
				c.read(lhs, state)
			}
		}
	case *ast.ExprStmt:
		if _, ok := callNamed(s.X, "GhiThrow"); ok {
			c.read(s.X, state)
			return state, false
		}
		c.read(s.X, state)
	case *ast.ReturnStmt:
		for _, value := range s.Results {
			c.read(value, state)
		}
		c.complete(s, state)
		return state, false
	case *ast.IfStmt:
		state, _ = c.statement(s.Init, state)
		c.read(s.Cond, state)
		yes, yesLive := c.block(s.Body, state.clone())
		no, noLive := c.statement(s.Else, state.clone())
		if !yesLive {
			return no, noLive
		}
		if !noLive {
			return yes, true
		}
		return intersectFields(yes, no), true
	case *ast.BlockStmt:
		return c.block(s, state)
	case *ast.ForStmt:
		state, _ = c.statement(s.Init, state)
		c.read(s.Cond, state)
		body, _ := c.block(s.Body, state.clone())
		c.statement(s.Post, body)
	case *ast.RangeStmt:
		c.read(s.X, state)
		c.block(s.Body, state.clone())
	case *ast.DeclStmt:
		if decl, ok := s.Decl.(*ast.GenDecl); ok {
			for _, spec := range decl.Specs {
				if value, ok := spec.(*ast.ValueSpec); ok {
					for _, expr := range value.Values {
						c.read(expr, state)
					}
				}
			}
		}
	default:
		if stmt != nil {
			ast.Inspect(stmt, func(node ast.Node) bool {
				if expr, ok := node.(ast.Expr); ok {
					c.read(expr, state)
					return false
				}
				return true
			})
		}
	}
	return state, true
}

func (p *program) validateConstruction() error {
	for _, ns := range p.Ordered {
		for _, file := range ns.Files {
			for _, class := range file.Unit.Classes {
				if class.Interface {
					continue
				}
				check := &constructorCheck{program: p, class: class, required: map[string]bool{}}
				for _, field := range class.Fields {
					if p.classNamed(expressionText(field.Type), file, ns) != nil {
						check.required[field.Name] = true
					}
				}
				state, live := check.block(class.Constructor.Node.Body, initializedFields{})
				if live {
					check.complete(class.Constructor.Node, state)
				}
				if check.err != nil {
					return check.err
				}
			}
		}
	}
	return nil
}
