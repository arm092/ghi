package compiler

import (
	"go/ast"
	"go/token"
	"go/types"
)

type nullProof map[types.Object]bool

func (proof nullProof) clone() nullProof {
	result := nullProof{}
	for object, value := range proof {
		result[object] = value
	}
	return result
}
func mergeProof(a, b nullProof) nullProof {
	result := nullProof{}
	for object := range a {
		if b[object] {
			result[object] = true
		}
	}
	return result
}

type nullableFlow struct {
	program  *program
	info     *types.Info
	unstable map[types.Object]bool
	scope    *types.Scope
	changed  bool
	results  *ast.FieldList
}

func (f *nullableFlow) checked(expr ast.Expr) *ast.StarExpr {
	star := &ast.StarExpr{X: expr}
	f.markChecked(star)
	return star
}
func (f *nullableFlow) markChecked(star *ast.StarExpr) {
	if f.program.CheckedDereferences == nil {
		f.program.CheckedDereferences = map[*ast.StarExpr]bool{}
	}
	f.program.CheckedDereferences[star] = true
}

func (f *nullableFlow) require(expr ast.Expr, expected types.Type, proof nullProof) ast.Expr {
	if !f.program.needsInitialization(expected) || !f.optional(expr) || !proof[f.object(expr)] {
		return expr
	}
	ptr := types.Unalias(f.info.TypeOf(expr)).(*types.Pointer)
	if !types.AssignableTo(ptr.Elem(), expected) {
		return expr
	}
	f.changed = true
	return f.checked(expr)
}

func (f *nullableFlow) object(expr ast.Expr) types.Object {
	id, ok := expr.(*ast.Ident)
	if !ok {
		return nil
	}
	return f.info.ObjectOf(id)
}
func (f *nullableFlow) optional(expr ast.Expr) bool {
	typ := f.info.TypeOf(expr)
	if typ == nil {
		return false
	}
	ptr, ok := types.Unalias(typ).(*types.Pointer)
	return ok && f.program.needsInitialization(ptr.Elem())
}
func (f *nullableFlow) assume(expr ast.Expr, truth bool, proof nullProof) nullProof {
	result := proof.clone()
	switch e := expr.(type) {
	case *ast.ParenExpr:
		return f.assume(e.X, truth, proof)
	case *ast.UnaryExpr:
		if e.Op == token.NOT {
			return f.assume(e.X, !truth, proof)
		}
	case *ast.BinaryExpr:
		if e.Op == token.LAND && truth {
			return f.assume(e.Y, true, f.assume(e.X, true, proof))
		}
		if e.Op == token.LOR && !truth {
			return f.assume(e.Y, false, f.assume(e.X, false, proof))
		}
		if e.Op == token.EQL || e.Op == token.NEQ {
			candidate := e.X
			nilValue := e.Y
			if id, ok := e.X.(*ast.Ident); ok && id.Name == "nil" {
				candidate = e.Y
				nilValue = e.X
			}
			id, ok := nilValue.(*ast.Ident)
			object := f.object(candidate)
			if ok && id.Name == "nil" && object != nil && f.optional(candidate) && !f.unstable[object] {
				local := false
				for scope := object.Parent(); scope != nil; scope = scope.Parent() {
					if scope == f.scope {
						local = true
						break
					}
				}
				if !local {
					return result
				}
				// Package variables can be changed by any call, including a callback.
				if object.Pkg() != nil && object.Parent() == object.Pkg().Scope() {
					return result
				}
				if truth == (e.Op == token.NEQ) {
					result[object] = true
				} else {
					delete(result, object)
				}
			}
		}
	}
	return result
}

