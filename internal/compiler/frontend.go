package compiler

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/scanner"
	"go/token"
	"strings"
)

type lexeme struct {
	Kind             token.Token
	Text             string
	Start, End, Line int
}
type fieldDecl struct {
	Name, Visibility string
	Type             ast.Expr
	Owner            *classDecl
}
type functionDecl struct {
	Name       string
	Node       *ast.FuncDecl
	Defaults   map[int]ast.Expr
	Visibility string
	Override   bool
	Owner      *classDecl
}
type classDecl struct {
	Name           string
	Line           int
	TypeParams     *ast.FieldList
	ParentName     string
	InterfaceNames []string
	Interface      bool
	Fields         []*fieldDecl
	Methods        []*functionDecl
	Constructor    *functionDecl
	File           *sourceFile
	Namespace      *namespace
	Parent         *classDecl
	Interfaces     []*classDecl
}
type unit struct {
	Native    bool
	Classes   []*classDecl
	Functions map[string]*functionDecl
}

func lexSource(filename string, source []byte) ([]lexeme, error) {
	fset := token.NewFileSet()
	file := fset.AddFile(filename, -1, len(source))
	var s scanner.Scanner
	var first error
	s.Init(file, source, func(pos token.Position, msg string) {
		if first == nil {
			first = fmt.Errorf("%s: %s", pos, msg)
		}
	}, 0)
	var result []lexeme
	for {
		pos, kind, text := s.Scan()
		offset := file.Offset(pos)
		end := offset + len(text)
		if text == "" {
			end = offset + len(kind.String())
		}
		if kind == token.EOF {
			end = offset
		}
		result = append(result, lexeme{kind, text, offset, end, fset.Position(pos).Line})
		if kind == token.EOF {
			break
		}
	}
	return result, first
}

func match(filename string, tokens []lexeme, start int, open, close token.Token) (int, error) {
	depth := 0
	for i := start; i < len(tokens); i++ {
		if tokens[i].Kind == open {
			depth++
		}
		if tokens[i].Kind == close {
			depth--
			if depth == 0 {
				return i, nil
			}
		}
	}
	return 0, fmt.Errorf("%s:%d: unclosed %s", filename, tokens[start].Line, open)
}

func erase(data []byte, start, end int) {
	for i := start; i < end && i < len(data); i++ {
		if data[i] != '\n' && data[i] != '\r' {
			data[i] = ' '
		}
	}
}

func expressionText(expression ast.Expr) string {
	var out bytes.Buffer
	printer.Fprint(&out, token.NewFileSet(), expression)
	return out.String()
}

// normalizeParameters removes default expressions for the underlying Go parser.
// Defaults are retained in the language AST and inserted at call sites later.
func normalizeParameters(text string) (string, map[int]ast.Expr, error) {
	tokens, err := lexSource("parameters", []byte(text))
	if err != nil {
		return "", nil, err
	}
	data := []byte(text)
	defaults := map[int]ast.Expr{}
	index := 0
	depth := 0
	for i := 0; i < len(tokens); i++ {
		t := tokens[i]
		switch t.Kind {
		case token.LPAREN, token.LBRACK, token.LBRACE:
			depth++
		case token.RPAREN, token.RBRACK, token.RBRACE:
			depth--
		case token.COMMA:
			if depth == 0 {
				index++
			}
		case token.ASSIGN:
			if depth != 0 {
				continue
			}
			start := t.Start
			end := len(text)
			j := i + 1
			inner := 0
			for ; j < len(tokens); j++ {
				k := tokens[j].Kind
				if inner == 0 && (k == token.COMMA || k == token.EOF || k == token.SEMICOLON) {
					end = tokens[j].Start
					break
				}
				switch k {
				case token.LPAREN, token.LBRACK, token.LBRACE:
					inner++
				case token.RPAREN, token.RBRACK, token.RBRACE:
					inner--
				}
			}
			value, err := parser.ParseExpr(text[t.End:end])
			if err != nil {
				return "", nil, fmt.Errorf("default argument: %w", err)
			}
			defaults[index] = value
			erase(data, start, end)
			i = j - 1
		}
	}
	return string(data), defaults, nil
}

func parseFunction(fset *token.FileSet, filename, name, parameters, tail string, line int) (*functionDecl, error) {
	params, defaults, err := normalizeParameters(parameters)
	if err != nil {
		return nil, err
	}
	normalized, err := normalizeExceptionsAt(filename, []byte(tail), line+strings.Count(parameters, "\n"))
	if err != nil {
		return nil, err
	}
	text := fmt.Sprintf("package parsed\n//line %s:%d\nfunc %s(%s)%s", filename, line, name, params, normalized)
	tree, err := parser.ParseFile(fset, filename, text, parser.AllErrors)
	if err != nil {
		return nil, err
	}
	fn := tree.Decls[0].(*ast.FuncDecl)
	for i, field := range fn.Type.Params.List {
		if len(field.Names) != 1 {
			return nil, fmt.Errorf("%s:%d: each Ghi parameter requires its own name and type", filename, line)
		}
		if _, ok := defaults[i]; !ok {
			for previous := range defaults {
				if previous < i {
					return nil, fmt.Errorf("%s:%d: required parameter follows a default parameter", filename, line)
				}
			}
		}
	}
	return &functionDecl{Name: name, Node: fn, Defaults: defaults, Visibility: "private"}, nil
}

