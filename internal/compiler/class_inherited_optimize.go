package compiler

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"reflect"
	"sort"
	"strings"
)

// Keep original bodies for parent calls and dynamic receivers. Concrete method
// wrappers can use a private copy whose receiver type is known exactly.
func (p *program) specializeInheritedReceivers(info *types.Info) {
	classes := p.classes()
	parents := map[*classDecl]bool{}
	for _, c := range classes {
		if c.Parent != nil {
			parents[c.Parent] = true
		}
	}
	p.SourceCopies = map[ast.Node]ast.Node{}
	p.SpecializedNames = map[string]string{}
	names := make([]string, 0, len(classes))
	for name := range classes {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, className := range names {
		c := classes[className]
		if c.Interface {
			continue
		}
		for _, method := range c.allMethods() {
			if method.Owner == c && !parents[c] {
				continue
			}
			original := method.Node
			// Bound code duplication; large bodies are unlikely to inline and
			// retain the shared implementation instead.
			nodes := 0
			ast.Inspect(original.Body, func(node ast.Node) bool {
				if node != nil {
					nodes++
				}
				return nodes <= 128
			})
			if nodes > 128 {
				continue
			}
			copies := map[ast.Node]ast.Node{}
			copy := cloneSpecializationNode(original, copies).(*ast.FuncDecl)
			copyInfo := &types.Info{Defs: map[*ast.Ident]types.Object{}, Uses: map[*ast.Ident]types.Object{}}
			for node, source := range copies {
				if id, ok := node.(*ast.Ident); ok {
					copyInfo.Defs[id] = info.Defs[source.(*ast.Ident)]
					copyInfo.Uses[id] = info.Uses[source.(*ast.Ident)]
				}
			}
			if !specializeReceiver(copy, c, copyInfo) {
				continue
			}
			parameters := map[types.Object]int{}
			if copy.Type.TypeParams != nil {
				for _, field := range copy.Type.TypeParams.List {
					for _, id := range field.Names {
						parameters[copyInfo.Defs[id]] = len(parameters)
					}
				}
				copy.Type.TypeParams = nil
			}
			name := fmt.Sprintf("ghi_specialized_%d_%s_%s", len(c.Name), c.Name, method.Name)
			destination := method.Owner.File
			if method.Owner.Namespace != c.Namespace || method.Owner.TypeParams != nil || c.TypeParams != nil {
				// A separate file isolates imported names from the child's source.
				destination = &sourceFile{Path: c.File.Path + "." + name, Unit: &unit{},
					Tree: &ast.File{Name: ast.NewIdent(c.Namespace.GoName)}}
				if !p.relocateSpecialization(copy, copyInfo, destination, c.Namespace, copies) {
					continue
				}
			}
			p.bindSpecializationTypes(copy, c, method.Owner, destination, copyInfo, parameters, copies, name)
			copy.Name = ast.NewIdent(name)
			destination.Tree.Decls = append(destination.Tree.Decls, copy)
			if destination != method.Owner.File {
				c.Namespace.Files = append(c.Namespace.Files, destination)
			}
			for node, source := range copies {
				p.SourceCopies[node] = source
			}
			for _, decl := range c.File.Tree.Decls {
				wrapper, ok := decl.(*ast.FuncDecl)
				if !ok || wrapper.Recv == nil || wrapper.Name.Name != "GhiM_"+method.Name {
					continue
				}
				ptr, ok := wrapper.Recv.List[0].Type.(*ast.StarExpr)
				if !ok {
					continue
				}
				receiver := ptr.X
				switch indexed := receiver.(type) {
				case *ast.IndexExpr:
					receiver = indexed.X
				case *ast.IndexListExpr:
					receiver = indexed.X
				}
				id, ok := receiver.(*ast.Ident)
				if !ok || id.Name != "ghiData_"+c.Name {
					continue
				}
				ast.Inspect(wrapper.Body, func(node ast.Node) bool {
					if call, ok := node.(*ast.CallExpr); ok {
						call.Fun, _ = parser.ParseExpr(name + c.typeArguments())
						return false
					}
					return true
				})
			}
			prefix := generatedModule + "/" + strings.ReplaceAll(c.Namespace.Name, ".", "/")
			if c.Namespace.Name == "main" {
				prefix = "main"
			}
			p.SpecializedNames[prefix+"."+name] = method.Owner.Namespace.Name + "." + method.Owner.Name + "." + method.Name
		}
	}
}

// Generated aliases and parameter names prevent substitution from capturing
// locals whose spelling matches a descendant's concrete type or type parameter.
func (p *program) bindSpecializationTypes(fn *ast.FuncDecl, child, owner *classDecl, file *sourceFile, info *types.Info, parameters map[types.Object]int, copies map[ast.Node]ast.Node, name string) {
	bindings := map[string]ast.Expr{}
	var names []string
	for i, original := range classParameterNames(child) {
		fresh := fmt.Sprintf("ghi_type_%d", i)
		bindings[original] = ast.NewIdent(fresh)
		names = append(names, fresh)
	}
	arguments := ""
	if len(names) > 0 {
		arguments = "[" + strings.Join(names, ",") + "]"
	}
	if child.TypeParams != nil {
		fn.Type.TypeParams = cloneSpecializationNode(child.TypeParams, map[ast.Node]ast.Node{}).(*ast.FieldList)
		for _, field := range fn.Type.TypeParams.List {
			for _, id := range field.Names {
				id.Name = bindings[id.Name].(*ast.Ident).Name
			}
			field.Type, _ = parser.ParseExpr(p.typeText(field.Type, child, file, child.Namespace))
			field.Type = substituteType(field.Type, bindings)
		}
		fn.Type.Params.List[0].Type = substituteType(fn.Type.Params.List[0].Type, bindings)
	}
	args := p.ancestorArguments(child, owner)
	aliases := map[int]string{}
	walkNode(fn, func(node ast.Node) ast.Node {
		id, ok := node.(*ast.Ident)
		if !ok {
			return node
		}
		i, ok := parameters[info.Uses[id]]
		if !ok {
			return node
		}
		alias := aliases[i]
		if alias == "" {
			typ, _ := parser.ParseExpr(p.typeText(args[i], child, file, child.Namespace))
			typ = substituteType(typ, bindings)
			if id, ok := typ.(*ast.Ident); ok {
				for _, fresh := range names {
					if id.Name == fresh {
						aliases[i] = fresh
						return typ
					}
				}
			}
			alias = fmt.Sprintf("%s_arg_%d", name, i)
			spec := &ast.TypeSpec{Name: ast.NewIdent(alias), Assign: token.Pos(1), Type: typ}
			if fn.Type.TypeParams != nil {
				spec.TypeParams = cloneSpecializationNode(fn.Type.TypeParams, map[ast.Node]ast.Node{}).(*ast.FieldList)
			}
			file.Tree.Decls = append(file.Tree.Decls, &ast.GenDecl{Tok: token.TYPE, Specs: []ast.Spec{spec}})
			alias += arguments
			aliases[i] = alias
		}
		expr, _ := parser.ParseExpr(alias)
		copies[expr] = copies[id]
		return expr
	}, true)
}

// Bind a moved body using resolved objects, never identifier spelling. All its
// dependencies are already dependencies of the ancestor, so importing them from
// the descendant cannot introduce a namespace cycle. Inaccessible declarations
// retain the shared implementation rather than widening source visibility.
func (p *program) relocateSpecialization(fn *ast.FuncDecl, info *types.Info, file *sourceFile, ns *namespace, copies map[ast.Node]ast.Node) bool {
	local := map[types.Object]bool{}
	reserved := map[string]bool{}
	packageNames := map[string]bool{}
	members := map[*ast.Ident]bool{}
	ast.Inspect(fn, func(node ast.Node) bool {
		if id, ok := node.(*ast.Ident); ok {
			reserved[id.Name] = true
			if obj := info.Defs[id]; obj != nil {
				local[obj] = true
			}
		}
		if sel, ok := node.(*ast.SelectorExpr); ok {
			members[sel.Sel] = true
		}
		return true
	})
	// Imports must also avoid declarations in the destination package.
	for _, source := range ns.Files {
		for _, decl := range source.Tree.Decls {
			switch decl := decl.(type) {
			case *ast.FuncDecl:
				if decl.Recv == nil {
					packageNames[decl.Name.Name] = true
				}
			case *ast.GenDecl:
				for _, spec := range decl.Specs {
					switch spec := spec.(type) {
					case *ast.TypeSpec:
						packageNames[spec.Name.Name] = true
					case *ast.ValueSpec:
						for _, name := range spec.Names {
							packageNames[name.Name] = true
						}
					}
				}
			}
		}
		ast.Inspect(source.Tree, func(node ast.Node) bool {
			if id, ok := node.(*ast.Ident); ok {
				reserved[id.Name] = true
			}
			return true
		})
	}
	accessible := true
	for id, obj := range info.Uses {
		// Universe names cannot be qualified. A destination declaration such
		// as len or int would silently change the original binding.
		if obj != nil && obj.Parent() == types.Universe && packageNames[id.Name] {
			return false
		}
		if obj == nil || local[obj] || obj.Pkg() == nil || obj.Pkg().Path() == namespacePath(ns) {
			continue
		}
		if _, imported := obj.(*types.PkgName); imported {
			continue
		}
		if (members[id] || obj.Parent() == obj.Pkg().Scope()) && !obj.Exported() {
			accessible = false
			break
		}
	}
	if !accessible {
		return false
	}
	aliases := map[string]string{}
	qualifier := func(pkg *types.Package) string {
		if alias := aliases[pkg.Path()]; alias != "" {
			return alias
		}
		alias := p.importAlias(nil, file, pkg.Path(), pkg.Name())
		for reserved[alias] {
			alias += "_"
		}
		file.Tree.Imports[len(file.Tree.Imports)-1].Name.Name = alias
		reserved[alias] = true
		aliases[pkg.Path()] = alias
		return alias
	}
	walkNode(fn, func(node ast.Node) ast.Node {
		id, ok := node.(*ast.Ident)
		if !ok || members[id] {
			return node
		}
		obj := info.Uses[id]
		if obj == nil || local[obj] {
			return node
		}
		if pkg, ok := obj.(*types.PkgName); ok {
			id.Name = qualifier(pkg.Imported())
			return id
		}
		if obj.Pkg() == nil || obj.Parent() != obj.Pkg().Scope() || obj.Pkg().Path() == namespacePath(ns) {
			return node
		}
		result := &ast.SelectorExpr{X: ast.NewIdent(qualifier(obj.Pkg())), Sel: id}
		copies[result] = copies[id]
		return result
	}, true)
	return true
}

// Clone AST nodes, retaining a source map. Resolution objects and scopes are
// deliberately shared: they are not traversed and go/types supplies bindings.
func cloneSpecializationNode(source ast.Node, copies map[ast.Node]ast.Node) ast.Node {
	value := reflect.ValueOf(source)
	if !value.IsValid() || value.IsNil() {
		return source
	}
	result := reflect.New(value.Elem().Type())
	result.Elem().Set(value.Elem())
	copy := result.Interface().(ast.Node)
	copies[copy] = source
	cloneSlot := func(slot reflect.Value) {
		if !slot.CanInterface() {
			return
		}
		if child, ok := slot.Interface().(ast.Node); ok {
			slot.Set(reflect.ValueOf(cloneSpecializationNode(child, copies)))
		}
	}
	for i := 0; i < result.Elem().NumField(); i++ {
		field := result.Elem().Field(i)
		if field.Kind() == reflect.Slice && !field.IsNil() {
			items := reflect.MakeSlice(field.Type(), field.Len(), field.Len())
			reflect.Copy(items, field)
			field.Set(items)
			for j := 0; j < field.Len(); j++ {
				cloneSlot(field.Index(j))
			}
		} else {
			cloneSlot(field)
		}
	}
	return copy
}
