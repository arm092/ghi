package compiler

import (
	"go/ast"
	"go/token"
	"reflect"
	"strings"
)

type nodeSource struct {
	position token.Position
	fields   map[string]token.Position
}

// Snapshot before lowering: generated AST fragments frequently have NoPos or
// positions belonging to a different parser. They must not become source lines.
func (p *program) snapshotSources() map[ast.Node]nodeSource {
	result := map[ast.Node]nodeSource{}
	record := func(root ast.Node) {
		ast.Inspect(root, func(n ast.Node) bool {
			if n == nil {
				return true
			}
			value := reflect.ValueOf(n)
			if value.Kind() != reflect.Pointer || value.IsNil() {
				return false
			}
			value = value.Elem()
			if value.Kind() != reflect.Struct {
				return true
			}
			entry := nodeSource{position: p.Fset.Position(n.Pos()), fields: map[string]token.Position{}}
			for i := 0; i < value.NumField(); i++ {
				f := value.Field(i)
				if f.Type() == reflect.TypeOf(token.NoPos) && f.Int() != 0 {
					entry.fields[value.Type().Field(i).Name] = p.Fset.Position(token.Pos(f.Int()))
				}
			}
			result[n] = entry
			return true
		})
	}
	for _, ns := range p.Ordered {
		for _, file := range ns.Files {
			if file.Unit.Native {
				continue
			}
			record(file.Tree)
			for _, c := range file.Unit.Classes {
				for _, method := range c.Methods {
					record(method.Node)
				}
				if c.Constructor != nil {
					record(c.Constructor.Node)
				}
			}
		}
	}
	return result
}

// Go's SourcePos printer uses physical positions, ignoring adjusted //line
// mappings. Rebase source nodes to virtual files containing the original text.
func (p *program) rebaseSources(original map[ast.Node]nodeSource) {
	files := map[string]*token.File{}
	sources := map[string][]byte{}
	for _, ns := range p.Ordered {
		for _, file := range ns.Files {
			sources[file.Path] = file.Source
		}
	}
	for _, entry := range original {
		name := entry.position.Filename
		if files[name] != nil || !strings.HasSuffix(name, ".ghi") {
			continue
		}
		data, ok := sources[name]
		if !ok {
			continue
		}
		file := p.Fset.AddFile(name, -1, len(data))
		file.SetLinesForContent(data)
		files[name] = file
	}
	convert := func(pos token.Position) token.Pos {
		file := files[pos.Filename]
		if file == nil || pos.Line < 1 || pos.Line > file.LineCount() {
			return token.NoPos
		}
		lines := file.Lines()
		offset := lines[pos.Line-1]
		end := file.Size()
		if pos.Line < len(lines) {
			end = lines[pos.Line] - 1
		}
		if pos.Column > 0 {
			offset += pos.Column - 1
		}
		if offset > end {
			offset = end
		}
		return file.Pos(offset)
	}
	anchors := map[ast.Node]token.Position{}
	var anchor func(ast.Node) token.Position
	anchor = func(n ast.Node) token.Position {
		if pos, ok := anchors[n]; ok {
			return pos
		}
		if entry, ok := original[n]; ok && files[entry.position.Filename] != nil {
			anchors[n] = entry.position
			return entry.position
		}
		var found token.Position
		ast.Inspect(n, func(child ast.Node) bool {
			if found.IsValid() {
				return false
			}
			if child == nil || child == n {
				return true
			}
			if entry, ok := original[child]; ok && files[entry.position.Filename] != nil {
				found = entry.position
				return false
			}
			return !found.IsValid()
		})
		anchors[n] = found
		return found
	}
	seen := map[ast.Node]bool{}
	var visit func(ast.Node, token.Position)
	visit = func(n ast.Node, parent token.Position) {
		if n == nil || seen[n] {
			return
		}
		seen[n] = true
		v := reflect.ValueOf(n)
		if v.Kind() != reflect.Pointer || v.IsNil() {
			return
		}
		v = v.Elem()
		if v.Kind() != reflect.Struct {
			return
		}
		pos := anchor(n)
		if !pos.IsValid() {
			pos = parent
		}
		for i := 0; i < v.NumField(); i++ {
			f := v.Field(i)
			if !f.CanSet() {
				continue
			}
			name := v.Type().Field(i).Name
			if f.Type() == reflect.TypeOf(token.NoPos) {
				target := pos
				if entry, ok := original[n]; ok {
					if field, ok := entry.fields[name]; ok {
						target = field
					}
				}
				if f.Int() != 0 || name == "NamePos" || name == "Lbrace" || name == "Rbrace" {
					if mapped := convert(target); mapped.IsValid() {
						f.SetInt(int64(mapped))
					}
				}
				continue
			}
			child := func(field reflect.Value) {
				if !field.CanInterface() {
					return
				}
				if (field.Kind() == reflect.Pointer || field.Kind() == reflect.Interface) && field.IsNil() {
					return
				}
				if node, ok := field.Interface().(ast.Node); ok {
					visit(node, pos)
				}
			}
			if f.Kind() == reflect.Slice {
				for j := 0; j < f.Len(); j++ {
					child(f.Index(j))
				}
			} else {
				child(f)
			}
		}
	}
	for _, ns := range p.Ordered {
		for _, file := range ns.Files {
			if file.Unit.Native {
				continue
			}
			// Normalization directives have served their purpose in the origin snapshot.
			comments := file.Tree.Comments[:0]
			for _, group := range file.Tree.Comments {
				list := group.List[:0]
				for _, c := range group.List {
					if !strings.HasPrefix(c.Text, "//line ") {
						list = append(list, c)
					}
				}
				group.List = list
				if len(list) > 0 {
					comments = append(comments, group)
				}
			}
			file.Tree.Comments = comments
			visit(file.Tree, token.Position{Filename: file.Path, Line: 1, Column: 1})
		}
	}
}
