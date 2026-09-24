package compiler

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"strings"
)

type selectedTypeImport struct {
	Namespace, Name, Alias string
	Start, Line            int
}

// Erase only the declaration, retaining byte offsets and newlines. Selected
// imports are bound after all project and Mojave namespaces have been loaded.
func extractTypeImports(filename string, source []byte) ([]byte, []selectedTypeImport, error) {
	tokens, err := lexSource(filename, source)
	if err != nil {
		return nil, nil, err
	}
	data := append([]byte(nil), source...)
	var imports []selectedTypeImport
	depth := 0
	for i := 0; i < len(tokens); i++ {
		t := tokens[i]
		if t.Kind == token.LBRACE {
			depth++
		}
		if t.Kind == token.RBRACE {
			depth--
		}
		if depth != 0 || t.Kind != token.IMPORT || i+1 >= len(tokens) || tokens[i+1].Kind != token.IDENT {
			continue
		}
		if i+2 < len(tokens) && tokens[i+2].Kind == token.STRING {
			continue
		}
		j := i + 1
		var parts []string
		for {
			if j >= len(tokens) || tokens[j].Kind != token.IDENT {
				return nil, nil, fmt.Errorf("%s:%d: expected namespace.Type import", filename, t.Line)
			}
			parts = append(parts, tokens[j].Text)
			j++
			if j >= len(tokens) || tokens[j].Kind != token.PERIOD {
				break
			}
			j++
		}
		alias := parts[len(parts)-1]
		if j < len(tokens) && tokens[j].Text == "as" {
			j++
			if j >= len(tokens) || tokens[j].Kind != token.IDENT {
				return nil, nil, fmt.Errorf("%s:%d: expected type import alias after as", filename, t.Line)
			}
			alias = tokens[j].Text
			j++
		}
		if len(parts) < 2 || j >= len(tokens) || (tokens[j].Kind != token.SEMICOLON && tokens[j].Kind != token.EOF) {
			return nil, nil, fmt.Errorf("%s:%d: expected namespace.Type import", filename, t.Line)
		}
		if alias == "_" || strings.Contains(" namespace class constructor extends implements override public private protected this parent new try catch finally throw as ", " "+alias+" ") {
			return nil, nil, fmt.Errorf("%s:%d: invalid type import alias %s", filename, t.Line, alias)
		}
		imports = append(imports, selectedTypeImport{strings.Join(parts[:len(parts)-1], "."), parts[len(parts)-1], alias, t.Start, t.Line})
		erase(data, t.Start, tokens[j].Start)
		i = j - 1
	}
	return data, imports, nil
}

func namespaceDeclarations(ns *namespace) map[string]bool {
	names := map[string]bool{}
	for _, file := range ns.Files {
		for _, class := range file.Unit.Classes {
			names[class.Name] = true
		}
		for _, decl := range file.Tree.Decls {
			switch decl := decl.(type) {
			case *ast.FuncDecl:
				if decl.Recv == nil {
					names[decl.Name.Name] = true
				}
			case *ast.GenDecl:
				for _, spec := range decl.Specs {
					switch spec := spec.(type) {
					case *ast.TypeSpec:
						names[spec.Name.Name] = true
					case *ast.ValueSpec:
						for _, name := range spec.Names {
							names[name.Name] = true
						}
					}
				}
			}
		}
	}
	return names
}

func namespaceHasType(ns *namespace, name string) bool {
	for _, file := range ns.Files {
		for _, class := range file.Unit.Classes {
			if class.Name == name {
				return true
			}
		}
		for _, decl := range file.Tree.Decls {
			if group, ok := decl.(*ast.GenDecl); ok && group.Tok == token.TYPE {
				for _, spec := range group.Specs {
					if spec.(*ast.TypeSpec).Name.Name == name {
						return true
					}
				}
			}
		}
	}
	return false
}

