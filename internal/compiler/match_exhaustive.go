package compiler

import (
	"fmt"
	"go/ast"
	"go/types"
	"strings"
)

func (p *program) validateExhaustiveMatches(info *types.Info) error {
	for fn := range p.MatchFunctions {
		if len(fn.Body.List) == 0 {
			continue
		}
		switchNode, ok := fn.Body.List[0].(*ast.SwitchStmt)
		if !ok {
			continue
		}
		clauses := []*ast.CaseClause{}
		for _, statement := range switchNode.Body.List {
			clause := statement.(*ast.CaseClause)
			if clause.List == nil {
				clauses = nil
				break
			}
			clauses = append(clauses, clause)
		}
		if clauses == nil {
			continue
		}
		position := p.Fset.Position(switchNode.Tag.Pos())
		fail := func(message string) error { return fmt.Errorf("%s: match %s", position, message) }
		typ := info.TypeOf(switchNode.Tag)
		if typ == nil {
			continue
		} // Preserve the underlying type diagnostic.
		named, ok := types.Unalias(typ).(*types.Named)
		if !ok || named.Obj().Pkg() == nil {
			return fail("requires a final default arm unless its subject is a plain enum")
		}
		var enumeration *enumDecl
		for _, ns := range p.Ordered {
			if namespacePath(ns) != named.Obj().Pkg().Path() {
				continue
			}
			for _, file := range ns.Files {
				for _, enum := range file.Unit.Enums {
					if enum.Name == named.Obj().Name() && enum.Backing == "" {
						enumeration = enum
					}
				}
			}
		}
		if enumeration == nil {
			return fail("requires a final default arm unless its subject is a plain enum")
		}
		seen := map[string]bool{}
		for _, clause := range clauses {
			for _, candidate := range clause.List {
				call, ok := unparen(candidate).(*ast.CallExpr)
				if !ok {
					return fail("without default requires declared enum cases as candidates")
				}
				var object types.Object
				switch expr := call.Fun.(type) {
				case *ast.Ident:
					object = info.ObjectOf(expr)
				case *ast.SelectorExpr:
					object = info.ObjectOf(expr.Sel)
				}
				name := ""
				if object != nil && object.Pkg() == named.Obj().Pkg() {
					for _, c := range enumeration.Cases {
						if object.Name() == enumSymbol(enumeration.Name, c.Name) {
							name = c.Name
						}
					}
				}
				if name == "" {
					return fail("without default requires declared enum cases as candidates")
				}
				if seen[name] {
					return fail("has duplicate enum case " + enumeration.Name + "." + name)
				}
				seen[name] = true
			}
		}
		missing := []string{}
		for _, c := range enumeration.Cases {
			if !seen[c.Name] {
				missing = append(missing, enumeration.Name+"."+c.Name)
			}
		}
		if len(missing) != 0 {
			return fail("is not exhaustive; missing enum cases: " + strings.Join(missing, ", "))
		}
	}
	return nil
}
