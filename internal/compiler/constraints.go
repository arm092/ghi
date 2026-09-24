package compiler

import (
	"fmt"
	"go/ast"
	"go/types"
)

// constraintClass searches every embedded bound, retaining its instantiated
// type. A composite interface has no single class declaration of its own.
func (p *program) constraintClass(typ types.Type, accept func(*classDecl) bool) (*classDecl, *types.Named) {
	seen := map[types.Type]bool{}
	var visit func(types.Type) (*classDecl, *types.Named)
	visit = func(typ types.Type) (*classDecl, *types.Named) {
		if typ == nil {
			return nil, nil
		}
		typ = types.Unalias(typ)
		if seen[typ] {
			return nil, nil
		}
		seen[typ] = true
		if parameter, ok := typ.(*types.TypeParam); ok {
			return visit(parameter.Constraint())
		}
		if named, ok := typ.(*types.Named); ok {
			if c := p.classType(named); c != nil && accept(c) {
				return c, named
			}
		}
		if iface, ok := typ.Underlying().(*types.Interface); ok {
			for i := 0; i < iface.NumEmbeddeds(); i++ {
				if c, named := visit(iface.EmbeddedType(i)); c != nil {
					return c, named
				}
			}
		}
		return nil, nil
	}
	return visit(typ)
}

func (p *program) expressionMethod(expr ast.Expr, name string, info *types.Info) (*classDecl, *functionDecl) {
	if info == nil {
		return nil, nil
	}
	c, _ := p.constraintClass(info.TypeOf(expr), func(c *classDecl) bool { return c.method(name) != nil })
	if c == nil {
		return nil, nil
	}
	return c, c.method(name)
}

// Generated method names also exist for private implementations. Go's
// structural checker cannot enforce Ghi visibility at a generic boundary.
func (p *program) validateConstraintVisibility(info *types.Info) error {
	for id, instance := range info.Instances {
		object := info.Uses[id]
		if object == nil {
			continue
		}
		var parameters *types.TypeParamList
		switch typ := types.Unalias(object.Type()).(type) {
		case *types.Named:
			parameters = typ.TypeParams()
		case *types.Signature:
			parameters = typ.TypeParams()
		}
		for i := 0; i < parameters.Len() && i < instance.TypeArgs.Len(); i++ {
			actual := p.classType(instance.TypeArgs.At(i))
			if actual == nil || actual.Interface {
				continue
			}
			var hidden *functionDecl
			p.constraintClass(parameters.At(i).Constraint(), func(bound *classDecl) bool {
				if !bound.Interface {
					return false
				}
				for _, requirement := range bound.Methods {
					if method := actual.method(requirement.Name); method != nil && method.Visibility != "public" {
						hidden = method
						return true
					}
				}
				return false
			})
			if hidden != nil {
				return fmt.Errorf("%s: type %s cannot satisfy interface constraint: method %s is %s", p.Fset.Position(id.Pos()), actual.Name, hidden.Name, hidden.Visibility)
			}
		}
	}
	return nil
}
