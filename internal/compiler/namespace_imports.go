package compiler

import (
	"fmt"
	"go/ast"
	"go/token"
	"strconv"
	"strings"
)

// Resolve dotted imports against loaded declarations, never capitalization.
// Remaining selected type imports are processed by the existing type binder.
func (p *program) bindNamespaceImports(file *sourceFile, current *namespace) error {
	if len(file.Unit.TypeImports) == 0 {
		return nil
	}
	occupied := namespaceDeclarations(current)
	for _, spec := range file.Tree.Imports {
		path, _ := strconv.Unquote(spec.Path.Value)
		name := path[strings.LastIndex(path, "/")+1:]
		if ns := p.Namespaces[path]; ns != nil {
			name = ns.GoName
		}
		if spec.Name != nil {
			name = spec.Name.Name
		}
		occupied[name] = true
	}
	var selected []selectedTypeImport
	for _, item := range file.Unit.TypeImports {
		path := item.Name
		if item.Namespace != "" {
			path = item.Namespace + "." + item.Name
		}
		target := p.Namespaces[path]
		parent := p.Namespaces[item.Namespace]
		isType := parent != nil && namespaceHasType(parent, item.Name)
		fail := func(message string) error { return fmt.Errorf("%s:%d: %s", file.Path, item.Line, message) }
		if target != nil && isType {
			return fail("ambiguous import " + path + ": both a namespace and a type exist")
		}
		if target == nil {
			if item.Namespace == "" {
				return fail("unknown namespace " + path)
			}
			selected = append(selected, item)
			continue
		}
		if target == current {
			return fail("cannot import its own namespace " + path)
		}
		if occupied[item.Alias] {
			return fail("import name collision: " + item.Alias)
		}
		occupied[item.Alias] = true
		position := p.Fset.File(file.Tree.Package).Pos(item.Start)
		spec := &ast.ImportSpec{Name: &ast.Ident{NamePos: position, Name: item.Alias}, Path: &ast.BasicLit{ValuePos: position, Kind: token.STRING, Value: strconv.Quote(path)}}
		file.Tree.Imports = append(file.Tree.Imports, spec)
		file.Tree.Decls = append([]ast.Decl{&ast.GenDecl{TokPos: position, Tok: token.IMPORT, Specs: []ast.Spec{spec}}}, file.Tree.Decls...)
	}
	file.Unit.TypeImports = selected
	return nil
}
