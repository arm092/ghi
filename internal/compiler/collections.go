package compiler

import (
	"fmt"
	"go/ast"
	"go/constant"
	"go/parser"
	"go/token"
	"go/types"
)

func (p *program) lowerClassMaps(info *types.Info) bool {
	changed := false
	for _, ns := range p.Ordered {
		for _, file := range ns.Files {
			if file.Unit.Native {
				continue
			}
			writes := map[*ast.IndexExpr]bool{}
			pairs := map[*ast.IndexExpr]bool{}
			ast.Inspect(file.Tree, func(node ast.Node) bool {
				if assignment, ok := node.(*ast.AssignStmt); ok {
					for _, lhs := range assignment.Lhs {
						if index, ok := unparen(lhs).(*ast.IndexExpr); ok {
							writes[index] = true
						}
					}
					if len(assignment.Lhs) == 2 && len(assignment.Rhs) == 1 {
						if index, ok := unparen(assignment.Rhs[0]).(*ast.IndexExpr); ok {
							pairs[index] = true
						}
					}
				}
				if value, ok := node.(*ast.ValueSpec); ok && len(value.Names) == 2 && len(value.Values) == 1 {
					if index, ok := unparen(value.Values[0]).(*ast.IndexExpr); ok {
						pairs[index] = true
					}
				}
				return true
			})
			for _, decl := range append([]ast.Decl(nil), file.Tree.Decls...) {
				walkNode(decl, func(node ast.Node) ast.Node {
					if binary, ok := node.(*ast.BinaryExpr); ok && (binary.Op == token.EQL || binary.Op == token.NEQ) {
						left, lok := info.TypeOf(binary.X).(*types.Pointer)
						right, rok := info.TypeOf(binary.Y).(*types.Pointer)
						if lok && rok && p.classType(left.Elem()) != nil && p.classType(right.Elem()) != nil && (types.AssignableTo(left.Elem(), right.Elem()) || types.AssignableTo(right.Elem(), left.Elem())) {
							fun, _ := parser.ParseExpr(p.runtimeSymbol("Equal", file, ns))
							call := &ast.CallExpr{Fun: fun, Args: []ast.Expr{binary.X, binary.Y}}
							changed = true
							if binary.Op == token.NEQ {
								return &ast.UnaryExpr{Op: token.NOT, X: call}
							}
							return call
						}
					}
					if slice, ok := node.(*ast.SliceExpr); ok {
						typ := info.TypeOf(slice.X)
						if typ != nil {
							if sequence, ok := typ.Underlying().(*types.Slice); ok && p.needsInitialization(sequence.Elem()) {
								fun, _ := parser.ParseExpr(p.runtimeSymbol("Slice", file, ns))
								low, high, max := slice.Low, slice.High, slice.Max
								if low == nil {
									low = intValue(0)
								}
								if high == nil {
									high = intValue(0)
								}
								if max == nil {
									max = intValue(0)
								}
								changed = true
								return &ast.CallExpr{Fun: fun, Args: []ast.Expr{slice.X, low, high, max, ast.NewIdent(fmt.Sprint(slice.High != nil)), ast.NewIdent(fmt.Sprint(slice.Max != nil))}}
							}
						}
					}
					index, ok := node.(*ast.IndexExpr)
					if !ok || writes[index] {
						return node
					}
					typ := info.TypeOf(index.X)
					if typ == nil {
						return node
					}
					mapping, ok := typ.Underlying().(*types.Map)
					if !ok || p.classType(mapping.Elem()) == nil {
						return node
					}
					name := "MapGet"
					if pairs[index] {
						name = "MapGetOK"
					}
					fun, _ := parser.ParseExpr(p.runtimeSymbol(name, file, ns))
					changed = true
					return &ast.CallExpr{Fun: fun, Args: []ast.Expr{index.X, index.Index}}
				}, false)
			}
		}
	}
	return changed
}

func unparen(expr ast.Expr) ast.Expr {
	for {
		if paren, ok := expr.(*ast.ParenExpr); ok {
			expr = paren.X
		} else {
			return expr
		}
	}
}

func (p *program) needsInitialization(typ types.Type) bool {
	if typ == nil {
		return false
	}
	if p.classType(typ) != nil {
		return true
	}
	switch t := typ.Underlying().(type) {
	case *types.Array:
		return t.Len() > 0 && p.needsInitialization(t.Elem())
	case *types.Struct:
		for i := 0; i < t.NumFields(); i++ {
			if p.needsInitialization(t.Field(i).Type()) {
				return true
			}
		}
	}
	return false
}

