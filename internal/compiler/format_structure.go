package compiler

import (
	"go/token"
	"sort"
	"strconv"
	"strings"
)

// formatStructure works on the original token stream: extension normalization
// intentionally never enters the printer, so literals and Ghi syntax survive.
func formatStructure(source []byte, all []formatToken) string {
	ts := make([]formatToken, 0, len(all))
	for _, t := range all {
		if !t.implicit {
			if len(ts) > 0 {
				breaks := strings.Count(string(source[ts[len(ts)-1].end:t.start]), "\n")
				t.breakBefore = breaks > 0
				t.blankBefore = breaks > 1
			}
			ts = append(ts, t)
		}
	}
	ts = formatImports(formatFlattenImports(ts))
	pairs := map[int]int{}
	var opens []int
	for i, t := range ts {
		switch t.kind {
		case token.LBRACE, token.LPAREN, token.LBRACK:
			opens = append(opens, i)
		case token.RBRACE, token.RPAREN, token.RBRACK:
			if len(opens) > 0 {
				j := opens[len(opens)-1]
				opens = opens[:len(opens)-1]
				pairs[j] = i
				pairs[i] = j
			}
		}
	}
	type frame struct {
		index                      int
		clause                     int
		literal, cases, activeCase bool
		indented                   bool
		parameters                 bool
		statement                  bool
		matchResult, armEnd        bool
		header                     string
	}
	var stack []*frame
	var out strings.Builder
	depth := 0
	lineStart := true
	pending := 0
	var prev *formatToken
	newline := func(n int) {
		if n > pending {
			pending = n
		}
	}
	emit := func(s string, space bool) {
		if pending > 0 && out.Len() > 0 {
			out.WriteString(strings.Repeat("\n", pending))
			lineStart = true
		}
		pending = 0
		if lineStart {
			out.WriteString(strings.Repeat("\t", depth))
			lineStart = false
		} else if space {
			out.WriteByte(' ')
		}
		out.WriteString(s)
	}
	// The current clause starts at a statement boundary. Parenthesized fragments
	// are skipped when classifying braces and header semicolons.
	clause := 0
	commas := map[int]bool{}
	literalBraces := map[int]bool{}
	header := func(end int) string {
		result := ""
		collection := false
		start := clause
		if len(stack) > 0 && ts[stack[len(stack)-1].index].kind == token.LPAREN {
			start = max(start, stack[len(stack)-1].index+1)
		}
		for j := start; j < end; j++ {
			if ts[j].kind == token.LBRACK && (j == start || (ts[j-1].kind != token.IDENT && ts[j-1].kind != token.RPAREN && ts[j-1].kind != token.RBRACK)) {
				collection = true
			}
			// A function type following a collection type is part of its element
			// type, rather than the beginning of a function body.
			if ts[j].kind == token.FUNC && j > clause && ts[j-1].kind == token.RBRACK {
				continue
			}
			if ts[j].kind == token.STRUCT || ts[j].kind == token.INTERFACE {
				if j+1 < end && ts[j+1].kind == token.LBRACE && pairs[j+1] < end {
					j = pairs[j+1]
					continue
				}
				return ts[j].text
			}
			if ts[j].kind == token.LPAREN || ts[j].kind == token.LBRACK || ts[j].kind == token.LBRACE {
				if k, ok := pairs[j]; ok && k < end {
					if ts[j].kind == token.LBRACE {
						collection = false
					}
					j = k
					continue
				}
			}
			switch ts[j].text {
			case "for", "if", "switch", "select", "func", "constructor", "class", "interface", "struct", "try", "catch", "finally", "else":
				if result == "" {
					result = ts[j].text
				}
			case "match":
				result = "match"
			}
		}
		if collection && (result == "for" || result == "if" || result == "switch") && end > 0 && ts[end-1].kind == token.IDENT {
			return ""
		}
		return result
	}
	lastCode := func(i int) int {
		for i--; i >= 0; i-- {
			if ts[i].kind != token.COMMENT {
				return i
			}
		}
		return -1
	}
	for i := range ts {
		t := &ts[i]
		var top *frame
		if len(stack) > 0 {
			top = stack[len(stack)-1]
		}
		p := lastCode(i)
		gapBreak := t.breakBefore
		if prev != nil && (gapBreak || (prev.kind == token.COMMENT && strings.HasPrefix(prev.text, "//"))) {
			newline(1)
			if t.blankBefore && prev.kind != token.LBRACE && prev.kind != token.LPAREN && t.kind != token.RBRACE && t.kind != token.RPAREN {
				newline(2)
			}
		}
		// A pending structural newline belongs after a trailing comment.
		if t.kind == token.COMMENT && !gapBreak && prev != nil && prev.kind != token.LBRACE {
			pending = 0
		}
		if top != nil && top.header == "match" && top.armEnd && t.kind != token.COMMENT {
			newline(1)
			top.armEnd = false
		}
		if t.kind == token.SEMICOLON {
			h := header(i)
			if (h == "for" || h == "if" || h == "switch") && !(p >= 0 && ts[p].kind == token.RBRACE && !literalBraces[p]) {
				emit(";", false)
				prev = t
				continue
			}
			newline(1)
			clause = i + 1
			continue
		}
		if gapBreak && p >= 0 && formatEndsStatement(ts[p]) && (top == nil || (ts[top.index].kind == token.LBRACE && !top.literal)) {
			clause = i
		}
		if t.kind == token.CASE || t.kind == token.DEFAULT {
			if top != nil && top.cases {
				if top.activeCase {
					depth--
				}
				top.activeCase = false
				newline(1)
				clause = i
			}
		}
		if t.kind == token.RBRACE || t.kind == token.RPAREN || t.kind == token.RBRACK {
			if top != nil {
				if top.indented {
					depth--
					newline(1)
				}
				if t.kind == token.RBRACE {
					clause = top.clause
					empty := i == top.index+1
					if top.activeCase {
						depth--
					}
					depth--
					if !empty {
						newline(1)
					}
				}
				stack = stack[:len(stack)-1]
			}
		}
		if t.kind == token.RETURN && top != nil && !top.literal && ts[top.index].kind == token.LBRACE {
			if top.statement {
				newline(2)
			} else {
				newline(1)
			}
		}
		space := prev != nil && formatSpace(*prev, *t)
		if p >= 0 {
			if t.kind == token.LBRACK && ts[p].kind == token.IDENT {
				end := pairs[i]
				if end == i+1 || (end+1 < len(ts) && ts[end+1].kind == token.IDENT) {
					space = true
				}
			}
			if t.kind == token.LBRACE && pairs[i] == i+1 && (ts[p].kind == token.STRUCT || ts[p].kind == token.INTERFACE) {
				space = false
			}
			if ts[p].kind == token.RBRACK && (t.kind == token.IDENT || t.kind == token.LBRACK || t.kind == token.FUNC || (t.kind == token.LBRACE && header(i) == "")) {
				space = false
			}
			if t.kind == token.LBRACE && header(i) == "" && ts[p].kind != token.GTR && ts[p].kind != token.RPAREN {
				space = false
			}
			if ts[p].kind == token.AND && (p == clause || p == 0 || !formatEndsStatement(ts[max(0, p-1)])) {
				space = false
			}
			if ts[p].kind == token.MUL && (p == clause || p == 0 || !formatEndsStatement(ts[max(0, p-1)])) {
				space = false
			}
			if ts[p].kind == token.MUL && ((top != nil && (top.parameters || top.header == "struct" || top.header == "interface")) || header(i) == "func") {
				space = false
			}
			if ts[p].kind == token.ARROW && (p == clause || p == 0 || !formatEndsStatement(ts[max(0, p-1)])) {
				space = false
			}
			if ts[p].kind == token.NOT {
				space = false
			}
		}
		emit(t.text, space)
		if commas[i] {
			emit(",", false)
		}
		if top != nil && ts[top.index].kind == token.LBRACE && t.kind != token.COMMENT && t.kind != token.RBRACE && t.kind != token.CASE && t.kind != token.DEFAULT {
			top.statement = true
		}
		switch t.kind {
		case token.LBRACE:
			h := header(i)
			literal := h == "" && p >= 0 && (ts[p].kind == token.IDENT || ts[p].kind == token.RBRACK || ts[p].kind == token.RBRACE || ts[p].kind == token.COMMA || ts[p].kind == token.COLON || ts[p].kind == token.LBRACE)
			if top != nil && top.literal && (p == top.index || ts[p].kind == token.COMMA || ts[p].kind == token.COLON) {
				literal = true
			}
			if literal {
				literalBraces[pairs[i]] = true
				last := lastCode(pairs[i])
				if last > i && ts[last].kind != token.COMMA {
					commas[last] = true
				}
			}
			stack = append(stack, &frame{index: i, clause: clause, literal: literal, cases: h == "switch" || h == "select", header: h})
			depth++
			if pairs[i] != i+1 {
				newline(1)
			}
			clause = i + 1
		case token.LPAREN, token.LBRACK:
			indent := t.kind == token.LPAREN && p >= 0 && ts[p].kind == token.IMPORT && pairs[i] != i+1
			parameters := t.kind == token.LPAREN && p >= 0 && (ts[p].kind == token.FUNC || ts[p].text == "constructor" || (p > 0 && ts[p-1].kind == token.FUNC))
			stack = append(stack, &frame{index: i, indented: indent, parameters: parameters})
			if indent {
				depth++
				newline(1)
			}
		case token.COMMA:
			if top != nil && top.header == "match" && top.matchResult {
				newline(1)
				top.matchResult = false
				top.armEnd = true
				clause = i + 1
			}
			if top != nil && top.literal {
				newline(1)
				clause = i + 1
			}
		case token.GTR:
			if top != nil && top.header == "match" && prev != nil && prev.kind == token.ASSIGN && prev.end == t.start {
				top.matchResult = true
				clause = i + 1
			}
		case token.COLON:
			if top != nil && top.cases && !top.activeCase {
				top.activeCase = true
				depth++
				newline(1)
				clause = i + 1
				top.statement = false
			}
		case token.RBRACE:
			next := i + 1
			for next < len(ts) && ts[next].kind == token.COMMENT {
				next++
			}
			if next < len(ts) {
				k := ts[next].kind
				if k != token.COMMA && k != token.RPAREN && k != token.RBRACK && k != token.PERIOD && k != token.LPAREN && k != token.LBRACE && ts[next].text != "else" && ts[next].text != "catch" && ts[next].text != "finally" && !k.IsOperator() {
					newline(1)
					clause = i + 1
				}
			}
		}
		prev = t
	}
	return strings.TrimRight(out.String(), "\n") + "\n"
}

