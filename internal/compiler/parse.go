package compiler

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/scanner"
	"go/token"
	"strings"
)

func parseFile(fset *token.FileSet, filename string, data []byte) (string, *ast.File, error) {
	// The declaration grammar starts with a namespace rather than a Go package.
	// Go's scanner preserves comments, literals and automatic semicolon rules.
	data = []byte(strings.TrimPrefix(string(data), "\ufeff"))
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
		return "", nil, fmt.Errorf("%s: expected namespace declaration", scanSet.Position(pos))
	}
	start := file.Offset(pos)
	var parts []string
	for {
		pos, tok, lit = s.Scan()
		if tok != token.IDENT {
			return "", nil, fmt.Errorf("%s: expected namespace identifier", scanSet.Position(pos))
		}
		parts = append(parts, lit)
		pos, tok, _ = s.Scan()
		if tok != token.PERIOD {
			break
		}
	}
	if scanErr != nil {
		return "", nil, scanErr
	}
	if tok != token.SEMICOLON && tok != token.EOF {
		return "", nil, fmt.Errorf("%s: expected end of namespace declaration", scanSet.Position(pos))
	}
	name := strings.Join(parts, ".")
	end := file.Offset(pos)
	transformed := string(data[:start]) + "package " + parts[len(parts)-1] + string(data[end:])
	tree, err := parser.ParseFile(fset, filename, transformed, parser.ParseComments|parser.AllErrors)
	if err != nil {
		return "", nil, err
	}
	return name, tree, nil
}