func extractExtensions(fset *token.FileSet, filename string, source []byte) ([]byte, *unit, error) {
	tokens, err := lexSource(filename, source)
	if err != nil {
		return nil, nil, err
	}
	for _, t := range tokens {
		if t.Kind != token.IDENT {
			continue
		}
		for _, prefix := range []string{"GhiGet_", "GhiSet_", "GhiRef_", "GhiM_", "GhiBody_", "GhiNew_", "GhiInit_", "GhiIs_", "GhiTry", "GhiThrow", "ghiData_", "ghi_"} {
			if strings.HasPrefix(t.Text, prefix) {
				return nil, nil, fmt.Errorf("%s:%d: identifier %s is reserved for the compiler", filename, t.Line, t.Text)
			}
		}
	}
	source, err = normalizeNew(filename, source, tokens)
	if err != nil {
		return nil, nil, err
	}
	tokens, err = lexSource(filename, source)
	if err != nil {
		return nil, nil, err
	}
	data := append([]byte(nil), source...)
	u := &unit{Functions: map[string]*functionDecl{}}
	depth := 0
	for i := 0; i < len(tokens); i++ {
		t := tokens[i]
		if depth == 0 && (t.Text == "class" || (t.Kind == token.INTERFACE && i+1 < len(tokens) && tokens[i+1].Kind == token.IDENT)) {
			class := &classDecl{Interface: t.Kind == token.INTERFACE, Line: t.Line}
			i++
			if tokens[i].Kind != token.IDENT {
				return nil, nil, fmt.Errorf("%s:%d: expected class name", filename, t.Line)
			}
			class.Name = tokens[i].Text
			i++
			if tokens[i].Kind == token.LBRACK {
				close, err := match(filename, tokens, i, token.LBRACK, token.RBRACK)
				if err != nil {
					return nil, nil, err
				}
				declaration := "package parsed\ntype Parameters" + string(source[tokens[i].Start:tokens[close].End]) + " struct{}"
				tree, err := parser.ParseFile(fset, filename, declaration, parser.AllErrors)
				if err != nil {
					return nil, nil, err
				}
				class.TypeParams = tree.Decls[0].(*ast.GenDecl).Specs[0].(*ast.TypeSpec).TypeParams
				i = close + 1
			}
			clauses := map[string]bool{}
			for tokens[i].Kind != token.LBRACE {
				if tokens[i].Kind == token.EOF {
					return nil, nil, fmt.Errorf("%s:%d: expected class body", filename, t.Line)
				}
				keyword := tokens[i].Text
				if keyword != "extends" && keyword != "implements" {
					return nil, nil, fmt.Errorf("%s:%d: unexpected class clause %s", filename, tokens[i].Line, keyword)
				}
				if clauses[keyword] {
					return nil, nil, fmt.Errorf("%s:%d: duplicate %s clause", filename, tokens[i].Line, keyword)
				}
				clauses[keyword] = true
				i++
				start := i
				for tokens[i].Kind != token.LBRACE && tokens[i].Kind != token.EOF && tokens[i].Text != "implements" && tokens[i].Text != "extends" {
					i++
				}
				if tokens[i].Kind == token.EOF {
					return nil, nil, fmt.Errorf("%s:%d: expected class body", filename, t.Line)
				}
				value := strings.TrimSpace(string(source[tokens[start].Start:tokens[i].Start]))
				if value == "" {
					return nil, nil, fmt.Errorf("%s:%d: %s requires a type name", filename, tokens[start].Line, keyword)
				}
				switch keyword {
				case "extends":
					class.ParentName = value
				case "implements":
					for _, name := range splitTypeNames(value) {
						class.InterfaceNames = append(class.InterfaceNames, strings.TrimSpace(name))
					}
				default:
					return nil, nil, fmt.Errorf("%s:%d: unexpected class clause %s", filename, t.Line, keyword)
				}
			}
			close, err := match(filename, tokens, i, token.LBRACE, token.RBRACE)
			if err != nil {
				return nil, nil, err
			}
			if err := parseMembers(fset, filename, source, tokens, i+1, close, class); err != nil {
				return nil, nil, err
			}
			endOffset := tokens[close].End
			if close+1 < len(tokens) && tokens[close+1].Kind == token.SEMICOLON {
				endOffset = tokens[close+1].End
			}
			erase(data, t.Start, endOffset)
			i = close
			u.Classes = append(u.Classes, class)
			continue
		}
		if depth == 0 && t.Kind == token.FUNC && i+2 < len(tokens) && tokens[i+1].Kind == token.IDENT {
			open := i + 2
			if tokens[open].Kind == token.LBRACK {
				close, err := match(filename, tokens, open, token.LBRACK, token.RBRACK)
				if err != nil {
					return nil, nil, err
				}
				open = close + 1
			}
			if tokens[open].Kind != token.LPAREN {
				continue
			}
			close, err := match(filename, tokens, open, token.LPAREN, token.RPAREN)
			if err != nil {
				return nil, nil, err
			}
			text := string(source[tokens[open].End:tokens[close].Start])
			normalized, defaults, err := normalizeParameters(text)
			if err != nil {
				return nil, nil, err
			}
			copy(data[tokens[open].End:tokens[close].Start], normalized)
			u.Functions[tokens[i+1].Text] = &functionDecl{Defaults: defaults}
		}
		switch t.Kind {
		case token.LBRACE:
			depth++
		case token.RBRACE:
			depth--
		}
	}
	return data, u, nil
}

