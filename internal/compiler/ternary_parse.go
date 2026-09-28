package compiler

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
	"strings"
)

const ternaryResultMarker = "ghi_ternary_result"

// Keep comments outside the grammar while retaining their source bytes in edits.
func ternaryTokens(filename string, source []byte) ([]formatToken, map[int]int, map[int]int, error) {
	all, err := scanFormatTokens(filename, source)
	if err != nil {
		return nil, nil, nil, err
	}
	var ts []formatToken
	for _, t := range all {
		if t.kind != token.COMMENT {
			ts = append(ts, t)
		}
	}
	groups := map[int]int{}
	var stack []int
	for i, t := range ts {
		switch t.kind {
		case token.LPAREN, token.LBRACK, token.LBRACE:
			stack = append(stack, i)
		case token.RPAREN, token.RBRACK, token.RBRACE:
			if len(stack) != 0 {
				j := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				groups[i] = j
				groups[j] = i
			}
		}
	}
	pairs := map[int]int{}
	for i, t := range ts {
		if t.text != "?" {
			continue
		}
		for j := i + 1; j < len(ts); j++ {
			if end, ok := groups[j]; ok && end > j {
				j = end
				continue
			}
			k := ts[j].kind
			if k == token.ASSIGN && j+1 < len(ts) && ts[j+1].kind == token.GTR && ts[j].end == ts[j+1].start {
				continue
			}
			if k == token.COLON {
				pairs[i] = j
				break
			}
			if k == token.COMMA || k == token.SEMICOLON || k == token.ASSIGN || k == token.DEFINE || k == token.RPAREN || k == token.RBRACK || k == token.RBRACE {
				break
			}
		}
	}
	return ts, groups, pairs, nil
}

func normalizeTernaries(filename string, source []byte, mappings ...*coverageSourceMap) ([]byte, error) {
	if !bytes.ContainsRune(source, '?') {
		return source, nil
	}
	for {
		ts, groups, pairs, err := ternaryTokens(filename, source)
		if err != nil {
			return nil, err
		}
		if len(pairs) == 0 {
			return source, nil
		}
		// Outermost first lets us reject unparenthesized nested branches before
		// inserted function parentheses could accidentally make them legal.
		questions := make([]int, 0, len(pairs))
		for q := range pairs {
			questions = append(questions, q)
		}
		sort.Ints(questions)
		q := questions[0]
		colon := pairs[q]
		fail := func(message string) ([]byte, error) {
			return nil, fmt.Errorf("%s: ternary %s", tokenPosition(source, filename, ts[q].start), message)
		}
		start := q - 1
		for ; start >= 0; start-- {
			if open, ok := groups[start]; ok && open < start {
				start = open
				continue
			}
			k := ts[start].kind
			if k == token.ARROW && start > 0 && formatEndsStatement(ts[start-1]) {
				break
			}
			if k == token.GTR && start > 0 && ts[start-1].kind == token.ASSIGN && ts[start-1].end == ts[start].start {
				break
			}
			if k == token.LPAREN || k == token.LBRACK || k == token.LBRACE || k == token.COMMA || k == token.SEMICOLON || k == token.COLON || k == token.RETURN || k == token.IF || k == token.FOR || k == token.SWITCH || k == token.CASE || k == token.RANGE || k == token.DEFINE || (k == token.ASSIGN || k >= token.ADD_ASSIGN && k <= token.AND_NOT_ASSIGN) || ts[start].text == "throw" {
				break
			}
		}
		start++
		header := ternaryInHeader(ts, groups, start)
		end := colon + 1
	scanEnd:
		for ; end < len(ts); end++ {
			if header && ts[end].kind == token.LBRACE {
				// A completed false expression ends at the control statement's
				// body. Function literals still need their own brace body first.
				if expr, err := parser.ParseExpr(string(source[ts[colon].end:ts[end].start])); err == nil {
					switch expr.(type) {
					case *ast.ArrayType, *ast.MapType, *ast.StructType, *ast.FuncType:
						// These prefixes still need their literal body.
					default:
						break scanEnd
					}
				}
			}
			if close, ok := groups[end]; ok && close > end {
				end = close
				continue
			}
			k := ts[end].kind
			if k == token.COMMA || k == token.SEMICOLON || k == token.COLON || k == token.RPAREN || k == token.RBRACK || k == token.RBRACE {
				break
			}
		}
		if start == q || colon == q+1 || end == colon+1 {
			return fail("requires a condition and two values")
		}
		for i := q + 1; i < end; i++ {
			if close, ok := groups[i]; ok && close > i {
				i = close
				continue
			}
			if _, nested := pairs[i]; nested {
				return fail("nested expressions require parentheses")
			}
		}
		last := ts[end-1].end
		edits := []sourceEdit{
			{ts[start].start, ts[start].start, "(func() " + ternaryResultMarker + " { if "},
			{ts[q].start, ts[q].end, " { return "},
			{ts[colon].start, ts[colon].end, " }; return "},
			{last, last, " })()"},
		}
		for _, m := range mappings {
			m.apply(source, edits)
		}
		var out strings.Builder
		previous := 0
		for _, e := range edits {
			out.Write(source[previous:e.start])
			out.WriteString(e.text)
			previous = e.end
		}
		out.Write(source[previous:])
		source = []byte(out.String())
	}
}

func tokenPosition(source []byte, filename string, offset int) string {
	line := 1 + strings.Count(string(source[:offset]), "\n")
	column := offset - strings.LastIndex(string(source[:offset]), "\n")
	return fmt.Sprintf("%s:%d:%d", filename, line, column)
}

func ternaryInHeader(ts []formatToken, groups map[int]int, start int) bool {
	for i := start - 1; i >= 0; i-- {
		switch ts[i].kind {
		case token.IF, token.FOR, token.SWITCH, token.RANGE:
			return true
		case token.LBRACE, token.RBRACE:
			return false
		case token.SEMICOLON:
			if i != start-1 {
				return false
			}
		case token.RPAREN, token.RBRACK:
			if open, ok := groups[i]; ok {
				i = open
			}
		}
	}
	return false
}
