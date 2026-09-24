package compiler

import (
	"fmt"
	"go/ast"
	"go/constant"
	"go/parser"
	"go/token"
	"go/types"
	"strconv"
	"strings"
)

type enumCase struct {
	Name, Value string
	Line        int
}
type enumDecl struct {
	Name, Backing string
	Line          int
	Cases         []enumCase
}

func extractEnums(filename string, source []byte) ([]byte, []*enumDecl, error) {
	ts, err := lexSource(filename, source)
	if err != nil {
		return nil, nil, err
	}
	data := append([]byte(nil), source...)
	var enums []*enumDecl
	depth := 0
	for i := 0; i < len(ts); i++ {
		t := ts[i]
		if t.Kind == token.IDENT && (strings.HasPrefix(t.Text, "GhiEnum_") || strings.HasPrefix(t.Text, "ghi_")) {
			return nil, nil, fmt.Errorf("%s:%d: identifier %s is reserved for the compiler", filename, t.Line, t.Text)
		}
		if depth == 0 && t.Kind == token.IDENT && t.Text == "enum" {
			at := t
			fail := func(message string) ([]byte, []*enumDecl, error) {
				return nil, nil, fmt.Errorf("%s:%d: enum %s", filename, at.Line, message)
			}
			if i+2 >= len(ts) || ts[i+1].Kind != token.IDENT || ts[i+1].Text == "_" {
				return fail("requires a name")
			}
			for j := i + 1; j < len(ts) && ts[j].Kind != token.RBRACE; j++ {
				if ts[j].Kind == token.IDENT {
					for _, prefix := range []string{"GhiEnum_", "GhiGet_", "GhiSet_", "GhiRef_", "GhiM_", "GhiBody_", "GhiNew_", "GhiInit_", "GhiIs_", "GhiTry", "GhiThrow", "ghiData_", "ghi_"} {
						if strings.HasPrefix(ts[j].Text, prefix) {
							return fail("uses reserved identifier " + ts[j].Text)
						}
					}
				}
			}
			e := &enumDecl{Name: ts[i+1].Text, Line: t.Line}
			j := i + 2
			if ts[j].Kind == token.IDENT {
				if ts[j].Text != "string" && ts[j].Text != "int" && ts[j].Text != "bool" {
					return fail("backing type must be string, int or bool")
				}
				e.Backing = ts[j].Text
				j++
			}
			if j >= len(ts) || ts[j].Kind != token.LBRACE {
				return fail("requires a brace body")
			}
			end, err := match(filename, ts, j, token.LBRACE, token.RBRACE)
			if err != nil {
				return nil, nil, err
			}
			names, values := map[string]bool{}, map[string]bool{}
			for j++; j < end; {
				if ts[j].Kind == token.SEMICOLON {
					j++
					continue
				}
				at = ts[j]
				if ts[j].Kind != token.IDENT || ts[j].Text == "_" {
					return fail("case requires a name")
				}
				c := enumCase{Name: ts[j].Text, Value: strconv.Itoa(len(e.Cases)), Line: ts[j].Line}
				j++
				if e.Backing == "" {
					if j < end && ts[j].Kind == token.ASSIGN {
						return fail("assigned values require an explicit backing type")
					}
				} else {
					if j >= end || ts[j].Kind != token.ASSIGN {
						return fail("backed cases require explicit values")
					}
					j++
					if j >= end {
						return fail("case requires a value")
					}
					if e.Backing == "string" {
						if ts[j].Kind != token.STRING {
							return fail("case value must be a string literal")
						}
						value, err := strconv.Unquote(ts[j].Text)
						if err != nil {
							return fail("invalid string literal")
						}
						c.Value = strconv.Quote(value)
						j++
					} else if e.Backing == "bool" {
						if ts[j].Text != "true" && ts[j].Text != "false" {
							return fail("case value must be true or false")
						}
						c.Value = ts[j].Text
						j++
					} else {
						sign := ""
						if ts[j].Kind == token.SUB || ts[j].Kind == token.ADD {
							sign = ts[j].Kind.String()
							j++
						}
						if j >= end || ts[j].Kind != token.INT {
							return fail("case value must be an integer literal")
						}
						value := constant.MakeFromLiteral(ts[j].Text, token.INT, 0)
						if sign == "-" {
							value = constant.UnaryOp(token.SUB, value, 0)
						}
						if value.Kind() != constant.Int {
							return fail("invalid integer literal")
						}
						c.Value = value.ExactString()
						j++
					}
				}
				if names[c.Name] {
					return fail("duplicate case " + c.Name)
				}
				if values[c.Value] {
					return fail("duplicate value " + strconv.Quote(c.Value))
				}
				names[c.Name], values[c.Value] = true, true
				e.Cases = append(e.Cases, c)
				if j < end {
					if ts[j].Kind != token.COMMA && ts[j].Kind != token.SEMICOLON {
						return fail("cases must be separated by a comma or newline")
					}
					j++
				}
			}
			if len(e.Cases) == 0 {
				return fail("requires at least one case")
			}
			enums = append(enums, e)
			erase(data, t.Start, ts[end].End)
			i = end
			continue
		}
		if t.Kind == token.LBRACE {
			depth++
		}
		if t.Kind == token.RBRACE {
			depth--
		}
	}
	return data, enums, nil
}

