package compiler

import (
	"go/ast"
	"go/parser"
	"strings"
)

func splitTypeNames(text string) []string {
	var result []string
	depth, start := 0, 0
	for i, r := range text {
		switch r {
		case '[':
			depth++
		case ']':
			depth--
		case ',':
			if depth == 0 {
				result = append(result, strings.TrimSpace(text[start:i]))
				start = i + 1
			}
		}
	}
	return append(result, strings.TrimSpace(text[start:]))
}

func genericBase(expr ast.Expr) (ast.Expr, []ast.Expr) {
	switch x := expr.(type) {
	case *ast.IndexExpr:
		return x.X, []ast.Expr{x.Index}
	case *ast.IndexListExpr:
		return x.X, x.Indices
	}
	return expr, nil
}
func genericName(text string) string {
	expr, err := parser.ParseExpr(text)
	if err != nil {
		return text
	}
	base, _ := genericBase(expr)
	return expressionText(base)
}
func typeArgumentsText(args []ast.Expr) string {
	if len(args) == 0 {
		return ""
	}
	var names []string
	for _, arg := range args {
		names = append(names, expressionText(arg))
	}
	return "[" + strings.Join(names, ", ") + "]"
}
func (c *classDecl) typeArguments() string {
	if c.TypeParams == nil {
		return ""
	}
	var names []string
	for _, field := range c.TypeParams.List {
		for _, name := range field.Names {
			names = append(names, name.Name)
		}
	}
	return "[" + strings.Join(names, ", ") + "]"
}
func (c *classDecl) markerParameters() string {
	if c.TypeParams == nil {
		return ""
	}
	var types []string
	for _, field := range c.TypeParams.List {
		for _, name := range field.Names {
			types = append(types, name.Name)
		}
	}
	return strings.Join(types, ", ")
}
func (c *classDecl) typeParameters() string {
	if c.TypeParams == nil {
		return ""
	}
	var declarations []string
	for _, field := range c.TypeParams.List {
		var names []string
		for _, name := range field.Names {
			names = append(names, name.Name)
		}
		declarations = append(declarations, strings.Join(names, ", ")+" "+expressionText(field.Type))
	}
	return "[" + strings.Join(declarations, ", ") + "]"
}

func (c *classDecl) mentionsTypeParameter(expr ast.Expr) bool {
	parameters := map[string]bool{}
	if c.TypeParams != nil {
		for _, field := range c.TypeParams.List {
			for _, name := range field.Names {
				parameters[name.Name] = true
			}
		}
	}
	var required func(ast.Expr) bool
	required = func(expr ast.Expr) bool {
		switch x := expr.(type) {
		case *ast.Ident:
			return parameters[x.Name]
		case *ast.ParenExpr:
			return required(x.X)
		case *ast.ArrayType:
			return x.Len != nil && required(x.Elt)
		case *ast.StructType:
			for _, field := range x.Fields.List {
				if required(field.Type) {
					return true
				}
			}
		}
		return false
	}
	return required(expr)
}