func parseMembers(fset *token.FileSet, filename string, source []byte, tokens []lexeme, start, end int, class *classDecl) error {
	names := map[string]bool{}
	for i := start; i < end; {
		if tokens[i].Kind == token.SEMICOLON {
			i++
			continue
		}
		visibility := "private"
		override := false
		if class.Interface {
			visibility = "public"
		}
		for tokens[i].Text == "public" || tokens[i].Text == "private" || tokens[i].Text == "protected" || tokens[i].Text == "override" {
			if tokens[i].Text == "override" {
				override = true
			} else {
				visibility = tokens[i].Text
			}
			i++
		}
		first := tokens[i]
		if class.Interface && (visibility != "public" || override) {
			return fmt.Errorf("%s:%d: interface methods must be public and cannot override", filename, first.Line)
		}
		if first.Kind == token.FUNC || first.Text == "constructor" {
			name := "constructor"
			if first.Kind == token.FUNC {
				i++
				name = tokens[i].Text
			}
			if names[name] {
				return fmt.Errorf("%s:%d: duplicate member %s; overloading is not supported", filename, first.Line, name)
			}
			names[name] = true
			i++
			if tokens[i].Kind != token.LPAREN {
				return fmt.Errorf("%s:%d: expected parameter list", filename, first.Line)
			}
			close, err := match(filename, tokens, i, token.LPAREN, token.RPAREN)
			if err != nil {
				return err
			}
			parameters := string(source[tokens[i].End:tokens[close].Start])
			i = close + 1
			resultStart := tokens[close].End
			for i < end && tokens[i].Kind != token.LBRACE && tokens[i].Kind != token.SEMICOLON {
				i++
			}
			var tail string
			if class.Interface {
				if i < end && tokens[i].Kind == token.LBRACE {
					return fmt.Errorf("%s:%d: interfaces cannot contain method implementations", filename, first.Line)
				}
				tail = string(source[resultStart:tokens[i].Start]) + " {}"
			} else {
				if tokens[i].Kind != token.LBRACE {
					return fmt.Errorf("%s:%d: expected method body", filename, first.Line)
				}
				bodyEnd, err := match(filename, tokens, i, token.LBRACE, token.RBRACE)
				if err != nil {
					return err
				}
				tail = string(source[resultStart:tokens[bodyEnd].End])
				i = bodyEnd + 1
			}
			fn, err := parseFunction(fset, filename, name, parameters, tail, first.Line)
			if err != nil {
				return err
			}
			fn.Visibility = visibility
			fn.Override = override
			fn.Owner = class
			if name == "constructor" {
				if class.Interface {
					return fmt.Errorf("interface cannot declare a constructor")
				}
				class.Constructor = fn
			} else {
				class.Methods = append(class.Methods, fn)
			}
		} else {
			if class.Interface {
				return fmt.Errorf("%s:%d: interface members must be methods", filename, first.Line)
			}
			if first.Kind != token.IDENT {
				return fmt.Errorf("%s:%d: expected field or method", filename, first.Line)
			}
			name := first.Text
			if names[name] {
				return fmt.Errorf("duplicate member %s", name)
			}
			names[name] = true
			i++
			typeStart := tokens[i].Start
			for i < end && tokens[i].Kind != token.SEMICOLON {
				i++
			}
			typ, err := parser.ParseExpr(string(source[typeStart:tokens[i].Start]))
			if err != nil {
				return err
			}
			class.Fields = append(class.Fields, &fieldDecl{Name: name, Visibility: visibility, Type: typ, Owner: class})
		}
	}
	return nil
}
