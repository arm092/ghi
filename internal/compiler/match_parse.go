package compiler

import (
	"fmt"
	"go/token"
	"sort"
	"strings"
)

const matchResultMarker = "ghi_match_result"

// Match syntax becomes a switch in an immediately invoked function. Only token
// spelling changes: original newlines, comments and literal bytes stay intact.
// The result marker is resolved to a concrete type by the lowering pass.
func normalizeMatches(filename string, source []byte) ([]byte, error) {
	for {
		tokens, err := lexSource(filename, source)
		if err != nil {
			return nil, err
		}
		start := -1
		for i := len(tokens) - 1; i >= 0; i-- {
			if tokens[i].Kind == token.IDENT && tokens[i].Text == "match" && (i == 0 || tokens[i-1].Kind != token.PERIOD) {
				start = i
				break
			}
		}
		if start < 0 {
			return source, nil
		}
		fail := func(at int, message string) ([]byte, error) {
			return nil, fmt.Errorf("%s:%d: match %s", filename, tokens[at].Line, message)
		}
		skip := func(i int) (int, error) {
			switch tokens[i].Kind {
			case token.LPAREN:
				return match(filename, tokens, i, token.LPAREN, token.RPAREN)
			case token.LBRACK:
				return match(filename, tokens, i, token.LBRACK, token.RBRACK)
			case token.LBRACE:
				return match(filename, tokens, i, token.LBRACE, token.RBRACE)
			}
			return i, nil
		}
		open := start + 1
		for open < len(tokens) && tokens[open].Kind != token.LBRACE {
			if tokens[open].Kind == token.EOF || tokens[open].Kind == token.SEMICOLON {
				return fail(start, "requires a subject and a brace body")
			}
			end, err := skip(open)
			if err != nil {
				return nil, err
			}
			open = end + 1
		}
		if open >= len(tokens) || open == start+1 {
			return fail(start, "requires a subject and a brace body")
		}
		close, err := match(filename, tokens, open, token.LBRACE, token.RBRACE)
		if err != nil {
			return nil, err
		}
		type edit struct {
			start, end int
			text       string
		}
		edits := []edit{{tokens[start].Start, tokens[start].End, "(func() " + matchResultMarker + " { switch"}, {tokens[close].Start, tokens[close].End, "}})()"}}
		foundDefault := false
		for i := open + 1; i < close; {
			if tokens[i].Kind == token.SEMICOLON && tokens[i].Text == "\n" {
				i++
				continue
			}
			if foundDefault {
				return fail(i, "default must be the final arm")
			}
			arm := i
			isDefault := tokens[i].Kind == token.DEFAULT
			arrow := -1
			for i < close {
				if tokens[i].Kind == token.ASSIGN && i+1 < close && tokens[i+1].Kind == token.GTR && tokens[i].End == tokens[i+1].Start {
					arrow = i
					break
				}
				if tokens[i].Kind == token.SEMICOLON {
					return fail(i, "arm requires =>")
				}
				end, err := skip(i)
				if err != nil {
					return nil, err
				}
				i = end + 1
			}
			if arrow < 0 || arrow == arm {
				return fail(arm, "arm requires candidates followed by =>")
			}
			if isDefault {
				if arrow != arm+1 {
					return fail(arm, "default cannot have candidates")
				}
				foundDefault = true
			} else {
				edits = append(edits, edit{tokens[arm].Start, tokens[arm].Start, "case "})
			}
			edits = append(edits, edit{tokens[arrow].Start, tokens[arrow+1].End, ": return "})
			i = arrow + 2
			result := i
			if i >= close || tokens[i].Kind == token.LBRACE {
				return fail(arm, "arm requires a result expression")
			}
			for i < close && tokens[i].Kind != token.COMMA {
				if tokens[i].Kind == token.SEMICOLON {
					return fail(i, "arm must end with a comma")
				}
				end, err := skip(i)
				if err != nil {
					return nil, err
				}
				i = end + 1
			}
			if i == result || i >= close {
				return fail(arm, "arm requires a result expression and trailing comma")
			}
			edits = append(edits, edit{tokens[i].Start, tokens[i].End, ";"})
			i++
		}
		if !foundDefault {
			return fail(start, "requires a final default arm")
		}
		sort.SliceStable(edits, func(i, j int) bool { return edits[i].start < edits[j].start })
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
