package compiler

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/scanner"
	"go/token"
	"strings"
)

func parseFile(fset *token.FileSet, filename string, data []byte) (string, *ast.File, *unit, error) {
	// The declaration grammar starts with a namespace rather than a Go package.
	// Go's scanner preserves comments, literals and automatic semicolon rules.
	data = []byte(strings.TrimPrefix(string(data), "\ufeff"))
	var nullableErr error
	data, nullableErr = normalizeNullable(filename, data)
	if nullableErr != nil {
		return "", nil, nil, nullableErr
	}
	scanSet := token.NewFileSet()
	file := scanSet.AddFile(filename, -1, len(data))
	var s scanner.Scanner
	var scanErr error
	s.Init(file, data, func(pos token.Position, msg string) {
		if scanErr == nil {
			scanErr = fmt.Errorf("%s: %s", pos, msg)
		}
	}, 0)
	pos, tok, lit := s.Scan()
	if tok != token.IDENT || lit != "namespace" {
		return "", nil, nil, fmt.Errorf("%s: expected namespace declaration", scanSet.Position(pos))
	}
	start := file.Offset(pos)
	var parts []string
	for {
		pos, tok, lit = s.Scan()
		if tok != token.IDENT {
			return "", nil, nil, fmt.Errorf("%s: expected namespace identifier", scanSet.Position(pos))
		}
		parts = append(parts, lit)
		pos, tok, _ = s.Scan()
		if tok != token.PERIOD {
			break
		}
	}
	if scanErr != nil {
		return "", nil, nil, scanErr
	}
	if tok != token.SEMICOLON && tok != token.EOF {
		return "", nil, nil, fmt.Errorf("%s: expected end of namespace declaration", scanSet.Position(pos))
	}
	name := strings.Join(parts, ".")
	end := file.Offset(pos)
	transformed := string(data[:start]) + "package " + parts[len(parts)-1] + string(data[end:])
	importSource, typeImports, err := extractTypeImports(filename, []byte(transformed))
	if err != nil {
		return "", nil, nil, err
	}
	normalized, unit, err := extractExtensions(fset, filename, importSource)
	if err != nil {
		return "", nil, nil, err
	}
	unit.TypeImports = typeImports
	normalized, err = normalizeExceptions(filename, normalized)
	if err != nil {
		return "", nil, nil, err
	}
	tree, err := parser.ParseFile(fset, filename, normalized, parser.ParseComments|parser.AllErrors)
	if err != nil {
		return "", nil, nil, err
	}
	for _, decl := range tree.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok {
			if meta := unit.Functions[fn.Name.Name]; meta != nil {
				meta.Node = fn
			}
		}
	}
	return name, tree, unit, nil
}
