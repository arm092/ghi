package compiler

import (
	"fmt"
	"go/token"
	"strings"
)

// normalizeExceptions represents extended statements as marker calls containing
// ordinary Go AST blocks. The control-flow pass removes every marker before
// type checking and restores Ghi return/break/continue semantics.
func normalizeExceptions(filename string, source []byte) ([]byte, error) {
	return normalizeExceptionsAt(filename, source, 1)
}

func normalizeExceptionsAt(filename string, source []byte, baseLine int) ([]byte, error) {
	tokens, err := lexSource(filename, source)
	if err != nil {
		return nil, err
	}
	var out strings.Builder
	cursor := 0
	location := func(offset int) string {
		prefix := string(source[:offset])
		line := baseLine + strings.Count(prefix, "\n")
		column := offset - strings.LastIndex(prefix, "\n")
		return fmt.Sprintf("\n//line %s:%d:%d\n", filename, line, column)
	}
	normalizeBlock := func(begin, end int) (string, error) {
		start := tokens[begin].End
		line := baseLine + strings.Count(string(source[:start]), "\n")
		data, err := normalizeExceptionsAt(filename, source[start:tokens[end].Start], line)
		return location(start) + string(data), err
	}
	for i := 0; i < len(tokens); i++ {
		if tokens[i].Kind != token.IDENT {
			continue
		}
		t := tokens[i]
		if t.Text == "throw" {
			j := i + 1
			depth := 0
			for ; j < len(tokens); j++ {
				kind := tokens[j].Kind
				if depth == 0 && (kind == token.SEMICOLON || kind == token.RBRACE || kind == token.EOF) {
					break
				}
				switch kind {
				case token.LPAREN, token.LBRACK, token.LBRACE:
					depth++
				case token.RPAREN, token.RBRACK, token.RBRACE:
					depth--
				}
			}
			if j >= len(tokens) {
				return nil, fmt.Errorf("%s:%d: incomplete throw", filename, t.Line)
			}
			expressionEnd := tokens[j-1].End
			expression := strings.TrimSpace(string(source[t.End:expressionEnd]))
			if expression == "" {
				return nil, fmt.Errorf("%s:%d: throw requires an exception", filename, t.Line)
			}
			out.Write(source[cursor:t.Start])
			leading := len(string(source[t.End:expressionEnd])) - len(strings.TrimLeft(string(source[t.End:expressionEnd]), " \t\r\n"))
			fmt.Fprintf(&out, "GhiThrow(%s%s)%s%s", location(t.End+leading), expression, source[expressionEnd:tokens[j].Start], location(tokens[j].Start))
			cursor = tokens[j].Start
			i = j - 1
		}
		if t.Text != "try" {
			continue
		}
		begin := i + 1
		if tokens[begin].Kind != token.LBRACE {
			return nil, fmt.Errorf("%s:%d: try requires a block", filename, t.Line)
		}
		end, err := match(tokens, begin, token.LBRACE, token.RBRACE)
		if err != nil {
			return nil, err
		}
		body, err := normalizeBlock(begin, end)
		if err != nil {
			return nil, err
		}
		last := end
		type handler struct{ name, typ, body string }
		var handlers []handler
		finally := ""
		hasFinally := false
		for {
			next := last + 1
			for next < len(tokens) && tokens[next].Kind == token.SEMICOLON {
				next++
			}
			if next >= len(tokens) || (tokens[next].Text != "catch" && tokens[next].Text != "finally") {
				break
			}
			kind := tokens[next].Text
			next++
			if hasFinally {
				return nil, fmt.Errorf("%s:%d: finally must be last", filename, t.Line)
			}
			name, typ := "", ""
			if kind == "catch" {
				if tokens[next].Kind != token.IDENT {
					return nil, fmt.Errorf("catch requires a variable and exception type")
				}
				name = tokens[next].Text
				next++
				typeStart := next
				for next < len(tokens) && tokens[next].Kind != token.LBRACE && tokens[next].Kind != token.EOF {
					next++
				}
				typ = strings.TrimSpace(string(source[tokens[typeStart].Start:tokens[next].Start]))
				if typ == "" {
					return nil, fmt.Errorf("catch requires an exception type")
				}
			}
			if tokens[next].Kind != token.LBRACE {
				return nil, fmt.Errorf("%s requires a block", kind)
			}
			end, err = match(tokens, next, token.LBRACE, token.RBRACE)
			if err != nil {
				return nil, err
			}
			content, err := normalizeBlock(next, end)
			if err != nil {
				return nil, err
			}
			if kind == "catch" {
				handlers = append(handlers, handler{name, typ, content})
			} else {
				hasFinally = true
				finally = content
			}
			last = end
		}
		if len(handlers) == 0 && !hasFinally {
			return nil, fmt.Errorf("try requires catch or finally")
		}
		catcher := "nil"
		if len(handlers) > 0 {
			var text strings.Builder
			text.WriteString("func(ghi_caught Exception) { ")
			for index, h := range handlers {
				if index > 0 {
					text.WriteString(" else ")
				}
				fmt.Fprintf(&text, "if %s, ghi_matched := ghi_caught.(%s); ghi_matched { _ = %s; %s\n}", h.name, h.typ, h.name, h.body)
			}
			text.WriteString(" else { GhiThrow(ghi_caught) } }")
			catcher = text.String()
		}
		finalizer := "nil"
		if hasFinally {
			finalizer = "func(){" + finally + "\n}"
		}
		out.Write(source[cursor:t.Start])
		fmt.Fprintf(&out, "GhiTry(func(){%s\n}, %s, %s)", body, catcher, finalizer)
		cursor = tokens[last].End
		out.WriteString(location(cursor))
		i = last
	}
	out.Write(source[cursor:])
	return []byte(out.String()), nil
}