func formatEndsStatement(t formatToken) bool {
	switch t.kind {
	case token.IDENT, token.INT, token.FLOAT, token.IMAG, token.CHAR, token.STRING, token.BREAK, token.CONTINUE, token.FALLTHROUGH, token.RETURN, token.INC, token.DEC, token.RPAREN, token.RBRACK, token.RBRACE:
		return true
	}
	return false
}

// Each import travels with contiguous leading comments and same-line trailing
// comments. The token positions remain original, preserving comment bytes.
func formatFlattenImports(ts []formatToken) []formatToken {
	var result []formatToken
	for i := 0; i < len(ts); i++ {
		if ts[i].kind != token.IMPORT || i+1 >= len(ts) || ts[i+1].kind != token.LPAREN {
			result = append(result, ts[i])
			continue
		}
		declaration := ts[i]
		i += 2
		first := true
		for i < len(ts) && ts[i].kind != token.RPAREN {
			begin := i
			for i < len(ts) && ts[i].kind == token.COMMENT {
				i++
			}
			name := i
			for i < len(ts) && ts[i].kind != token.STRING && ts[i].kind != token.RPAREN {
				i++
			}
			if i >= len(ts) || ts[i].kind == token.RPAREN {
				result = append(result, ts[begin:i]...)
				break
			}
			i++
			for i < len(ts) && ts[i].kind == token.COMMENT && !ts[i].breakBefore {
				i++
			}
			if i < len(ts) && ts[i].kind == token.SEMICOLON {
				i++
			}
			leading := append([]formatToken(nil), ts[begin:name]...)
			if len(leading) > 0 {
				leading[0].breakBefore = true
			}
			imp := declaration
			imp.start = ts[name].start
			imp.end = ts[name].start
			imp.breakBefore = true
			imp.blankBefore = false
			if first {
				if len(leading) > 0 {
					leading[0].blankBefore = declaration.blankBefore
				} else {
					imp.blankBefore = declaration.blankBefore
				}
			}
			result = append(result, leading...)
			result = append(result, imp)
			body := append([]formatToken(nil), ts[name:i]...)
			body[0].breakBefore = false
			body[0].blankBefore = false
			result = append(result, body...)
			first = false
		}
	}
	return result
}