func (f *nullableFlow) expression(expr ast.Expr, proof nullProof) {
	switch e := expr.(type) {
	case *ast.SelectorExpr:
		f.expression(e.X, proof)
		if f.optional(e.X) && proof[f.object(e.X)] {
			e.X = f.checked(e.X)
			f.changed = true
		}
	case *ast.BinaryExpr:
		f.expression(e.X, proof)
		right := proof
		if e.Op == token.LAND {
			right = f.assume(e.X, true, proof)
		}
		if e.Op == token.LOR {
			right = f.assume(e.X, false, proof)
		}
		f.expression(e.Y, right)
	case *ast.CallExpr:
		f.expression(e.Fun, proof)
		for i, arg := range e.Args {
			f.expression(arg, proof)
			if signature, ok := functionSignature(f.info.TypeOf(e.Fun)); ok && i < signature.Params().Len() && !(signature.Variadic() && i == signature.Params().Len()-1) {
				e.Args[i] = f.require(arg, signature.Params().At(i).Type(), proof)
			}
		}
	case *ast.ParenExpr:
		f.expression(e.X, proof)
	case *ast.UnaryExpr:
		f.expression(e.X, proof)
	case *ast.StarExpr:
		if f.optional(e.X) && proof[f.object(e.X)] {
			f.markChecked(e)
		}
		f.expression(e.X, proof)
	case *ast.IndexExpr:
		f.expression(e.X, proof)
		if f.optional(e.X) && proof[f.object(e.X)] {
			e.X = f.checked(e.X)
			f.changed = true
		}
		f.expression(e.Index, proof)
	case *ast.IndexListExpr:
		f.expression(e.X, proof)
		for _, index := range e.Indices {
			f.expression(index, proof)
		}
	case *ast.SliceExpr:
		f.expression(e.X, proof)
		if f.optional(e.X) && proof[f.object(e.X)] {
			e.X = f.checked(e.X)
			f.changed = true
		}
		f.expression(e.Low, proof)
		f.expression(e.High, proof)
		f.expression(e.Max, proof)
	case *ast.TypeAssertExpr:
		f.expression(e.X, proof)
	case *ast.CompositeLit:
		for _, value := range e.Elts {
			f.expression(value, proof)
		}
	case *ast.KeyValueExpr:
		f.expression(e.Key, proof)
		f.expression(e.Value, proof)
	case *ast.FuncLit:
		if f.program.narrowFunction(e.Type, e.Body, f.info) {
			f.changed = true
		}
	}
}

func (f *nullableFlow) block(body *ast.BlockStmt, proof nullProof) nullProof {
	if body == nil {
		return proof
	}
	for _, statement := range body.List {
		proof = f.statement(statement, proof)
	}
	return proof
}
func (f *nullableFlow) invalidateWrites(node ast.Node, proof nullProof) {
	ast.Inspect(node, func(n ast.Node) bool {
		switch s := n.(type) {
		case *ast.AssignStmt:
			for _, lhs := range s.Lhs {
				delete(proof, f.object(lhs))
			}
		case *ast.RangeStmt:
			delete(proof, f.object(s.Key))
			delete(proof, f.object(s.Value))
		}
		return true
	})
}
func (f *nullableFlow) statement(statement ast.Stmt, proof nullProof) nullProof {
	switch s := statement.(type) {
	case *ast.BlockStmt:
		return f.block(s, proof)
	case *ast.ExprStmt:
		f.expression(s.X, proof)
	case *ast.AssignStmt:
		for i, value := range s.Rhs {
			f.expression(value, proof)
			if len(s.Rhs) == len(s.Lhs) {
				s.Rhs[i] = f.require(value, f.info.TypeOf(s.Lhs[i]), proof)
			}
		}
		for _, lhs := range s.Lhs {
			f.expression(lhs, proof)
			delete(proof, f.object(lhs))
		}
	case *ast.DeclStmt:
		if decl, ok := s.Decl.(*ast.GenDecl); ok {
			for _, spec := range decl.Specs {
				if v, ok := spec.(*ast.ValueSpec); ok {
					for i, value := range v.Values {
						f.expression(value, proof)
						if len(v.Values) == len(v.Names) {
							if object := f.info.Defs[v.Names[i]]; object != nil {
								v.Values[i] = f.require(value, object.Type(), proof)
							}
						}
					}
				}
			}
		}
	case *ast.ReturnStmt:
		for _, value := range s.Results {
			f.expression(value, proof)
		}
		if f.results != nil {
			index := 0
			for _, field := range f.results.List {
				count := len(field.Names)
				if count == 0 {
					count = 1
				}
				for j := 0; j < count && index < len(s.Results); j++ {
					s.Results[index] = f.require(s.Results[index], f.info.TypeOf(field.Type), proof)
					index++
				}
			}
		}
	case *ast.IfStmt:
		proof = f.statement(s.Init, proof)
		f.expression(s.Cond, proof)
		yes := f.block(s.Body, f.assume(s.Cond, true, proof))
		no := f.statement(s.Else, f.assume(s.Cond, false, proof))
		if blockTerminates(s.Body.List) {
			return no
		}
		if other, ok := s.Else.(*ast.BlockStmt); ok && blockTerminates(other.List) {
			return yes
		}
		return mergeProof(yes, no)
	case *ast.ForStmt:
		proof = f.statement(s.Init, proof)
		f.invalidateWrites(s, proof)
		f.expression(s.Cond, proof)
		body := f.block(s.Body, f.assume(s.Cond, true, proof))
		f.statement(s.Post, body)
	case *ast.RangeStmt:
		f.expression(s.X, proof)
		if f.optional(s.X) && proof[f.object(s.X)] {
			s.X = f.checked(s.X)
			f.changed = true
		}
		f.invalidateWrites(s, proof)
		f.block(s.Body, proof.clone())
	case *ast.IncDecStmt:
		f.expression(s.X, proof)
	case *ast.SendStmt:
		f.expression(s.Chan, proof)
		f.expression(s.Value, proof)
		if typ := f.info.TypeOf(s.Chan); typ != nil {
			if channel, ok := typ.Underlying().(*types.Chan); ok {
				s.Value = f.require(s.Value, channel.Elem(), proof)
			}
		}
	case *ast.GoStmt:
		f.expression(s.Call, proof)
	case *ast.DeferStmt:
		f.expression(s.Call, proof)
	case *ast.SwitchStmt:
		proof = f.statement(s.Init, proof)
		f.expression(s.Tag, proof)
		f.switchCases(s, proof)
		f.invalidateWrites(s, proof)
	case *ast.TypeSwitchStmt:
		proof = f.statement(s.Init, proof)
		f.statement(s.Assign, proof)
		f.invalidateWrites(s, proof)
		f.block(s.Body, proof.clone())
	case *ast.SelectStmt:
		f.invalidateWrites(s, proof)
		f.block(s.Body, proof.clone())
	case *ast.CaseClause:
		for _, expr := range s.List {
			f.expression(expr, proof)
		}
		f.block(&ast.BlockStmt{List: s.Body}, proof.clone())
	case *ast.CommClause:
		f.statement(s.Comm, proof)
		f.block(&ast.BlockStmt{List: s.Body}, proof.clone())
	case *ast.LabeledStmt:
		// A jump can bypass a dominating nil check. Re-establish proof after labels.
		return f.statement(s.Stmt, nullProof{})
	}
	return proof
}

