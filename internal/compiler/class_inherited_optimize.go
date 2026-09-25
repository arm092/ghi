package compiler

import (
	"fmt"
	"go/ast"
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
		if c.Interface || c.TypeParams != nil {
			continue
		}
		for _, method := range c.allMethods() {
			// Moving generic bodies or bodies across namespaces requires binding
			// their types and imports separately. Leave those on the shared path.
			if method.Owner.TypeParams != nil || method.Owner.Namespace != c.Namespace ||
				(method.Owner == c && !parents[c]) {
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
			name := fmt.Sprintf("ghi_specialized_%d_%s_%s", len(c.Name), c.Name, method.Name)
			copy.Name = ast.NewIdent(name)
			// The copy stays in the original file so its imports and private
			// namespace declarations keep their original bindings.
			method.Owner.File.Tree.Decls = append(method.Owner.File.Tree.Decls, copy)
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
				id, ok := ptr.X.(*ast.Ident)
				if !ok || id.Name != "ghiData_"+c.Name {
					continue
				}
				ast.Inspect(wrapper.Body, func(node ast.Node) bool {
					if call, ok := node.(*ast.CallExpr); ok {
						call.Fun = ast.NewIdent(name)
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
