package compiler

import (
	"bytes"
	"context"
	"fmt"
	"go/scanner"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

type formatToken struct {
	kind       token.Token
	text       string
	start, end int
	implicit   bool
}

// FormatSource normalizes whitespace without rewriting Ghi tokens or literals.
// Line breaks are retained because they participate in semicolon insertion.
func FormatSource(filename string, source []byte) (output []byte, err error) {
	// The extension parser predates error recovery for incomplete class members.
	// A malformed editor buffer must be an error, never a formatter panic.
	defer func() {
		if problem := recover(); problem != nil {
			output = nil
			err = fmt.Errorf("%s: invalid source: %v", filename, problem)
		}
	}()
	if _, _, _, err = parseFile(token.NewFileSet(), filename, source); err != nil {
		return nil, fmt.Errorf("%s: %w", filename, err)
	}
	tokens, err := scanFormatTokens(filename, source)
	if err != nil {
		return nil, err
	}
	var out strings.Builder
	depth, lastEnd := 0, 0
	var previous *formatToken
	for i := range tokens {
		current := &tokens[i]
		if current.implicit {
			continue
		}
		gap := string(source[lastEnd:current.start])
		breaks := strings.Count(gap, "\n")
		closing := current.kind == token.RBRACE || current.kind == token.RPAREN || current.kind == token.RBRACK
		if closing && depth > 0 {
			depth--
		}
		if previous == nil || breaks > 0 {
			if previous != nil {
				out.WriteByte('\n')
				if breaks > 1 {
					out.WriteByte('\n')
				}
			}
			out.WriteString(strings.Repeat("\t", depth))
		} else if formatSpace(*previous, *current) {
			out.WriteByte(' ')
		}
		out.WriteString(current.text)
		if current.kind == token.LBRACE || current.kind == token.LPAREN || current.kind == token.LBRACK {
			depth++
		}
		previous = current
		lastEnd = current.end
	}
	out.WriteByte('\n')
	output = []byte(out.String())
	after, err := scanFormatTokens(filename, output)
	if err != nil {
		return nil, err
	}
	if len(after) != len(tokens) {
		return nil, fmt.Errorf("%s: formatting changed token boundaries", filename)
	}
	for i := range tokens {
		if tokens[i].kind != after[i].kind || tokens[i].text != after[i].text || tokens[i].implicit != after[i].implicit {
			return nil, fmt.Errorf("%s: formatting changed token boundaries", filename)
		}
	}
	if _, _, _, err := parseFile(token.NewFileSet(), filename, output); err != nil {
		return nil, fmt.Errorf("%s: formatted source is invalid: %w", filename, err)
	}
	return output, nil
}

func scanFormatTokens(filename string, source []byte) ([]formatToken, error) {
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
	}, scanner.ScanComments)
	var result []formatToken
	for {
		pos, kind, literal := scan.Scan()
		if kind == token.EOF {
			break
		}
		start := file.Offset(pos)
		implicit := kind == token.SEMICOLON && literal == "\n"
		if implicit {
			result = append(result, formatToken{kind: kind, text: "\n", start: start, end: start, implicit: true})
			continue
		}
		if literal == "" {
			literal = kind.String()
		}
		end := start + len(literal)
		// scanner normalizes CR inside raw strings and comments; slice the original
		// source instead so formatting cannot alter their contents.
		if kind == token.STRING && source[start] == '`' {
			end = start + 1 + bytes.IndexByte(source[start+1:], '`') + 1
		} else if kind == token.COMMENT {
			if source[start+1] == '/' {
				end = start + bytes.IndexByte(source[start:], '\n')
				if end < start {
					end = len(source)
				}
				if end > start && source[end-1] == '\r' {
					end--
				}
			} else {
				end = start + bytes.Index(source[start:], []byte("*/")) + 2
			}
		}
		if end < start || end > len(source) {
			return nil, fmt.Errorf("%s: invalid token", filename)
		}
		result = append(result, formatToken{kind: kind, text: string(source[start:end]), start: start, end: end})
	}
	return result, first
}

func formatSpace(previous, current formatToken) bool {
	if previous.kind == token.ASSIGN && current.kind == token.GTR && previous.end == current.start {
		return false
	}
	if previous.kind == token.COMMENT || current.kind == token.COMMENT {
		return true
	}
	if previous.text == "?" {
		return false
	}
	switch current.kind {
	case token.COMMA, token.SEMICOLON, token.PERIOD, token.COLON, token.RPAREN, token.RBRACK:
		return false
	case token.LPAREN:
		if previous.kind == token.DEFINE || previous.kind == token.ASSIGN || previous.kind == token.COMMA || previous.kind == token.RETURN {
			return true
		}
		switch previous.kind {
		case token.IF, token.FOR, token.SWITCH, token.SELECT, token.FUNC:
			return true
		}
		return previous.text == "catch"
	case token.LBRACK:
		return previous.kind.IsOperator() && previous.kind != token.RBRACK && previous.kind != token.LPAREN
	case token.RBRACE:
		return previous.kind != token.LBRACE
	case token.INC, token.DEC:
		return false
	}
	switch previous.kind {
	case token.LPAREN, token.LBRACK, token.PERIOD:
		return false
	}
	return true
}

// FormatProject checks every source (including tests) before writing any file.
// Changed paths are absolute and sorted by directory traversal. In check mode
// no files are written. Syntax errors never leave a partly formatted project.
func FormatProject(ctx context.Context, dir string, check bool) ([]string, error) {
	root, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(root)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("format project: %s is not a directory", root)
	}
	type change struct {
		path string
		data []byte
		mode fs.FileMode
	}
	var changes []change
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			if path != root && (strings.HasPrefix(d.Name(), ".") || d.Name() == "bin" || d.Name() == "vendor" || d.Name() == "node_modules") {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(d.Name(), ".") || filepath.Ext(path) != ".ghi" {
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("source symlinks are not supported: %s", path)
		}
		source, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		output, err := FormatSource(path, source)
		if err != nil {
			return err
		}
		if !bytes.Equal(source, output) {
			info, err := d.Info()
			if err != nil {
				return err
			}
			changes = append(changes, change{path, output, info.Mode()})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	var paths []string
	for _, change := range changes {
		if err := ctx.Err(); err != nil {
			return paths, err
		}
		if !check {
			// Stage the entire file in its directory, then replace it. A failed write
			// never truncates an existing source file.
			temp, err := os.CreateTemp(filepath.Dir(change.path), ".ghi-format-*")
			if err != nil {
				return paths, err
			}
			name := temp.Name()
			_, writeErr := temp.Write(change.data)
			if writeErr == nil {
				writeErr = temp.Chmod(change.mode.Perm())
			}
			closeErr := temp.Close()
			if writeErr == nil {
				writeErr = closeErr
			}
			if writeErr == nil {
				writeErr = os.Rename(name, change.path)
			}
			if writeErr != nil {
				os.Remove(name)
				return paths, writeErr
			}
		}
		paths = append(paths, change.path)
	}
	return paths, nil
}
