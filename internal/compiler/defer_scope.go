package compiler

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
)

// Try is lowered to closures, but user defers still belong to the surrounding
// user function. Route all its defers through one list to preserve their order.
func (p *program) lowerScopedDefers(body *ast.BlockStmt, statements []*ast.DeferStmt, file *sourceFile, ns *namespace) {
	owned := map[ast.Node]bool{}
	for _, statement := range statements {
		owned[statement] = true
	}
	register, _ := parser.ParseExpr(p.runtimeSymbol("RegisterDefer", file, ns))
	walkNode(body, func(node ast.Node) ast.Node {
		if !owned[node] {
			return node
		}
		delete(owned, node)
		statement := node.(*ast.DeferStmt)
		// Keep a real call in a marker callback until type checking has supplied
		// its signature, and ordinary rewriting has supplied defaults/methods.
		callback := &ast.FuncLit{Type: &ast.FuncType{Params: &ast.FieldList{}}, Body: &ast.BlockStmt{List: []ast.Stmt{statement}}}
		call := &ast.CallExpr{Fun: register, Args: []ast.Expr{&ast.UnaryExpr{Op: token.AND, X: ast.NewIdent("ghi_defers")}, callback}}
		p.Wrapped[call] = true
		return &ast.ExprStmt{X: call}
	}, false)
	parsed, _ := parser.ParseFile(p.Fset, "defer-scope.go", "package generated\nfunc scope(){ var ghi_defers []func(); defer "+p.runtimeSymbol("RunDefers", file, ns)+"(&ghi_defers) }", 0)
	prefix := parsed.Decls[0].(*ast.FuncDecl).Body.List
	body.List = append(prefix, body.List...)
}

func (p *program) captureScopedDefer(call *ast.CallExpr, info *types.Info, file *sourceFile, ns *namespace) (bool, error) {
	if info == nil || !p.Wrapped[call] || len(call.Args) != 2 {
		return false, nil
	}
	marker, ok := call.Args[1].(*ast.FuncLit)
	if !ok || len(marker.Body.List) != 1 {
		return false, nil
	}
	statement, ok := marker.Body.List[0].(*ast.DeferStmt)
	if !ok {
		return false, nil
	}
	name := ""
	switch fun := call.Fun.(type) {
	case *ast.Ident:
		name = fun.Name
	case *ast.SelectorExpr:
		name = fun.Sel.Name
	}
	if name != "RegisterDefer" || expressionText(call.Fun) != p.runtimeSymbol("RegisterDefer", file, ns) {
		return false, nil
	}
	original := statement.Call
	signature, builtin := scheduledSignature(original, info)
	if signature == nil || !scheduledArgumentsReady(original, signature, info) {
		return false, nil
	}
	bridge := !p.Wrapped[original] && p.trailingErrorSignature(original, info) != nil
	// A zero-argument, zero-result function can be deferred directly. Besides
	// avoiding a wrapper, this preserves Go's direct recover requirement.
	if !builtin && !bridge && signature.Params().Len() == 0 && signature.Results().Len() == 0 {
		call.Args[1] = original.Fun
		return true, nil
	}
	captured, err := p.captureScheduledCall(original, signature, file, ns, false, bridge, builtin)
	if err != nil {
		return false, err
	}
	// captureScheduledCall returns factory(function)(arguments). Turn the
	// argument-taking callback into a factory returning a zero-argument thunk;
	// function and argument evaluation stay at the registration statement.
	var callback *ast.FuncLit
	if builtin {
		callback = captured.Fun.(*ast.FuncLit)
	} else {
		factory := captured.Fun.(*ast.CallExpr).Fun.(*ast.FuncLit)
		callback = factory.Body.List[0].(*ast.ReturnStmt).Results[0].(*ast.FuncLit)
	}
	thunk := &ast.FuncLit{Type: &ast.FuncType{Params: &ast.FieldList{}}, Body: callback.Body}
	if !bridge {
		// Forward an active panic into the thunk while deferring the original
		// invocation directly. Native recover inside a function with arguments
		// must not gain an ordinary caller frame from our capture wrapper.
		invoke := callback.Body.List[0].(*ast.ExprStmt).X.(*ast.CallExpr)
		parsed, _ := parser.ParseFile(p.Fset, "defer-panic.go", "package generated\nfunc forward(){ ghi_deferred_panic:=recover(); if ghi_deferred_panic!=nil { panic(ghi_deferred_panic) } }", 0)
		forward := parsed.Decls[0].(*ast.FuncDecl).Body.List
		thunk.Body = &ast.BlockStmt{List: []ast.Stmt{forward[0], &ast.DeferStmt{Call: invoke}, forward[1]}}
	}
	callback.Type.Results = &ast.FieldList{List: []*ast.Field{{Type: thunk.Type}}}
	callback.Body = &ast.BlockStmt{List: []ast.Stmt{&ast.ReturnStmt{Results: []ast.Expr{thunk}}}}
	call.Args[1] = captured
	return true, nil
}
