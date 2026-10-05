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
	// Pointer type identity preserves declaration origin through identifier
	// uses, inference and container element access. Do not use types.Identical:
	// the backend intentionally has the same representation for ?T and *T.
	nullableTypes := map[types.Type]bool{}
	nativePointers := map[types.Type]bool{}
	getters := map[string]*fieldDecl{}
	for _, class := range p.classes() {
		for _, field := range class.Fields {
			getters[fieldGet(field)] = field
		}
	}
	var fieldOrigin func(ast.Expr, ast.Expr, *unit)
	fieldsOrigin := func(original, generated *ast.FieldList, unit *unit) {
		if original == nil || generated == nil || len(original.List) != len(generated.List) {
			return
		}
		for i := range original.List {
			fieldOrigin(original.List[i].Type, generated.List[i].Type, unit)
		}
	}
	fieldOrigin = func(original, generated ast.Expr, unit *unit) {
		switch original := original.(type) {
		case *ast.StarExpr:
			if target, ok := generated.(*ast.StarExpr); ok {
				if typ := info.TypeOf(target); typ != nil {
					if unit.NullableTypes[original] {
						nullableTypes[typ] = true
					}
					if unit.NativePointers[original] {
						nativePointers[typ] = true
					}
				}
				fieldOrigin(original.X, target.X, unit)
			}
		case *ast.ArrayType:
			if target, ok := generated.(*ast.ArrayType); ok {
				fieldOrigin(original.Elt, target.Elt, unit)
			}
		case *ast.MapType:
			if target, ok := generated.(*ast.MapType); ok {
				fieldOrigin(original.Key, target.Key, unit)
				fieldOrigin(original.Value, target.Value, unit)
			}
		case *ast.ChanType:
			if target, ok := generated.(*ast.ChanType); ok {
				fieldOrigin(original.Value, target.Value, unit)
			}
		case *ast.FuncType:
			if target, ok := generated.(*ast.FuncType); ok {
				fieldsOrigin(original.Params, target.Params, unit)
				fieldsOrigin(original.Results, target.Results, unit)
			}
		case *ast.StructType:
			if target, ok := generated.(*ast.StructType); ok {
				fieldsOrigin(original.Fields, target.Fields, unit)
			}
		case *ast.ParenExpr:
			if target, ok := generated.(*ast.ParenExpr); ok {
				fieldOrigin(original.X, target.X, unit)
			}
		case *ast.Ellipsis:
			if target, ok := generated.(*ast.Ellipsis); ok {
				fieldOrigin(original.Elt, target.Elt, unit)
			}
		case *ast.IndexExpr:
			if target, ok := generated.(*ast.IndexExpr); ok {
				fieldOrigin(original.Index, target.Index, unit)
			}
		case *ast.IndexListExpr:
			if target, ok := generated.(*ast.IndexListExpr); ok && len(original.Indices) == len(target.Indices) {
				for i := range original.Indices {
					fieldOrigin(original.Indices[i], target.Indices[i], unit)
				}
			}
		case *ast.InterfaceType:
			if target, ok := generated.(*ast.InterfaceType); ok {
				fieldsOrigin(original.Methods, target.Methods, unit)
			}
		}
	}
	for _, ns := range p.Ordered {
		for _, file := range ns.Files {
			ast.Inspect(file.Tree, func(node ast.Node) bool {
				var name string
				var signature *ast.FuncType
				switch node := node.(type) {
				case *ast.FuncDecl:
					name = node.Name.Name
					signature = node.Type
				case *ast.Field:
					if len(node.Names) == 1 {
						name = node.Names[0].Name
						signature, _ = node.Type.(*ast.FuncType)
					}
				}
				if field := getters[name]; field != nil && signature != nil && signature.Results != nil && len(signature.Results.List) == 1 {
					fieldOrigin(field.Type, signature.Results.List[0].Type, field.Owner.File.Unit)
				}
				return true
			})
			for star := range file.Unit.NullableTypes {
				if typ := info.TypeOf(star); typ != nil && info.Types[star].IsType() {
					nullableTypes[typ] = true
				}
			}
			for star := range file.Unit.NativePointers {
				if typ := info.TypeOf(star); typ != nil && info.Types[star].IsType() {
					nativePointers[typ] = true
				}
			}
		}
	}
	// Instantiation creates fresh pointer types. Carry the spelling origin from
	// generic declarations into substituted function and container signatures.
	var propagate func(types.Type, types.Type)
	seen := map[[2]types.Type]bool{}
	propagate = func(original, instantiated types.Type) {
		if original == nil || instantiated == nil {
			return
		}
		pair := [2]types.Type{original, instantiated}
		if seen[pair] {
			return
		}
		seen[pair] = true
		if nullableTypes[original] {
			nullableTypes[instantiated] = true
		}
		if nativePointers[original] {
			nativePointers[instantiated] = true
		}
		switch original := types.Unalias(original).(type) {
		case *types.Pointer:
			if target, ok := types.Unalias(instantiated).(*types.Pointer); ok {
				propagate(original.Elem(), target.Elem())
			}
		case *types.Signature:
			if target, ok := types.Unalias(instantiated).(*types.Signature); ok {
				propagate(original.Params(), target.Params())
				propagate(original.Results(), target.Results())
			}
		case *types.Tuple:
			if target, ok := types.Unalias(instantiated).(*types.Tuple); ok && original.Len() == target.Len() {
				for i := 0; i < original.Len(); i++ {
					propagate(original.At(i).Type(), target.At(i).Type())
				}
			}
		case *types.Slice:
			if target, ok := types.Unalias(instantiated).(*types.Slice); ok {
				propagate(original.Elem(), target.Elem())
			}
		case *types.Array:
			if target, ok := types.Unalias(instantiated).(*types.Array); ok {
				propagate(original.Elem(), target.Elem())
			}
		case *types.Map:
			if target, ok := types.Unalias(instantiated).(*types.Map); ok {
				propagate(original.Key(), target.Key())
				propagate(original.Elem(), target.Elem())
			}
		case *types.Chan:
			if target, ok := types.Unalias(instantiated).(*types.Chan); ok {
				propagate(original.Elem(), target.Elem())
			}
		case *types.Named:
			if target, ok := types.Unalias(instantiated).(*types.Named); ok {
				propagate(original.Underlying(), target.Underlying())
			}
		case *types.Struct:
			if target, ok := types.Unalias(instantiated).(*types.Struct); ok && original.NumFields() == target.NumFields() {
				for i := 0; i < original.NumFields(); i++ {
					propagate(original.Field(i).Type(), target.Field(i).Type())
				}
			}
		case *types.Interface:
			if target, ok := types.Unalias(instantiated).(*types.Interface); ok && original.NumMethods() == target.NumMethods() {
				for i := 0; i < original.NumMethods(); i++ {
					propagate(original.Method(i).Type(), target.Method(i).Type())
				}
			}
		}
	}
	for identifier, instance := range info.Instances {
		if object := info.Uses[identifier]; object != nil {
			// Compiler helpers return nullable wrappers for absent collection
			// values and explicit boxing, despite their native Go signatures.
			if p.Runtime != nil && object.Pkg() != nil && object.Pkg().Path() == namespacePath(p.Runtime) {
				switch object.Name() {
				case "Some", "MapGet", "MapGetOK", "Receive", "ReceiveOK", "Received", "Assert":
					if signature, ok := functionSignature(object.Type()); ok && signature.Results().Len() > 0 {
						nullableTypes[signature.Results().At(0).Type()] = true
					}
				}
			}
			propagate(object.Type(), instance.Type)
		}
	}
	for _, selection := range info.Selections {
		if function, ok := selection.Obj().(*types.Func); ok {
			propagate(function.Origin().Type(), selection.Type())
		}
	}
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
						name := ""
						if lok && rok {
							if _, generic := types.Unalias(left.Elem()).(*types.TypeParam); generic {
								if types.Identical(left.Elem(), right.Elem()) && nullableTypes[left] && nullableTypes[right] {
									name = "NullableEqual"
								}
							} else if !nativePointers[left] && !nativePointers[right] && p.classType(left.Elem()) != nil && p.classType(right.Elem()) != nil && (types.AssignableTo(left.Elem(), right.Elem()) || types.AssignableTo(right.Elem(), left.Elem())) {
								name = "Equal"
							}
						}
						if name != "" {
							fun, _ := parser.ParseExpr(p.runtimeSymbol(name, file, ns))
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
					if !ok || !p.needsInitialization(mapping.Elem()) {
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
	if typ != nil {
		if _, ok := types.Unalias(typ).(*types.TypeParam); ok {
			return true
		}
	}
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
