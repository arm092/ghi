package compiler

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"strings"
)

// nativeErrorSignature identifies native calls whose trailing error is bridged.
func nativeErrorSignature(call *ast.CallExpr, info *types.Info, values map[types.Object]bool) *types.Signature {
	if info == nil {
		return nil
	}
	base, _ := genericBase(unparen(call.Fun))
	var object types.Object
	var identifier *ast.Ident
	switch fun := base.(type) {
	case *ast.Ident:
		identifier = fun
		object = info.Uses[fun]
	case *ast.SelectorExpr:
		identifier = fun.Sel
		object = info.Uses[fun.Sel]
	}
	fn, isFunction := object.(*types.Func)
	if !values[object] && !(isFunction && fn.Pkg() != nil && !strings.HasPrefix(fn.Pkg().Path(), generatedModule)) {
		return nil
	}
	signature, ok := functionSignature(info.TypeOf(call.Fun))
	if identifier != nil {
		if instance, exists := info.Instances[identifier]; exists {
			signature, ok = functionSignature(instance.Type)
		}
	}
	if !ok || signature.Results().Len() == 0 {
		return nil
	}
	last := signature.Results().Len() - 1
	if !types.Identical(signature.Results().At(last).Type(), types.Universe.Lookup("error").Type()) {
		return nil
	}
	return signature
}

// captureScheduledCall preserves defer/go's eager function/argument capture while
// moving both the native invocation and error bridge to the eventual execution.
// A factory keeps the original arguments as a single call argument list, so
// tuple-valued arguments and variadic expansion retain ordinary Go semantics.
func (p *program) captureScheduledCall(call *ast.CallExpr, signature *types.Signature, file *sourceFile, ns *namespace, asynchronous, bridgeErrors bool, builtin bool) (*ast.CallExpr, error) {
	qualify := func(pkg *types.Package) string {
		if pkg.Path() == namespacePath(ns) {
			return ""
		}
		return p.importAlias(nil, file, pkg.Path(), pkg.Name())
	}
	var signatureType ast.Expr
	var err error
	if !builtin {
		signatureType, err = parser.ParseExpr(types.TypeString(signature, qualify))
	}
	if err != nil {
		return nil, fmt.Errorf("capture scheduled call signature: %w", err)
	}
	params := &ast.FieldList{}
	args := []ast.Expr{}
	for i := 0; i < signature.Params().Len(); i++ {
		paramType := types.Default(signature.Params().At(i).Type())
		variadic := signature.Variadic() && i == signature.Params().Len()-1
		if variadic {
			paramType = paramType.(*types.Slice).Elem()
		}
		typ, parseErr := parser.ParseExpr(types.TypeString(paramType, qualify))
		if parseErr != nil {
			return nil, parseErr
		}
		if variadic {
			typ = &ast.Ellipsis{Elt: typ}
		}
		name := fmt.Sprintf("_ghiNativeArg%d", i)
		params.List = append(params.List, &ast.Field{Names: []*ast.Ident{ast.NewIdent(name)}, Type: typ})
		args = append(args, ast.NewIdent(name))
	}
	invoke := &ast.CallExpr{Fun: ast.NewIdent("_ghiNativeFunction"), Args: args}
	if signature.Variadic() {
		invoke.Ellipsis = token.Pos(1)
	}
	p.Wrapped[invoke] = true
	var execution ast.Expr = invoke
	if builtin {
		invoke.Fun = call.Fun
	}
	if bridgeErrors {
		helper, _ := parser.ParseExpr(p.runtimeSymbol(p.ensureErrorHelper(signature.Results().Len()-1), file, ns))
		execution = &ast.CallExpr{Fun: helper, Args: []ast.Expr{invoke}}
	}
	callbackType := &ast.FuncType{Params: params}
	callback := &ast.FuncLit{Type: callbackType, Body: &ast.BlockStmt{List: []ast.Stmt{&ast.ExprStmt{X: execution}}}}
	if asynchronous {
		report, _ := parser.ParseExpr(p.runtimeSymbol("ReportPanic", file, ns))
		callback.Body.List = append([]ast.Stmt{&ast.DeferStmt{Call: &ast.CallExpr{Fun: report}}}, callback.Body.List...)
	}
	factory := &ast.FuncLit{Type: &ast.FuncType{
		Params:  &ast.FieldList{List: []*ast.Field{{Names: []*ast.Ident{ast.NewIdent("_ghiNativeFunction")}, Type: signatureType}}},
		Results: &ast.FieldList{List: []*ast.Field{{Type: callbackType}}},
	}, Body: &ast.BlockStmt{List: []ast.Stmt{&ast.ReturnStmt{Results: []ast.Expr{callback}}}}}
	var fun ast.Expr = &ast.CallExpr{Fun: factory, Args: []ast.Expr{call.Fun}}
	if builtin {
		fun = callback
	}
	replacement := &ast.CallExpr{Fun: fun, Args: call.Args, Ellipsis: call.Ellipsis, Lparen: call.Lparen, Rparen: call.Rparen}
	p.Wrapped[replacement] = true
	return replacement, nil
}

func scheduledSignature(call *ast.CallExpr, info *types.Info) (*types.Signature, bool) {
	if info == nil {
		return nil, false
	}
	signature, _ := functionSignature(info.TypeOf(call.Fun))
	base, _ := genericBase(unparen(call.Fun))
	var name *ast.Ident
	switch fun := base.(type) {
	case *ast.Ident:
		name = fun
	case *ast.SelectorExpr:
		name = fun.Sel
	}
	builtin := false
	if name != nil {
		if instance, ok := info.Instances[name]; ok {
			signature, _ = functionSignature(instance.Type)
		}
		_, builtin = info.Uses[name].(*types.Builtin)
	}
	return signature, builtin
}

func scheduledArgumentsReady(call *ast.CallExpr, signature *types.Signature, info *types.Info) bool {
	count := len(call.Args)
	if count == 1 {
		if tuple, ok := info.TypeOf(call.Args[0]).(*types.Tuple); ok {
			count = tuple.Len()
		}
	}
	required := signature.Params().Len()
	if signature.Variadic() {
		required--
	}
	return count >= required
}
