package compiler

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
)

// specializeLeafReceivers runs after semantic validation. Only the private
// implementation entry points change: public class types remain interfaces.
// A class with descendants must retain a dynamic receiver for inherited bodies.
func (p *program) specializeLeafReceivers(info *types.Info) {
	classes := p.classes()
	parents := map[*classDecl]bool{}
	for _, c := range classes {
		if c.Parent != nil {
			parents[c.Parent] = true
		}
	}
	for _, c := range classes {
		if c.Interface || parents[c] {
			continue
		}
		methods := append([]*functionDecl{}, c.Methods...)
		methods = append(methods, c.Constructor)
		for _, method := range methods {
			fn := method.Node
			receiver := fn.Type.Params.List[0]
			object := info.Defs[receiver.Names[0]]
			if object == nil {
				continue
			}
			isReceiver := func(expression ast.Expr) bool {
				id, ok := unparen(expression).(*ast.Ident)
				return ok && info.Uses[id] == object
			}
			mutable := false
			ast.Inspect(fn.Body, func(node ast.Node) bool {
				switch n := node.(type) {
				case *ast.AssignStmt:
					for _, lhs := range n.Lhs {
						mutable = mutable || isReceiver(lhs)
					}
				case *ast.RangeStmt:
					mutable = mutable || isReceiver(n.Key) || isReceiver(n.Value)
				case *ast.UnaryExpr:
					mutable = mutable || n.Op == token.AND && isReceiver(n.X)
				}
				return !mutable
			})
			// Rebinding this or exposing its address needs the original interface
			// variable, including inside captured closures.
			if mutable {
				continue
			}
			ast.Inspect(fn.Body, func(node ast.Node) bool {
				if selector, ok := node.(*ast.SelectorExpr); ok && isReceiver(selector.X) {
					unparen(selector.X).(*ast.Ident).Name = "ghi_receiver"
				}
				return true
			})
			needsAlias := false
			ast.Inspect(fn.Body, func(node ast.Node) bool {
				if id, ok := node.(*ast.Ident); ok && id.Name == "this" && info.Uses[id] == object {
					needsAlias = true
				}
				return true
			})
			if needsAlias {
				// Keep inferred types of `alias := this`, arguments and returns
				// exactly as they were before specialization.
				alias := &ast.DeclStmt{Decl: &ast.GenDecl{Tok: token.VAR, Specs: []ast.Spec{
					&ast.ValueSpec{Names: []*ast.Ident{ast.NewIdent("this")}, Type: receiver.Type, Values: []ast.Expr{ast.NewIdent("ghi_receiver")}},
				}}}
				fn.Body.List = append([]ast.Stmt{alias}, fn.Body.List...)
			}
			receiver.Names[0].Name = "ghi_receiver"
			receiver.Type, _ = parser.ParseExpr("*ghiData_" + c.Name + c.typeArguments())
		}
	}
}
