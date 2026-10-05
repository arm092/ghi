package compiler

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/scanner"
	"go/token"
	"strconv"
	"strings"
)

func parseFile(fset *token.FileSet, filename string, data []byte, coverage ...bool) (string, *ast.File, *unit, error) {
	// The declaration grammar starts with a namespace rather than a Go package.
	// Go's scanner preserves comments, literals and automatic semicolon rules.
	data = []byte(strings.TrimPrefix(string(data), "\ufeff"))
	originalSource := append([]byte(nil), data...)
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
	data, enums, enumErr := extractEnums(filename, data)
	if enumErr != nil {
		return "", nil, nil, enumErr
	}
	transformed := string(data[:start]) + "package " + parts[len(parts)-1] + string(data[end:])
	importSource, typeImports, err := extractTypeImports(filename, []byte(transformed))
	if err != nil {
		return "", nil, nil, err
	}
	normalized, unit, err := extractExtensions(fset, filename, importSource, coverage...)
	if err != nil {
		return "", nil, nil, err
	}
	unit.TypeImports = typeImports
	unit.Enums = enums
	normalized, err = normalizeExceptions(filename, normalized)
	if err != nil {
		return "", nil, nil, err
	}
	tree, err := parser.ParseFile(fset, filename, normalized, parser.ParseComments|parser.AllErrors)
	if err != nil {
		return "", nil, nil, err
	}
	for _, spec := range tree.Imports {
		path, _ := strconv.Unquote(spec.Path.Value)
		if !strings.HasPrefix(path, "go:") {
			return "", nil, nil, fmt.Errorf("%s: quoted imports are only for Go packages; use import %s with an optional as alias for Ghi", fset.Position(spec.Pos()), path)
		}
	}
	for _, decl := range tree.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok {
			if meta := unit.Functions[fn.Name.Name]; meta != nil {
				meta.Node = fn
			}
		}
	}
	if err := appendEnumDeclarations(fset, filename, tree, enums); err != nil {
		return "", nil, nil, err
	}
	recordNullableTypes(fset, filename, originalSource, tree, unit)
	return name, tree, unit, nil
}

// Preserve nullable spelling independently of the backend's pointer type.
// A native *T and a nullable ?T must retain different equality semantics.
func recordNullableTypes(fset *token.FileSet, filename string, source []byte, tree *ast.File, unit *unit) {
	unit.NullableTypes = map[*ast.StarExpr]bool{}
	unit.NativePointers = map[*ast.StarExpr]bool{}
	if !bytes.ContainsRune(source, '?') && !bytes.ContainsRune(source, '*') {
		return
	}
	file := &sourceFile{Path: filename, Source: source, Tree: tree, Unit: unit}
	program := &program{Fset: fset}
	// Include method signatures as well as bodies in the existing source-origin
	// resolver; parameter fragments use their separately retained byte origins.
	copyTree := *tree
	copyTree.Decls = append([]ast.Decl(nil), tree.Decls...)
	for _, class := range unit.Classes {
		functions := append([]*functionDecl(nil), class.Methods...)
		if class.Constructor != nil {
			functions = append(functions, class.Constructor)
		}
		for _, function := range functions {
			if function.Node != nil {
				copyTree.Decls = append(copyTree.Decls, function.Node)
			}
		}
	}
	file.Tree = &copyTree
	for expression, boundary := range program.expressionCandidates(file) {
		if star, ok := expression.(*ast.StarExpr); ok {
			if source[boundary[0]] == '?' {
				unit.NullableTypes[star] = true
			} else if source[boundary[0]] == '*' {
				unit.NativePointers[star] = true
			}
		}
	}
	lines := []int{0}
	for i, b := range source {
		if b == '\n' {
			lines = append(lines, i+1)
		}
	}
	for _, class := range unit.Classes {
		origins := map[*ast.StarExpr]token.Position{}
		for _, field := range class.Fields {
			for star, position := range field.TypeOrigins {
				origins[star] = position
			}
		}
		functions := append([]*functionDecl(nil), class.Methods...)
		if class.Constructor != nil {
			functions = append(functions, class.Constructor)
		}
		for _, function := range functions {
			for star, position := range function.ParameterOrigins {
				origins[star] = position
			}
		}
		for star, position := range origins {
			if position.Line < 1 || position.Line > len(lines) {
				continue
			}
			offset := lines[position.Line-1] + position.Column - 1
			if mapping := unit.CoverageSource; mapping != nil && mapping.offsets != nil {
				if position.Line > len(mapping.lines) {
					continue
				}
				mapped := mapping.expressionBoundary(mapping.lines[position.Line-1] + position.Column - 1)
				if mapped < 0 {
					continue
				}
				line := 1 + strings.Count(string(mapping.original[:mapped]), "\n")
				column := mapped - strings.LastIndex(string(mapping.original[:mapped]), "\n")
				if line > len(lines) {
					continue
				}
				offset = lines[line-1] + column - 1
			}
			if offset >= 0 && offset < len(source) {
				if source[offset] == '?' {
					unit.NullableTypes[star] = true
				} else if source[offset] == '*' {
					unit.NativePointers[star] = true
				}
			}
		}
	}
}
