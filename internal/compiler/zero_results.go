package compiler

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
)

func (p *program) classReceive(expr ast.Expr, info *types.Info) (*ast.UnaryExpr, bool) {
	receive, ok := unparen(expr).(*ast.UnaryExpr)
	if !ok || receive.Op != token.ARROW {
		return nil, false
	}
	typ := info.TypeOf(receive.X)
	if typ == nil {
		return nil, false
	}
	channel, ok := typ.Underlying().(*types.Chan)
	return receive, ok && p.classType(channel.Elem()) != nil
}

func (p *program) lowerZeroResults(info *types.Info) bool {
	changed := false
	if p.LoweredReceives == nil {
		p.LoweredReceives = map[*ast.UnaryExpr]bool{}
	}
	for _, ns := range p.Ordered {
		for _, file := range ns.Files {
			if file.Unit.Native {
				continue
			}
			pairs := map[ast.Expr]bool{}
			selectReceives := map[*ast.UnaryExpr]bool{}
			helper := func(name string) ast.Expr { expr, _ := parser.ParseExpr(p.runtimeSymbol(name, file, ns)); return expr }
			ast.Inspect(file.Tree, func(node ast.Node) bool {
				switch n := node.(type) {
				case *ast.AssignStmt:
					if len(n.Lhs) == 2 && len(n.Rhs) == 1 {
						pairs[unparen(n.Rhs[0])] = true
					}
				case *ast.ValueSpec:
					if len(n.Names) == 2 && len(n.Values) == 1 {
						pairs[unparen(n.Values[0])] = true
					}
				case *ast.CommClause:
					switch communication := n.Comm.(type) {
					case *ast.ExprStmt:
						if receive, ok := p.classReceive(communication.X, info); ok {
							selectReceives[receive] = true
						}
					case *ast.AssignStmt:
						if len(communication.Rhs) != 1 {
							break
						}
						receive, ok := p.classReceive(communication.Rhs[0], info)
						if !ok {
							break
						}
						selectReceives[receive] = true
						if p.LoweredReceives[receive] {
							break
						}
						p.LoweredReceives[receive] = true
						p.ReceiveID++
						valueName := fmt.Sprintf("ghi_received_%d", p.ReceiveID)
						okName := fmt.Sprintf("ghi_received_ok_%d", p.ReceiveID)
						n.Comm = &ast.AssignStmt{Lhs: []ast.Expr{ast.NewIdent(valueName), ast.NewIdent(okName)}, Tok: token.DEFINE, Rhs: communication.Rhs}
						communication.Rhs = []ast.Expr{&ast.CallExpr{Fun: helper("Received"), Args: []ast.Expr{ast.NewIdent(valueName), ast.NewIdent(okName)}}}
						if len(communication.Lhs) == 2 {
							communication.Rhs = append(communication.Rhs, ast.NewIdent(okName))
						}
						n.Body = append([]ast.Stmt{communication}, n.Body...)
						changed = true
					}
				}
				return true
			})
			for _, decl := range append([]ast.Decl(nil), file.Tree.Decls...) {
				walkNode(decl, func(node ast.Node) ast.Node {
					if receive, ok := node.(*ast.UnaryExpr); ok && !selectReceives[receive] && !p.LoweredReceives[receive] {
						if _, ok := p.classReceive(receive, info); ok {
							name := "Receive"
							if pairs[receive] {
								name = "ReceiveOK"
							}
							changed = true
							return &ast.CallExpr{Fun: helper(name), Args: []ast.Expr{receive.X}}
						}
					}
					if assertion, ok := node.(*ast.TypeAssertExpr); ok && pairs[assertion] && p.classType(info.TypeOf(assertion.Type)) != nil {
						// Catch lowering already tests the assertion before exposing its variable.
						if id, ok := assertion.X.(*ast.Ident); ok && id.Name == "ghi_caught" {
							return node
						}
						changed = true
						return &ast.CallExpr{Fun: &ast.IndexExpr{X: helper("Assert"), Index: assertion.Type}, Args: []ast.Expr{assertion.X}}
					}
					return node
				}, false)
			}
		}
	}
	return changed
}
