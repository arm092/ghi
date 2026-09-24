package compiler

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
)

type resultExit struct {
	state nullProof
	flow  token.Token
	label string
}

type resultFlow struct {
	program  *program
	info     *types.Info
	required map[types.Object]bool
	failure  error
	callback int
}

func mergeResultExits(exits []resultExit) []resultExit {
	var result []resultExit
	for _, exit := range exits {
		found := false
		for i, previous := range result {
			if previous.flow == exit.flow && previous.label == exit.label {
				result[i].state = mergeProof(previous.state, exit.state)
				found = true
				break
			}
		}
		if !found {
			result = append(result, resultExit{exit.state.clone(), exit.flow, exit.label})
		}
	}
	return result
}

func (f *resultFlow) reject(node ast.Node, object types.Object) {
	if f.failure == nil {
		f.failure = fmt.Errorf("%s: nonnullable result %s is read before initialization", f.program.Fset.Position(node.Pos()), object.Name())
	}
}
func (f *resultFlow) read(node ast.Node, state nullProof) {
	if node == nil {
		return
	}
	ast.Inspect(node, func(child ast.Node) bool {
		if id, ok := child.(*ast.Ident); ok {
			object := f.info.Uses[id]
			if f.required[object] && !state[object] {
				f.reject(id, object)
			}
		}
		return true
	})
}
func (f *resultFlow) paths(list []ast.Stmt, exits []resultExit) []resultExit {
	for _, stmt := range list {
		var next []resultExit
		for _, exit := range exits {
			if exit.flow != token.ILLEGAL {
				next = append(next, exit)
				continue
			}
			next = append(next, f.step(stmt, exit.state)...)
		}
		exits = mergeResultExits(next)
	}
	return exits
}
func (f *resultFlow) step(stmt ast.Stmt, state nullProof) []resultExit {
	normal := func() []resultExit { return []resultExit{{state: state}} }
	switch s := stmt.(type) {
	case *ast.AssignStmt:
		for _, value := range s.Rhs {
			f.read(value, state)
		}
		for _, lhs := range s.Lhs {
			if id, ok := lhs.(*ast.Ident); ok && (s.Tok == token.ASSIGN || s.Tok == token.DEFINE) {
				if object := f.info.ObjectOf(id); f.required[object] {
					state[object] = true
				}
			} else {
				f.read(lhs, state)
			}
		}
	case *ast.ReturnStmt:
		for _, value := range s.Results {
			f.read(value, state)
		}
		if len(s.Results) == 0 && f.callback == 0 {
			for object := range f.required {
				if !state[object] {
					f.reject(s, object)
				}
			}
		}
		return []resultExit{{state: state, flow: token.RETURN}}
	case *ast.BranchStmt:
		if s.Tok == token.GOTO {
			// Continue checking later labels with no initialization proof.
			return []resultExit{{state: nullProof{}}}
		}
		label := ""
		if s.Label != nil {
			label = s.Label.Name
		}
		return []resultExit{{state: state, flow: s.Tok, label: label}}
	case *ast.BlockStmt:
		return f.paths(s.List, normal())
	case *ast.IfStmt:
		initial := f.step(s.Init, state)
		var output []resultExit
		for _, entry := range initial {
			if entry.flow != token.ILLEGAL {
				output = append(output, entry)
				continue
			}
			f.read(s.Cond, entry.state)
			output = append(output, f.paths(s.Body.List, []resultExit{{state: entry.state.clone()}})...)
			output = append(output, f.step(s.Else, entry.state.clone())...)
		}
		return mergeResultExits(output)
	case *ast.ForStmt:
		var output []resultExit
		for _, entry := range f.step(s.Init, state) {
			if entry.flow != token.ILLEGAL {
				output = append(output, entry)
				continue
			}
			f.read(s.Cond, entry.state)
			if s.Cond != nil {
				output = append(output, resultExit{state: entry.state.clone()})
			}
			for _, exit := range f.paths(s.Body.List, []resultExit{{state: entry.state.clone()}}) {
				switch {
				case exit.flow == token.BREAK && exit.label == "":
					exit.flow = token.ILLEGAL
					output = append(output, exit)
				case exit.flow == token.ILLEGAL || exit.flow == token.CONTINUE && exit.label == "":
					f.step(s.Post, exit.state)
				default:
					output = append(output, exit)
				}
			}
		}
		return mergeResultExits(output)
	case *ast.RangeStmt:
		f.read(s.X, state)
		output := normal()
		for _, exit := range f.paths(s.Body.List, []resultExit{{state: state.clone()}}) {
			if exit.flow != token.ILLEGAL && !(exit.label == "" && (exit.flow == token.BREAK || exit.flow == token.CONTINUE)) {
				output = append(output, exit)
			}
		}
		return mergeResultExits(output)
	case *ast.SwitchStmt:
		var output []resultExit
		for _, entry := range f.step(s.Init, state) {
			f.read(s.Tag, entry.state)
			output = append(output, f.cases(s.Body, entry.state)...)
		}
		return mergeResultExits(output)
	case *ast.TypeSwitchStmt:
		var output []resultExit
		for _, entry := range f.step(s.Init, state) {
			for _, assigned := range f.step(s.Assign, entry.state) {
				output = append(output, f.cases(s.Body, assigned.state)...)
			}
		}
		return mergeResultExits(output)
	case *ast.SelectStmt:
		var output []resultExit
		for _, stmt := range s.Body.List {
			clause := stmt.(*ast.CommClause)
			exits := f.paths(clause.Body, f.step(clause.Comm, state.clone()))
			for _, exit := range exits {
				if exit.flow == token.BREAK && exit.label == "" {
					exit.flow = token.ILLEGAL
				}
				output = append(output, exit)
			}
		}
		return mergeResultExits(output)
	case *ast.LabeledStmt:
		// Arbitrary jumps can bypass initialization; retaining no proof is safe.
		exits := f.step(s.Stmt, nullProof{})
		for i := range exits {
			if exits[i].label == s.Label.Name && (exits[i].flow == token.BREAK || exits[i].flow == token.CONTINUE) {
				exits[i] = resultExit{state: nullProof{}}
			}
		}
		return mergeResultExits(exits)
	case *ast.ExprStmt:
		if call, ok := s.X.(*ast.CallExpr); ok && runtimeCall(f.info, call, "Try") && len(call.Args) == 3 {
			return f.tryPaths(call, state)
		}
		f.read(s.X, state)
		if call, ok := s.X.(*ast.CallExpr); ok {
			if id, ok := call.Fun.(*ast.Ident); ok {
				if object, ok := f.info.ObjectOf(id).(*types.Builtin); ok && object.Name() == "panic" {
					return nil
				}
			}
		}
	default:
		f.read(stmt, state)
	}
	return normal()
}
func (f *resultFlow) cases(body *ast.BlockStmt, state nullProof) []resultExit {
	var output, fallen []resultExit
	hasDefault := false
	for _, stmt := range body.List {
		clause := stmt.(*ast.CaseClause)
		if clause.List == nil {
			hasDefault = true
		}
		for _, expr := range clause.List {
			f.read(expr, state)
		}
		incoming := append([]resultExit{{state: state.clone()}}, fallen...)
		fallen = nil
		for _, exit := range f.paths(clause.Body, mergeResultExits(incoming)) {
			switch {
			case exit.flow == token.BREAK && exit.label == "":
				exit.flow = token.ILLEGAL
				output = append(output, exit)
			case exit.flow == token.FALLTHROUGH:
				exit.flow = token.ILLEGAL
				fallen = append(fallen, exit)
			default:
				output = append(output, exit)
			}
		}
	}
	if !hasDefault {
		output = append(output, resultExit{state: state})
	}
	return mergeResultExits(output)
}

