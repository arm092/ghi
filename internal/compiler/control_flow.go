package compiler

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
)

type flowTarget struct {
	label                          string
	depth, breakCode, continueCode int
}
type returnSlot struct {
	name  string
	typ   ast.Expr
	named bool
}
type controlFlow struct {
	program  *program
	file     *sourceFile
	ns       *namespace
	slots    []returnSlot
	used     bool
	nextCode int
	nextTemp int
	err      error
	nested   map[*ast.FuncLit]bool
}

func callNamed(expression ast.Expr, name string) (*ast.CallExpr, bool) {
	call, ok := expression.(*ast.CallExpr)
	if !ok {
		return nil, false
	}
	id, ok := call.Fun.(*ast.Ident)
	return call, ok && id.Name == name
}
func intValue(value int) ast.Expr { return &ast.BasicLit{Kind: token.INT, Value: fmt.Sprint(value)} }
func signalSet(value int) ast.Stmt {
	return &ast.AssignStmt{Lhs: []ast.Expr{ast.NewIdent("ghi_flow")}, Tok: token.ASSIGN, Rhs: []ast.Expr{intValue(value)}}
}
func signalTest(value int, body ...ast.Stmt) ast.Stmt {
	return &ast.IfStmt{Cond: &ast.BinaryExpr{X: ast.NewIdent("ghi_flow"), Op: token.EQL, Y: intValue(value)}, Body: &ast.BlockStmt{List: body}}
}