func enumSymbol(name, member string) string {
	return "GhiEnum_" + strconv.Itoa(len(name)) + "_" + name + "_" + member
}

func appendEnumDeclarations(fset *token.FileSet, filename string, tree *ast.File, enums []*enumDecl) error {
	for _, e := range enums {
		var b strings.Builder
		if e.Backing == "" {
			fmt.Fprintf(&b, "package parsed\n//line %s:%d\ntype %s struct { ghi_enum_tag uint32 }\n", filename, e.Line, e.Name)
			for _, c := range e.Cases {
				fmt.Fprintf(&b, "//line %s:%d\nfunc %s() %s { return %s{ghi_enum_tag: %s} }\n", filename, c.Line, enumSymbol(e.Name, c.Name), e.Name, e.Name, c.Value)
			}
		} else {
			fmt.Fprintf(&b, "package parsed\n//line %s:%d\ntype %s = %s\n", filename, e.Line, e.Name, e.Backing)
			for _, c := range e.Cases {
				fmt.Fprintf(&b, "//line %s:%d\nconst %s %s = %s\n", filename, c.Line, enumSymbol(e.Name, c.Name), e.Backing, c.Value)
			}
		}
		generated, err := parser.ParseFile(fset, filename, b.String(), parser.AllErrors)
		if err != nil {
			return err
		}
		tree.Decls = append(tree.Decls, generated.Decls...)
	}
	return nil
}

