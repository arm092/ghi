package compiler

import (
	"go/ast"
	"go/token"
)

const constructorThrow token.Token = -1

// ILLEGAL denotes ordinary fallthrough; constructorThrow denotes exception propagation.
// Keeping exits separate lets finally run before normal-exit validation.
type constructorExit struct {
	state initializedFields
	flow  token.Token
}

func mergeConstructorExits(exits []constructorExit) []constructorExit {
	result := []constructorExit{}
	for _, exit := range exits {
		found := false
		for i, previous := range result {
			if previous.flow == exit.flow {
				result[i].state = intersectFields(previous.state, exit.state)
				found = true
				break
			}
		}
		if !found {
			result = append(result, constructorExit{exit.state.clone(), exit.flow})
		}
	}
	return result
}

func (c *constructorCheck) paths(list []ast.Stmt, exits []constructorExit) []constructorExit {
	for _, stmt := range list {
		next := []constructorExit{}
		for _, exit := range exits {
			if exit.flow != token.ILLEGAL {
				next = append(next, exit)
				continue
			}
			next = append(next, c.step(stmt, exit.state)...)
		}
		exits = mergeConstructorExits(next)
	}
	return exits
}

func (c *constructorCheck) step(stmt ast.Stmt, state initializedFields) []constructorExit {
	normal := func() []constructorExit { return []constructorExit{{state, token.ILLEGAL}} }
	switch s := stmt.(type) {
	case *ast.AssignStmt:
		for _, expr := range s.Rhs {
			c.read(expr, state)
		}
		for _, lhs := range s.Lhs {
			if name, ok := thisField(lhs); ok && s.Tok == token.ASSIGN {
				state[name] = true
			} else {
				c.read(lhs, state)
			}
		}
	case *ast.ExprStmt:
		if call, ok := callNamed(s.X, "GhiTry"); ok {
			return c.tryPaths(call, state)
		}
		c.read(s.X, state)
		if _, ok := callNamed(s.X, "GhiThrow"); ok {
			return []constructorExit{{state, constructorThrow}}
		}
	case *ast.ReturnStmt:
		for _, expr := range s.Results {
			c.read(expr, state)
		}
		return []constructorExit{{state, token.RETURN}}
	case *ast.BranchStmt:
		if s.Label != nil || s.Tok == token.GOTO {
			c.reject(s, "labelled jumps are not supported during construction")
		}
		return []constructorExit{{state, s.Tok}}
	case *ast.BlockStmt:
		return c.paths(s.List, normal())
	case *ast.IfStmt:
		initial := c.step(s.Init, state)
		output := []constructorExit{}
		for _, entry := range initial {
			if entry.flow != token.ILLEGAL {
				output = append(output, entry)
				continue
			}
			c.read(s.Cond, entry.state)
			output = append(output, c.paths(s.Body.List, []constructorExit{{entry.state.clone(), token.ILLEGAL}})...)
			output = append(output, c.step(s.Else, entry.state.clone())...)
		}
		return mergeConstructorExits(output)
	case *ast.ForStmt:
		output := []constructorExit{}
		for _, entry := range c.step(s.Init, state) {
			if entry.flow != token.ILLEGAL {
				output = append(output, entry)
				continue
			}
			c.read(s.Cond, entry.state)
			infinite := s.Cond == nil
			if id, ok := s.Cond.(*ast.Ident); ok && id.Name == "true" {
				infinite = true
			}
			if !infinite {
				output = append(output, constructorExit{entry.state.clone(), token.ILLEGAL})
			}
			for _, exit := range c.paths(s.Body.List, []constructorExit{{entry.state.clone(), token.ILLEGAL}}) {
				switch exit.flow {
				case token.BREAK:
					exit.flow = token.ILLEGAL
					output = append(output, exit)
				case token.ILLEGAL, token.CONTINUE:
					c.step(s.Post, exit.state)
				default:
					output = append(output, exit)
				}
			}
		}
		return mergeConstructorExits(output)
	case *ast.RangeStmt:
		c.read(s.X, state)
		output := normal()
		for _, exit := range c.paths(s.Body.List, []constructorExit{{state.clone(), token.ILLEGAL}}) {
			if exit.flow == token.RETURN || exit.flow == constructorThrow {
				output = append(output, exit)
			}
		}
		return mergeConstructorExits(output)
	case *ast.SwitchStmt:
		output := []constructorExit{}
		for _, entry := range c.step(s.Init, state) {
			if entry.flow != token.ILLEGAL {
				output = append(output, entry)
				continue
			}
			c.read(s.Tag, entry.state)
			hasDefault := false
			var fallen []constructorExit
			for _, statement := range s.Body.List {
				clause := statement.(*ast.CaseClause)
				if clause.List == nil {
					hasDefault = true
				}
				for _, expr := range clause.List {
					c.read(expr, entry.state)
				}
				incoming := append([]constructorExit{{entry.state.clone(), token.ILLEGAL}}, fallen...)
				fallen = nil
				for _, exit := range c.paths(clause.Body, mergeConstructorExits(incoming)) {
					switch exit.flow {
					case token.BREAK:
						exit.flow = token.ILLEGAL
						output = append(output, exit)
					case token.FALLTHROUGH:
						exit.flow = token.ILLEGAL
						fallen = append(fallen, exit)
					default:
						output = append(output, exit)
					}
				}
			}
			if !hasDefault {
				output = append(output, entry)
			}
		}
		return mergeConstructorExits(output)
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
	return normal()
}

func (c *constructorCheck) tryPaths(call *ast.CallExpr, state initializedFields) []constructorExit {
	body, _ := call.Args[0].(*ast.FuncLit)
	handler, _ := call.Args[1].(*ast.FuncLit)
	finalizer, _ := call.Args[2].(*ast.FuncLit)
	exits := c.paths(body.Body.List, []constructorExit{{state.clone(), token.ILLEGAL}})
	if handler != nil {
		next := []constructorExit{}
		for _, exit := range exits {
			if exit.flow != constructorThrow {
				next = append(next, exit)
			}
		}
		// Any expression in try can throw before a field assignment completes.
		next = append(next, c.paths(handler.Body.List, []constructorExit{{state.clone(), token.ILLEGAL}})...)
		exits = mergeConstructorExits(next)
	} else if finalizer != nil {
		exits = append(exits, constructorExit{state.clone(), constructorThrow})
	}
	if finalizer == nil {
		return exits
	}
	output := []constructorExit{}
	for _, pending := range exits {
		for _, exit := range c.paths(finalizer.Body.List, []constructorExit{{pending.state.clone(), token.ILLEGAL}}) {
			if exit.flow == token.ILLEGAL {
				exit.flow = pending.flow
			}
			output = append(output, exit)
		}
	}
	return mergeConstructorExits(output)
}