func (p *program) lowerControl() error {
	for _, ns := range p.Ordered {
		for _, file := range ns.Files {
			if file.Unit.Native {
				continue
			}
			functions := []*ast.FuncDecl{}
			for _, decl := range file.Tree.Decls {
				if fn, ok := decl.(*ast.FuncDecl); ok {
					functions = append(functions, fn)
				}
			}
			for _, class := range file.Unit.Classes {
				for _, m := range class.Methods {
					functions = append(functions, m.Node)
				}
				if class.Constructor != nil {
					functions = append(functions, class.Constructor.Node)
				}
			}
			for _, fn := range functions {
				if err := p.lowerFunctionControl(fn.Type, fn.Body, file, ns); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
func (p *program) lowerFunctionControl(typ *ast.FuncType, body *ast.BlockStmt, file *sourceFile, ns *namespace) error {
	if body == nil {
		return nil
	}
	ctx := &controlFlow{program: p, file: file, ns: ns, nextCode: 2, nested: map[*ast.FuncLit]bool{}}
	if typ.Results != nil {
		for _, field := range typ.Results.List {
			if len(field.Names) > 0 {
				for _, name := range field.Names {
					ctx.slots = append(ctx.slots, returnSlot{name.Name, field.Type, true})
				}
			} else {
				ctx.slots = append(ctx.slots, returnSlot{fmt.Sprintf("ghi_return_%d", len(ctx.slots)), field.Type, false})
			}
		}
	}
	terminates := blockTerminates(body.List)
	body.List = ctx.block(body.List, 0, nil)
	if ctx.err != nil {
		return ctx.err
	}
	if ctx.used {
		prefix := []ast.Stmt{&ast.DeclStmt{Decl: &ast.GenDecl{Tok: token.VAR, Specs: []ast.Spec{&ast.ValueSpec{Names: []*ast.Ident{ast.NewIdent("ghi_flow")}, Type: ast.NewIdent("int")}}}}}
		for _, slot := range ctx.slots {
			if !slot.named {
				prefix = append(prefix, &ast.DeclStmt{Decl: &ast.GenDecl{Tok: token.VAR, Specs: []ast.Spec{&ast.ValueSpec{Names: []*ast.Ident{ast.NewIdent(slot.name)}, Type: slot.typ}}}})
			}
		}
		body.List = append(prefix, body.List...)
		if len(ctx.slots) > 0 && terminates {
			body.List = append(body.List, &ast.ExprStmt{X: &ast.CallExpr{Fun: ast.NewIdent("panic"), Args: []ast.Expr{&ast.BasicLit{Kind: token.STRING, Value: `"unreachable Ghi control flow"`}}}})
		}
	}
	return nil
}

func (c *controlFlow) expressions(expression ast.Expr) ast.Expr {
	if expression == nil {
		return nil
	}
	return walkNode(expression, func(node ast.Node) ast.Node {
		if fn, ok := node.(*ast.FuncLit); ok && !c.nested[fn] {
			c.nested[fn] = true
			if err := c.program.lowerFunctionControl(fn.Type, fn.Body, c.file, c.ns); err != nil {
				c.err = err
			}
		}
		return node
	}, false).(ast.Expr)
}
func (c *controlFlow) block(statements []ast.Stmt, depth int, targets []flowTarget) []ast.Stmt {
	var output []ast.Stmt
	for _, statement := range statements {
		output = append(output, c.statement(statement, depth, targets, "")...)
	}
	return output
}
func (c *controlFlow) statement(statement ast.Stmt, depth int, targets []flowTarget, label string) []ast.Stmt {
	if statement == nil {
		return nil
	}
	switch s := statement.(type) {
	case *ast.ExprStmt:
		if call, ok := callNamed(s.X, "GhiTry"); ok {
			return c.try(call, depth, targets)
		}
		s.X = c.expressions(s.X)
	case *ast.ReturnStmt:
		for i, value := range s.Results {
			s.Results[i] = c.expressions(value)
		}
		if depth > 0 {
			output := []ast.Stmt{}
			if len(s.Results) > 0 {
				names := []ast.Expr{}
				for _, slot := range c.slots {
					names = append(names, ast.NewIdent(slot.name))
				}
				if len(names) == 0 {
					c.err = fmt.Errorf("return value in a function without results")
					return []ast.Stmt{s}
				}
				output = append(output, &ast.AssignStmt{Lhs: names, Tok: token.ASSIGN, Rhs: s.Results})
			} else {
				for _, slot := range c.slots {
					if !slot.named {
						c.err = fmt.Errorf("return requires a value")
					}
				}
			}
			return append(output, signalSet(1), &ast.ReturnStmt{})
		}
	case *ast.BranchStmt:
		if s.Tok == token.BREAK || s.Tok == token.CONTINUE {
			for i := len(targets) - 1; i >= 0; i-- {
				target := targets[i]
				if s.Label != nil && s.Label.Name != target.label {
					continue
				}
				code := target.breakCode
				if s.Tok == token.CONTINUE {
					code = target.continueCode
					if code == 0 {
						continue
					}
				}
				if target.depth != depth {
					return []ast.Stmt{signalSet(code), &ast.ReturnStmt{}}
				}
				return []ast.Stmt{s}
			}
			c.err = fmt.Errorf("%s has no matching enclosing target", s.Tok)
		}
	case *ast.BlockStmt:
		s.List = c.block(s.List, depth, targets)
	case *ast.IfStmt:
		s.Cond = c.expressions(s.Cond)
		s.Body.List = c.block(s.Body.List, depth, targets)
		if s.Else != nil {
			s.Else = c.statement(s.Else, depth, targets, "")[0]
		}
	case *ast.ForStmt:
		s.Cond = c.expressions(s.Cond)
		target := flowTarget{label, depth, c.nextCode, c.nextCode + 1}
		c.nextCode += 2
		s.Body.List = c.block(s.Body.List, depth, append(targets, target))
	case *ast.RangeStmt:
		s.X = c.expressions(s.X)
		target := flowTarget{label, depth, c.nextCode, c.nextCode + 1}
		c.nextCode += 2
		s.Body.List = c.block(s.Body.List, depth, append(targets, target))
	case *ast.SwitchStmt:
		s.Tag = c.expressions(s.Tag)
		target := flowTarget{label: label, depth: depth, breakCode: c.nextCode}
		c.nextCode++
		s.Body.List = c.block(s.Body.List, depth, append(targets, target))
	case *ast.TypeSwitchStmt:
		target := flowTarget{label: label, depth: depth, breakCode: c.nextCode}
		c.nextCode++
		s.Body.List = c.block(s.Body.List, depth, append(targets, target))
	case *ast.SelectStmt:
		target := flowTarget{label: label, depth: depth, breakCode: c.nextCode}
		c.nextCode++
		s.Body.List = c.block(s.Body.List, depth, append(targets, target))
	case *ast.CaseClause:
		s.Body = c.block(s.Body, depth, targets)
	case *ast.CommClause:
		s.Body = c.block(s.Body, depth, targets)
	case *ast.LabeledStmt:
		lowered := c.statement(s.Stmt, depth, targets, s.Label.Name)
		if len(lowered) == 1 {
			s.Stmt = lowered[0]
		} else {
			s.Stmt = &ast.BlockStmt{List: lowered}
		}
	case *ast.AssignStmt:
		for i, value := range s.Rhs {
			s.Rhs[i] = c.expressions(value)
		}
	case *ast.DeclStmt:
		if decl, ok := s.Decl.(*ast.GenDecl); ok {
			for _, spec := range decl.Specs {
				if v, ok := spec.(*ast.ValueSpec); ok {
					for i, value := range v.Values {
						v.Values[i] = c.expressions(value)
					}
				}
			}
		}
	case *ast.DeferStmt:
		s.Call = c.expressions(s.Call).(*ast.CallExpr)
	case *ast.GoStmt:
		s.Call = c.expressions(s.Call).(*ast.CallExpr)
	}
	return []ast.Stmt{statement}
}

func (c *controlFlow) try(call *ast.CallExpr, depth int, targets []flowTarget) []ast.Stmt {
	c.used = true
	for i, argument := range call.Args {
		fn, ok := argument.(*ast.FuncLit)
		if !ok {
			continue
		}
		original := fn.Body.List
		if i == 1 {
			ast.Inspect(fn.Body, func(node ast.Node) bool {
				assertion, ok := node.(*ast.TypeAssertExpr)
				if !ok {
					return true
				}
				id, ok := assertion.X.(*ast.Ident)
				if !ok || id.Name != "ghi_caught" {
					return true
				}
				target := c.program.classNamed(expressionText(assertion.Type), c.file, c.ns)
				valid := false
				for base := target; base != nil; base = base.Parent {
					if base.Name == "Exception" && base.Namespace == c.program.Runtime {
						valid = true
						break
					}
				}
				if !valid {
					c.err = fmt.Errorf("catch type %s must derive from Exception", expressionText(assertion.Type))
				}
				return true
			})
		}
		fn.Body.List = c.block(original, depth+1, targets)
		if i == 1 {
			fn.Body.List = append([]ast.Stmt{signalSet(0)}, fn.Body.List...)
		}
		if i == 2 {
			c.nextTemp++
			saved := fmt.Sprintf("ghi_saved_%d", c.nextTemp)
			save := &ast.AssignStmt{Lhs: []ast.Expr{ast.NewIdent(saved)}, Tok: token.DEFINE, Rhs: []ast.Expr{ast.NewIdent("ghi_flow")}}
			restore := signalTest(0, &ast.AssignStmt{Lhs: []ast.Expr{ast.NewIdent("ghi_flow")}, Tok: token.ASSIGN, Rhs: []ast.Expr{ast.NewIdent(saved)}})
			deferred := &ast.DeferStmt{Call: &ast.CallExpr{Fun: &ast.FuncLit{Type: &ast.FuncType{Params: &ast.FieldList{}}, Body: &ast.BlockStmt{List: []ast.Stmt{restore}}}}}
			fn.Body.List = append([]ast.Stmt{save, signalSet(0), deferred}, fn.Body.List...)
		}
	}
	call.Fun, _ = parser.ParseExpr(c.program.runtimeSymbol("Try", c.file, c.ns))
	output := []ast.Stmt{&ast.ExprStmt{X: call}}
	emittedBreak, emittedContinue := false, false
	for i := len(targets) - 1; i >= 0; i-- {
		target := targets[i]
		if target.depth != depth {
			continue
		}
		var label *ast.Ident
		if target.label != "" {
			label = ast.NewIdent(target.label)
		}
		if !emittedBreak || label != nil {
			output = append(output, signalTest(target.breakCode, signalSet(0), &ast.BranchStmt{Tok: token.BREAK, Label: label}))
			emittedBreak = true
		}
		if target.continueCode != 0 && (!emittedContinue || label != nil) {
			output = append(output, signalTest(target.continueCode, signalSet(0), &ast.BranchStmt{Tok: token.CONTINUE, Label: label}))
			emittedContinue = true
		}
	}
	if depth > 0 {
		output = append(output, &ast.IfStmt{Cond: &ast.BinaryExpr{X: ast.NewIdent("ghi_flow"), Op: token.NEQ, Y: intValue(0)}, Body: &ast.BlockStmt{List: []ast.Stmt{&ast.ReturnStmt{}}}})
	} else {
		results := []ast.Expr{}
		for _, slot := range c.slots {
			results = append(results, ast.NewIdent(slot.name))
		}
		output = append(output, signalTest(1, &ast.ReturnStmt{Results: results}))
	}
	return output
}

func blockTerminates(statements []ast.Stmt) bool {
	if len(statements) == 0 {
		return false
	}
	switch s := statements[len(statements)-1].(type) {
	case *ast.ReturnStmt:
		return true
	case *ast.ExprStmt:
		if _, ok := callNamed(s.X, "GhiThrow"); ok {
			return true
		}
		if _, ok := callNamed(s.X, "panic"); ok {
			return true
		}
		if call, ok := callNamed(s.X, "GhiTry"); ok {
			body := call.Args[0].(*ast.FuncLit)
			if finalizer, ok := call.Args[2].(*ast.FuncLit); ok && blockTerminates(finalizer.Body.List) {
				return true
			}
			if !blockTerminates(body.Body.List) {
				return false
			}
			if handler, ok := call.Args[1].(*ast.FuncLit); ok {
				return blockTerminates(handler.Body.List)
			}
			return true
		}
	case *ast.BlockStmt:
		return blockTerminates(s.List)
	case *ast.IfStmt:
		return s.Else != nil && blockTerminates(s.Body.List) && blockTerminates([]ast.Stmt{s.Else})
	}
	return false
}