// Resolve only enum declaration receivers, leaving shadowed locals and ordinary
// struct selectors intact. Imported enums have already received a scoped alias.
func (p *program) bindEnums() error {
	all := map[*namespace]map[string]*enumDecl{}
	for _, ns := range p.Ordered {
		all[ns] = map[string]*enumDecl{}
		for _, f := range ns.Files {
			for _, e := range f.Unit.Enums {
				if all[ns][e.Name] != nil {
					return fmt.Errorf("%s:%d: duplicate enum %s", f.Path, e.Line, e.Name)
				}
				all[ns][e.Name] = e
			}
		}
	}
	for _, ns := range p.Ordered {
		for _, f := range ns.Files {
			imports := map[string]*namespace{}
			for _, s := range f.Tree.Imports {
				path, _ := strconv.Unquote(s.Path.Value)
				target := p.Namespaces[path]
				if target == nil {
					continue
				}
				alias := target.GoName
				if s.Name != nil {
					alias = s.Name.Name
				}
				imports[alias] = target
			}
			var failure error
			rewrite := func(root ast.Node, shadow map[string]bool) ast.Node {
				return walkNode(root, func(node ast.Node) ast.Node {
					sel, ok := node.(*ast.SelectorExpr)
					if !ok {
						return node
					}
					var e *enumDecl
					var qualifier *ast.Ident
					switch x := sel.X.(type) {
					case *ast.Ident:
						if !shadow[x.Name] && x.Obj == nil {
							e = all[ns][x.Name]
						}
					case *ast.SelectorExpr:
						if id, ok := x.X.(*ast.Ident); ok && id.Obj == nil && !shadow[id.Name] {
							e = all[imports[id.Name]][x.Sel.Name]
							qualifier = id
						}
					}
					if e == nil {
						return node
					}
					found := false
					for _, c := range e.Cases {
						if c.Name == sel.Sel.Name {
							found = true
							break
						}
					}
					if !found {
						if failure == nil {
							failure = fmt.Errorf("%s: enum %s has no case %s", p.Fset.Position(sel.Sel.Pos()), e.Name, sel.Sel.Name)
						}
						return node
					}
					id := &ast.Ident{NamePos: sel.Sel.NamePos, Name: enumSymbol(e.Name, sel.Sel.Name)}
					var value ast.Expr = id
					if qualifier != nil {
						value = &ast.SelectorExpr{X: qualifier, Sel: id}
					}
					if e.Backing == "" {
						value = &ast.CallExpr{Fun: value, Lparen: sel.Sel.NamePos, Rparen: sel.Sel.End()}
					}
					return value
				}, true)
			}
			rewrite(f.Tree, nil)
			for _, fn := range f.Unit.Functions {
				for i, v := range fn.Defaults {
					fn.Defaults[i] = rewrite(v, nil).(ast.Expr)
				}
			}
			for _, c := range f.Unit.Classes {
				shadow := map[string]bool{}
				for _, name := range classParameterNames(c) {
					shadow[name] = true
				}
				methods := append([]*functionDecl(nil), c.Methods...)
				if c.Constructor != nil {
					methods = append(methods, c.Constructor)
				}
				for _, m := range methods {
					rewrite(m.Node, shadow)
					for i, v := range m.Defaults {
						m.Defaults[i] = rewrite(v, shadow).(ast.Expr)
					}
				}
			}
			if failure != nil {
				return failure
			}
		}
	}
	return nil
}

func (p *program) plainEnumType(typ types.Type) bool {
	if typ == nil {
		return false
	}
	named, ok := types.Unalias(typ).(*types.Named)
	if !ok || named.Obj().Pkg() == nil {
		return false
	}
	for _, ns := range p.Ordered {
		if namespacePath(ns) != named.Obj().Pkg().Path() {
			continue
		}
		for _, f := range ns.Files {
			for _, e := range f.Unit.Enums {
				if e.Backing == "" && e.Name == named.Obj().Name() {
					return true
				}
			}
		}
	}
	return false
}

// Plain enum values come from cases (or the zero value, its first case), never
// from user-specified representation fields or conversions from another type.
func (p *program) validateEnumValues(info *types.Info) error {
	var failure error
	for _, ns := range p.Ordered {
		for _, f := range ns.Files {
			ast.Inspect(f.Tree, func(node ast.Node) bool {
				if fn, ok := node.(*ast.FuncDecl); ok && strings.HasPrefix(fn.Name.Name, "GhiEnum_") {
					return false
				}
				if failure != nil {
					return false
				}
				var typ types.Type
				switch n := node.(type) {
				case *ast.CompositeLit:
					typ = info.TypeOf(n)
				case *ast.CallExpr:
					if tv, ok := info.Types[n.Fun]; ok && tv.IsType() {
						typ = tv.Type
					}
				}
				if p.plainEnumType(typ) {
					failure = fmt.Errorf("%s: use a declared enum case instead of constructing or converting an enum", p.Fset.Position(node.Pos()))
					return false
				}
				return true
			})
		}
	}
	return failure
}
