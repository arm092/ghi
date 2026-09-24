package compiler

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
)

// normalizeArrows lowers block lambdas to Go function literals before class
// extraction, so methods, closures and exception bodies share the same parser.
// Edits never add or remove newlines, preserving source line diagnostics.
func normalizeArrows(filename string, source []byte) ([]byte, error) {
	tokens, err := lexSource(filename, source)
	if err != nil {
		return nil, err
	}
	starts := map[int]bool{}
	arrows := map[int]bool{}
	for i := 0; i+1 < len(tokens); i++ {
		if tokens[i].Kind != token.ASSIGN || tokens[i+1].Kind != token.GTR || tokens[i].End != tokens[i+1].Start {
			continue
		}
		fail := func(message string) ([]byte, error) {
			return nil, fmt.Errorf("%s:%d: %s", filename, tokens[i].Line, message)
		}
		if i+2 >= len(tokens) || tokens[i+2].Kind != token.LBRACE {
			return fail("arrow function requires a block body")
		}
		start, depth := -1, 0
		for j := i - 1; j >= 0; j-- {
			kind := tokens[j].Kind
			switch kind {
			case token.RPAREN, token.RBRACK, token.RBRACE:
				depth++
			case token.LPAREN, token.LBRACK, token.LBRACE:
				depth--
				if depth < 0 {
					break
				}
				if depth == 0 && kind == token.LPAREN {
					expr, parseErr := parser.ParseExpr("func" + string(source[tokens[j].Start:tokens[i].Start]) + "{}")
					if parseErr == nil {
						literal, ok := expr.(*ast.FuncLit)
						if ok {
							named := true
							for _, field := range literal.Type.Params.List {
								if len(field.Names) == 0 {
									named = false
								}
							}
							if named {
								start = tokens[j].Start
							}
						}
					}
				}
			}
			if depth < 0 {
				break
			}
			if depth == 0 && (kind == token.SEMICOLON || kind == token.DEFINE || kind == token.ASSIGN || kind == token.COMMA || kind == token.RETURN || kind == token.COLON) {
				break
			}
		}
		if start < 0 {
			return fail("invalid arrow function signature; parameters require names and explicit types")
		}
		starts[start] = true
		arrows[tokens[i].Start] = true
	}
	if len(arrows) == 0 {
		return source, nil
	}
	var out strings.Builder
	for i := 0; i < len(source); i++ {
		if starts[i] {
			out.WriteString("func")
		}
		if arrows[i] {
			out.WriteString("  ")
			i++
			continue
		}
		out.WriteByte(source[i])
	}
	return []byte(out.String()), nil
}
