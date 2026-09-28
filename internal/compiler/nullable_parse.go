package compiler

import (
	"bytes"
	"fmt"
	"go/scanner"
	"go/token"
)

// normalizeNullable replaces a prefix question mark with Go's pointer syntax.
// Both representations have the same byte length, retaining diagnostic offsets.
// Scanning first keeps question marks in comments and literals untouched.
func normalizeNullable(filename string, source []byte) ([]byte, error) {
	if !bytes.ContainsRune(source, '?') {
		return source, nil
	}
	ts, _, pairs, err := ternaryTokens(filename, source)
	if err != nil {
		return nil, err
	}
	ternaryOffsets := map[int]bool{}
	for q := range pairs {
		ternaryOffsets[ts[q].start] = true
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
	pending := -1
	for {
		pos, kind, literal := scan.Scan()
		start := file.Offset(pos)
		if pending >= 0 {
			if kind != token.IDENT || start != pending+1 {
				return nil, fmt.Errorf("%s: nullable '?' must immediately precede a type name (use '?Type')", file.Position(file.Pos(pending)))
			}
			pending = -1
		}
		if kind == token.EOF {
			break
		}
		if kind == token.ILLEGAL && literal == "?" {
			if ternaryOffsets[start] {
				continue
			}
			output[start] = '*'
			pending = start
		}
	}
	if first != nil {
		return nil, first
	}
	return output, nil
}