func formatImports(ts []formatToken) []formatToken {
	type entry struct {
		tokens []formatToken
		key    string
	}
	for start := 0; start < len(ts); start++ {
		if ts[start].kind != token.IMPORT {
			continue
		}
		first := start
		for first > 0 && ts[first-1].kind == token.COMMENT && ts[first-1].breakBefore && !ts[first].blankBefore {
			first--
		}
		var entries []entry
		end := first
		for end < len(ts) {
			begin := end
			for end < len(ts) && ts[end].kind == token.COMMENT {
				end++
			}
			if end >= len(ts) || ts[end].kind != token.IMPORT {
				end = begin
				break
			}
			end++
			if end < len(ts) && ts[end].kind == token.LPAREN {
				end = begin
				break
			}
			key := ""
			for end < len(ts) {
				t := ts[end]
				if t.kind == token.SEMICOLON {
					end++
					break
				}
				if end > begin && t.breakBefore {
					break
				}
				if t.kind == token.STRING {
					key, _ = strconv.Unquote(t.text)
				}
				end++
			}
			if key == "" {
				for j := begin; j < end; j++ {
					if ts[j].kind == token.IMPORT {
						for j++; j < end && ts[j].text != "as" && ts[j].kind != token.COMMENT && ts[j].kind != token.SEMICOLON; j++ {
							key += ts[j].text
						}
						break
					}
				}
			}
			entries = append(entries, entry{ts[begin:end], key})
		}
		if len(entries) > 1 {
			sort.SliceStable(entries, func(i, j int) bool { return entries[i].key < entries[j].key })
			var sorted []formatToken
			for _, e := range entries {
				sorted = append(sorted, e.tokens...)
			}
			copy(ts[first:end], sorted)
		}
		if end > start {
			start = end - 1
		}
	}
	return ts
}