func (p *program) bindTypeImports() error {
	for _, ns := range p.Ordered {
		declared := namespaceDeclarations(ns)
		for _, file := range ns.Files {
			if len(file.Unit.TypeImports) == 0 {
				continue
			}
			bindings := map[string]string{}
			aliases := map[string]string{}
			occupied := map[string]bool{}
			for _, spec := range file.Tree.Imports {
				path, _ := strconv.Unquote(spec.Path.Value)
				name := path[strings.LastIndex(path, "/")+1:]
				if target := p.Namespaces[path]; target != nil {
					name = target.GoName
				}
				if spec.Name != nil {
					name = spec.Name.Name
				}
				occupied[name] = true
			}
			var added []*ast.ImportSpec
			for _, selected := range file.Unit.TypeImports {
				fail := func(message string) error { return fmt.Errorf("%s:%d: %s", file.Path, selected.Line, message) }
				if declared[selected.Alias] || occupied[selected.Alias] || bindings[selected.Alias] != "" {
					return fail("type import name collision: " + selected.Alias)
				}
				target := p.Namespaces[selected.Namespace]
				if target == nil {
					return fail("unknown namespace " + selected.Namespace)
				}
				if target == ns {
					return fail("cannot import a type from its own namespace")
				}
				if !namespaceHasType(target, selected.Name) {
					return fail("unknown type " + selected.Namespace + "." + selected.Name)
				}
				alias := aliases[selected.Namespace]
				if alias == "" {
					alias = fmt.Sprintf("ghi_type_import_%d", len(added))
					aliases[selected.Namespace] = alias
					position := p.Fset.File(file.Tree.Package).Pos(selected.Start)
					added = append(added, &ast.ImportSpec{Name: &ast.Ident{NamePos: position, Name: alias}, Path: &ast.BasicLit{ValuePos: position, Kind: token.STRING, Value: strconv.Quote(selected.Namespace)}})
				}
				bindings[selected.Alias] = alias + "." + selected.Name
			}
			used := map[string]bool{}
			rewrite := func(root ast.Node, shadowed map[string]bool) ast.Node {
				return qualifySelectedTypes(root, bindings, shadowed, used)
			}
			rewrite(file.Tree, nil)
			for _, fn := range file.Unit.Functions {
				for index, value := range fn.Defaults {
					fn.Defaults[index] = rewrite(value, nil).(ast.Expr)
				}
			}
			for _, class := range file.Unit.Classes {
				shadowed := map[string]bool{}
				if class.TypeParams != nil {
					for _, field := range class.TypeParams.List {
						for _, name := range field.Names {
							shadowed[name.Name] = true
						}
					}
				}
				rewrite(class.TypeParams, shadowed)
				qualifyName := func(name string) string {
					if name == "" {
						return name
					}
					expr, err := parser.ParseExpr(name)
					if err != nil {
						return name
					}
					return expressionText(rewrite(expr, shadowed).(ast.Expr))
				}
				class.ParentName = qualifyName(class.ParentName)
				for i, name := range class.InterfaceNames {
					class.InterfaceNames[i] = qualifyName(name)
				}
				for _, field := range class.Fields {
					field.Type = rewrite(field.Type, shadowed).(ast.Expr)
				}
				methods := append([]*functionDecl(nil), class.Methods...)
				if class.Constructor != nil {
					methods = append(methods, class.Constructor)
				}
				for _, method := range methods {
					rewrite(method.Node, shadowed)
					for i, value := range method.Defaults {
						method.Defaults[i] = rewrite(value, shadowed).(ast.Expr)
					}
				}
			}
			for _, selected := range file.Unit.TypeImports {
				if !used[selected.Alias] {
					return fmt.Errorf("%s:%d: imported type %s is not used", file.Path, selected.Line, selected.Alias)
				}
			}
			// Generated class helpers must choose the same unshadowable alias.
			file.Tree.Imports = append(added, file.Tree.Imports...)
			for _, spec := range added {
				file.Tree.Decls = append([]ast.Decl{&ast.GenDecl{TokPos: spec.Pos(), Tok: token.IMPORT, Specs: []ast.Spec{spec}}}, file.Tree.Decls...)
			}
		}
	}
	return nil
}

// Parser resolution objects distinguish local variables/type parameters from
// free imported names. Member names and declaration names are never bindings.
func qualifySelectedTypes(root ast.Node, bindings map[string]string, shadowed, used map[string]bool) ast.Node {
	if root == nil {
		return nil
	}
	if fields, ok := root.(*ast.FieldList); ok && fields == nil {
		return root
	}
	ignored := map[*ast.Ident]bool{}
	ast.Inspect(root, func(node ast.Node) bool {
		switch node := node.(type) {
		case *ast.SelectorExpr:
			ignored[node.Sel] = true
		case *ast.Field:
			for _, name := range node.Names {
				ignored[name] = true
			}
		case *ast.KeyValueExpr:
			if key, ok := node.Key.(*ast.Ident); ok {
				ignored[key] = true
			}
		case *ast.LabeledStmt:
			ignored[node.Label] = true
		case *ast.BranchStmt:
			ignored[node.Label] = true
		case *ast.FuncDecl:
			ignored[node.Name] = true
		case *ast.TypeSpec:
			ignored[node.Name] = true
		case *ast.ValueSpec:
			for _, name := range node.Names {
				ignored[name] = true
			}
		case *ast.ImportSpec:
			ignored[node.Name] = true
		case *ast.File:
			ignored[node.Name] = true
		}
		return true
	})
	return walkNode(root, func(node ast.Node) ast.Node {
		// Explicit construction is represented as new.Type until expression lowering.
		if selected, ok := node.(*ast.SelectorExpr); ok {
			if marker, ok := selected.X.(*ast.Ident); ok && marker.Name == "new" && !shadowed[selected.Sel.Name] {
				if qualified := bindings[selected.Sel.Name]; qualified != "" {
					used[selected.Sel.Name] = true
					alias, name, _ := strings.Cut(qualified, ".")
					selected.X = &ast.SelectorExpr{X: marker, Sel: &ast.Ident{NamePos: selected.Sel.NamePos, Name: alias}}
					selected.Sel = &ast.Ident{NamePos: selected.Sel.NamePos, Name: name}
				}
			}
			return node
		}
		id, ok := node.(*ast.Ident)
		if !ok || id.Obj != nil || ignored[id] || shadowed[id.Name] || bindings[id.Name] == "" {
			return node
		}
		used[id.Name] = true
		alias, name, _ := strings.Cut(bindings[id.Name], ".")
		return &ast.SelectorExpr{X: &ast.Ident{NamePos: id.NamePos, Name: alias}, Sel: &ast.Ident{NamePos: id.NamePos, Name: name}}
	}, true)
}
