package compiler

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
)

// Named results start with Go zero values, so nonnullable results require a
// definite assignment before they are read or returned implicitly.
func (p *program) validateNamedResults(typ *ast.FuncType, body *ast.BlockStmt, info *types.Info) error {
	required := map[types.Object]bool{}
	if typ.Results == nil {
		return nil
	}
	for _, field := range typ.Results.List {
		for _, name := range field.Names {
			if object := info.Defs[name]; object != nil && p.classType(object.Type()) != nil {
				required[object] = true
			}
		}
	}
	if len(required) == 0 {
		return nil
	}
	var failure error
	reject := func(node ast.Node, object types.Object) {
		if failure == nil {
			failure = fmt.Errorf("%s: nonnullable result %s is read before initialization", p.Fset.Position(node.Pos()), object.Name())
		}
	}
	read := func(node ast.Node, state nullProof) {
		if node == nil {
			return
		}
		ast.Inspect(node, func(child ast.Node) bool {
			if id, ok := child.(*ast.Ident); ok {
				object := info.Uses[id]
				if required[object] && !state[object] {
					reject(id, object)
				}
			}
			return true
		})
	}
	var block func([]ast.Stmt, nullProof) (nullProof, bool)
	var statement func(ast.Stmt, nullProof) (nullProof, bool)
	block = func(list []ast.Stmt, state nullProof) (nullProof, bool) {
		for _, stmt := range list {
			var live bool
			state, live = statement(stmt, state)
			if !live {
				return state, false
			}
			if _, branch := stmt.(*ast.BranchStmt); branch {
				break
			}
		}
		return state, true
	}
	statement = func(stmt ast.Stmt, state nullProof) (nullProof, bool) {
		switch s := stmt.(type) {
		case *ast.AssignStmt:
			for _, value := range s.Rhs {
				read(value, state)
			}
			for _, lhs := range s.Lhs {
				if id, ok := lhs.(*ast.Ident); ok && (s.Tok == token.ASSIGN || s.Tok == token.DEFINE) {
					if object := info.ObjectOf(id); required[object] {
						state[object] = true
					}
				} else {
					read(lhs, state)
				}
			}
		case *ast.ReturnStmt:
			for _, value := range s.Results {
				read(value, state)
			}
			if len(s.Results) == 0 {
				for object := range required {
					if !state[object] {
						reject(s, object)
					}
				}
			}
			return state, false
		case *ast.IfStmt:
			state, _ = statement(s.Init, state)
			read(s.Cond, state)
			yes, yesLive := block(s.Body.List, state.clone())
			no, noLive := statement(s.Else, state.clone())
			if !yesLive {
				return no, noLive
			}
			if !noLive {
				return yes, true
			}
			return mergeProof(yes, no), true
		case *ast.BlockStmt:
			return block(s.List, state)
		case *ast.ForStmt:
			state, _ = statement(s.Init, state)
			read(s.Cond, state)
			after, _ := block(s.Body.List, state.clone())
			statement(s.Post, mergeProof(state, after))
		case *ast.RangeStmt:
			read(s.X, state)
			block(s.Body.List, state.clone())
		case *ast.LabeledStmt:
			return statement(s.Stmt, nullProof{})
		default:
			read(stmt, state)
		}
		return state, true
	}
	block(body.List, nullProof{})
	return failure
}
