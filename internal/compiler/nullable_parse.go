package compiler

import (
	"bytes"
	"fmt"
	"go/scanner"
	"go/token"
)

// normalizeNullable rotates a postfix question mark into Go's pointer syntax.
// Both representations have the same byte length, retaining diagnostic offsets.
// Scanning first keeps question marks in comments and literals untouched.
func normalizeNullable(filename string, source []byte) ([]byte, error) {
	if !bytes.ContainsRune(source, '?') {
		return source, nil
	}
	fset := token.NewFileSet()
	file := fset.AddFile(filename, -1, len(source))
	var scan scanner.Scanner
	var first error
	scan.Init(file, source, func(pos token.Position, message string) {
		if pos.Offset < len(source) && source[pos.Offset] == '?' {
			return
		}
		if first == nil {
			first = fmt.Errorf("%s: %s", pos, message)
		}
	}, 0)
	output := bytes.Clone(source)
	var previous []lexeme
	for {
		pos, kind, literal := scan.Scan()
		if kind == token.EOF {
			break
		}
		start := file.Offset(pos)
		if kind == token.ILLEGAL && literal == "?" {
			last := len(previous) - 1
			if last < 0 || previous[last].Kind != token.IDENT || previous[last].End != start {
				return nil, fmt.Errorf("%s: nullable '?' must immediately follow a type name", fset.Position(pos))
			}
			begin := previous[last].Start
			for last >= 2 && previous[last-1].Kind == token.PERIOD && previous[last-2].Kind == token.IDENT {
				last -= 2
				begin = previous[last].Start
			}
			copy(output[begin+1:start+1], source[begin:start])
			output[begin] = '*'
		}
		end := start + len(literal)
		if literal == "" {
			end = start + len(kind.String())
		}
		previous = append(previous, lexeme{Kind: kind, Text: literal, Start: start, End: end})
	}
	if first != nil {
		return nil, first
	}
	return output, nil
}