// Each case starts from its own condition, never a proof learned in a sibling.
// In an expressionless switch, failed earlier cases also narrow later cases.
func (f *nullableFlow) switchCases(statement *ast.SwitchStmt, proof nullProof) {
	remaining := proof.clone()
	caseProofs := make(map[*ast.CaseClause]nullProof)
	var defaultClause *ast.CaseClause
	for _, stmt := range statement.Body.List {
		clause := stmt.(*ast.CaseClause)
		if clause.List == nil {
			defaultClause = clause
			continue
		}
		var matched nullProof
		for _, condition := range clause.List {
			f.expression(condition, remaining)
			branch := remaining.clone()
			if statement.Tag == nil {
				branch = f.assume(condition, true, remaining)
				remaining = f.assume(condition, false, remaining)
			}
			if matched == nil {
				matched = branch
			} else {
				matched = mergeProof(matched, branch)
			}
		}
		caseProofs[clause] = matched
	}
	if defaultClause != nil {
		caseProofs[defaultClause] = remaining
	}
	fallthroughPossible := false
	for _, stmt := range statement.Body.List {
		clause := stmt.(*ast.CaseClause)
		incoming := caseProofs[clause]
		if fallthroughPossible {
			// Fallthrough skips the next condition entirely. Until exit-specific
			// proofs are tracked, carry no nullable assumptions across this edge.
			incoming = nullProof{}
		}
		f.block(&ast.BlockStmt{List: clause.Body}, incoming.clone())
		fallthroughPossible = false
		ast.Inspect(&ast.BlockStmt{List: clause.Body}, func(node ast.Node) bool {
			if _, ok := node.(*ast.FuncLit); ok {
				return false
			}
			if branch, ok := node.(*ast.BranchStmt); ok && branch.Tok == token.FALLTHROUGH {
				fallthroughPossible = true
			}
			return true
		})
	}
}

func (p *program) narrowFunction(typ *ast.FuncType, body *ast.BlockStmt, info *types.Info) bool {
	if body == nil {
		return false
	}
	f := &nullableFlow{program: p, info: info, scope: info.Scopes[typ], results: typ.Results, unstable: map[types.Object]bool{}}
	ast.Inspect(body, func(node ast.Node) bool {
		switch n := node.(type) {
		case *ast.FuncLit:
			ast.Inspect(n.Body, func(child ast.Node) bool {
				if id, ok := child.(*ast.Ident); ok {
					f.unstable[info.ObjectOf(id)] = true
				}
				return true
			})
			return false
		case *ast.UnaryExpr:
			if n.Op == token.AND {
				f.unstable[f.object(n.X)] = true
			}
		}
		return true
	})
	f.block(body, nullProof{})
	return f.changed
}

func (p *program) narrowNullable(info *types.Info) bool {
	changed := false
	for _, ns := range p.Ordered {
		for _, file := range ns.Files {
			if file.Unit.Native {
				continue
			}
			for _, decl := range file.Tree.Decls {
				if fn, ok := decl.(*ast.FuncDecl); ok && p.narrowFunction(fn.Type, fn.Body, info) {
					changed = true
				}
			}
		}
	}
	return changed
}
