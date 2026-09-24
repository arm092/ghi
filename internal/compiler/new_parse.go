package compiler

import (
	"fmt"
	"go/ast"
	"go/token"
)

// normalizeNew uses a same-width selector marker, preserving source columns,
// member access precedence and generic arguments. Comments in the keyword gap
// are erased only in the parser input; the formatter retains original source.
func normalizeNew(filename string, source []byte, tokens []lexeme) ([]byte, error) {
	data := append([]byte(nil), source...)
	for i, t := range tokens {
		if t.Kind != token.IDENT || t.Text != "new" {
			continue
		}
		// Retain Go's allocation builtin for native types. Existing nonnullable
		// allocation validation rejects new(Class) without running a constructor.
		if i+1 < len(tokens) && tokens[i+1].Kind == token.LPAREN {
			continue
		}
		j := i + 1
		if j >= len(tokens) || tokens[j].Kind != token.IDENT {
			return nil, fmt.Errorf("%s:%d: expected class name after new", filename, t.Line)
		}
		j++
		for j+1 < len(tokens) && tokens[j].Kind == token.PERIOD && tokens[j+1].Kind == token.IDENT {
			j += 2
		}
		if j < len(tokens) && tokens[j].Kind == token.LBRACK {
			end, err := match(tokens, j, token.LBRACK, token.RBRACK)
			if err != nil {
				return nil, err
			}
			j = end + 1
		}
		if j >= len(tokens) || tokens[j].Kind != token.LPAREN {
			return nil, fmt.Errorf("%s:%d: expected constructor arguments after new", filename, t.Line)
		}
		erase(data, t.End, tokens[i+1].Start)
		data[t.End] = '.'
	}
	return data, nil
}

func unwrapConstruction(expression ast.Expr) (ast.Expr, bool) {
	switch value := expression.(type) {
	case *ast.SelectorExpr:
		if id, ok := value.X.(*ast.Ident); ok && id.Name == "new" {
			return value.Sel, true
		}
		target, ok := unwrapConstruction(value.X)
		if ok {
			value.X = target
		}
		return value, ok
	case *ast.IndexExpr:
		target, ok := unwrapConstruction(value.X)
		if ok {
			value.X = target
		}
		return value, ok
	case *ast.IndexListExpr:
		target, ok := unwrapConstruction(value.X)
		if ok {
			value.X = target
		}
		return value, ok
	}
	return expression, false
}