func integerValue(info *types.Info, expr ast.Expr) (int64, bool) {
	value := info.Types[expr].Value
	if value == nil {
		return 0, false
	}
	value = constant.ToInt(value)
	if value.Kind() != constant.Int {
		return 0, false
	}
	return constant.Int64Val(value)
}

func (p *program) checkCollection(node ast.Node, info *types.Info, check func(ast.Expr, types.Type), reject func(ast.Node, string)) {
	switch n := node.(type) {
	case *ast.CallExpr:
		id, ok := n.Fun.(*ast.Ident)
		if !ok {
			return
		}
		builtin, ok := info.Uses[id].(*types.Builtin)
		if !ok {
			return
		}
		switch builtin.Name() {
		case "new":
			if len(n.Args) == 1 && p.needsInitialization(info.TypeOf(n.Args[0])) {
				reject(n, "new cannot zero-initialize a nonnullable object; call its constructor or initialize every field")
			}
		case "make":
			if len(n.Args) >= 2 {
				if slice, ok := info.TypeOf(n).Underlying().(*types.Slice); ok && p.needsInitialization(slice.Elem()) {
					if length, known := integerValue(info, n.Args[1]); !known || length != 0 {
						reject(n, "zero-filled slices require nullable elements; use a complete literal or append initialized objects")
					}
				}
			}
		case "clear":
			if len(n.Args) == 1 {
				if slice, ok := info.TypeOf(n.Args[0]).Underlying().(*types.Slice); ok && p.needsInitialization(slice.Elem()) {
					reject(n, "clear would create nil elements in a nonnullable slice")
				}
			}
		case "append":
			if len(n.Args) > 1 && !n.Ellipsis.IsValid() {
				if slice, ok := info.TypeOf(n.Args[0]).Underlying().(*types.Slice); ok {
					for _, value := range n.Args[1:] {
						check(value, slice.Elem())
					}
				}
			}
		}
	case *ast.CompositeLit:
		typ := info.TypeOf(n)
		if typ == nil {
			return
		}
		switch t := typ.Underlying().(type) {
		case *types.Map:
			for _, entry := range n.Elts {
				if pair, ok := entry.(*ast.KeyValueExpr); ok {
					check(pair.Key, t.Key())
					check(pair.Value, t.Elem())
				}
			}
		case *types.Struct:
			assigned := map[int]bool{}
			for i, entry := range n.Elts {
				index := i
				value := entry
				if pair, ok := entry.(*ast.KeyValueExpr); ok {
					value = pair.Value
					index = -1
					if id, ok := pair.Key.(*ast.Ident); ok {
						for j := 0; j < t.NumFields(); j++ {
							if t.Field(j).Name() == id.Name {
								index = j
								break
							}
						}
					}
				}
				if index >= 0 && index < t.NumFields() {
					assigned[index] = true
					check(value, t.Field(index).Type())
				}
			}
			for i := 0; i < t.NumFields(); i++ {
				if !assigned[i] && p.needsInitialization(t.Field(i).Type()) {
					reject(n, "missing initializer for nonnullable field "+t.Field(i).Name())
				}
			}
		case *types.Array:
			p.checkSequence(n, t.Elem(), t.Len(), info, check, reject)
		case *types.Slice:
			p.checkSequence(n, t.Elem(), -1, info, check, reject)
		}
	}
}

func (p *program) checkSequence(lit *ast.CompositeLit, element types.Type, length int64, info *types.Info, check func(ast.Expr, types.Type), reject func(ast.Node, string)) {
	var next, maxIndex int64
	assigned := map[int64]bool{}
	for _, entry := range lit.Elts {
		value := entry
		if pair, ok := entry.(*ast.KeyValueExpr); ok {
			value = pair.Value
			if index, ok := integerValue(info, pair.Key); ok {
				next = index
			}
		}
		assigned[next] = true
		next++
		if next > maxIndex {
			maxIndex = next
		}
		check(value, element)
	}
	if length < 0 {
		length = maxIndex
	}
	if p.needsInitialization(element) && int64(len(assigned)) != length {
		reject(lit, "every nonnullable array or slice element requires an initializer")
	}
}