// Only the compiler runtime's Try function has synchronous exception callback
// semantics. A same-named user function must never receive this special proof.
func runtimeCall(info *types.Info, call *ast.CallExpr, name string) bool {
	var object types.Object
	switch expr := call.Fun.(type) {
	case *ast.Ident:
		object = info.ObjectOf(expr)
	case *ast.SelectorExpr:
		object = info.ObjectOf(expr.Sel)
	}
	return object != nil && object.Name() == name && object.Pkg() != nil && object.Pkg().Path() == generatedModule+"/ghi/runtime"
}
func (f *resultFlow) callbackPaths(fn *ast.FuncLit, state nullProof) []resultExit {
	f.callback++
	exits := f.paths(fn.Body.List, []resultExit{{state: state}})
	f.callback--
	for i := range exits {
		if exits[i].flow == token.RETURN {
			exits[i].flow = token.ILLEGAL
		}
	}
	return mergeResultExits(exits)
}
func (f *resultFlow) tryPaths(call *ast.CallExpr, state nullProof) []resultExit {
	body, ok := call.Args[0].(*ast.FuncLit)
	if !ok {
		f.read(call, state)
		return []resultExit{{state: state}}
	}
	exits := f.callbackPaths(body, state.clone())
	if handler, ok := call.Args[1].(*ast.FuncLit); ok {
		// Exceptions may occur before any assignment in the body has completed.
		exits = mergeResultExits(append(exits, f.callbackPaths(handler, state.clone())...))
	}
	if finalizer, ok := call.Args[2].(*ast.FuncLit); ok {
		// Validate finalizer reads even on exceptional paths, where initialization
		// from the body or handler is not guaranteed.
		f.callbackPaths(finalizer, state.clone())
		var output []resultExit
		for _, exit := range exits {
			output = append(output, f.callbackPaths(finalizer, exit.state)...)
		}
		exits = mergeResultExits(output)
	}
	return exits
}

// Named results start with zero values. Track every reachable branch rather
// than mistaking assignment targets inside compound statements for reads.
func (p *program) validateNamedResults(typ *ast.FuncType, body *ast.BlockStmt, info *types.Info) error {
	f := &resultFlow{program: p, info: info, required: map[types.Object]bool{}}
	if typ.Results == nil {
		return nil
	}
	for _, field := range typ.Results.List {
		for _, name := range field.Names {
			if object := info.Defs[name]; object != nil && p.needsInitialization(object.Type()) {
				f.required[object] = true
			}
		}
	}
	if len(f.required) == 0 {
		return nil
	}
	f.paths(body.List, []resultExit{{state: nullProof{}}})
	return f.failure
}
