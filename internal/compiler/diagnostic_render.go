package compiler

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

var diagnosticLocation = regexp.MustCompile(`^(.+\.ghi):([0-9]+)(?::([0-9]+))?:`)
var diagnosticMismatch = regexp.MustCompile(`cannot use .+ \(([^\n]+)\) as (.+?) value(?: |$)`)

// FormatDiagnostic retains the machine-readable first line and adds source
// context for CLI users. Error() and editor-overlay output stay unchanged.
func FormatDiagnostic(err error, root string, overlay map[string][]byte) string {
	if err == nil {
		return ""
	}
	message := err.Error()
	location := diagnosticLocation.FindStringSubmatch(message)
	if location == nil {
		return message
	}
	root, rootErr := filepath.Abs(root)
	path, pathErr := filepath.Abs(location[1])
	if rootErr != nil || pathErr != nil {
		return message
	}
	rel, relErr := filepath.Rel(root, path)
	if relErr != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return message
	}
	source, ok := overlay[path]
	if !ok {
		var readErr error
		source, readErr = os.ReadFile(path)
		if readErr != nil {
			return message
		}
	}
	lineNumber, _ := strconv.Atoi(location[2])
	lines := strings.Split(string(source), "\n")
	if lineNumber < 1 || lineNumber > len(lines) {
		return message
	}
	line := strings.TrimSuffix(lines[lineNumber-1], "\r")
	column, _ := strconv.Atoi(location[3])
	gutter := strings.Repeat(" ", len(location[2]))
	result := fmt.Sprintf("%s\n%s |\n%d | %s", message, gutter, lineNumber, expandDiagnosticLine(line))
	if column > 0 && column <= len(line)+1 && utf8.ValidString(line[:column-1]) {
		prefix := expandDiagnosticLine(line[:column-1])
		width := 0
		for _, r := range line[column-1:] {
			if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' {
				break
			}
			width++
		}
		result += fmt.Sprintf("\n%s | %s^%s", gutter, strings.Repeat(" ", utf8.RuneCountInString(prefix)), strings.Repeat("~", max(0, width-1)))
	}
	if types := diagnosticMismatch.FindStringSubmatch(message); types != nil {
		result += fmt.Sprintf("\n%s = expected: %s; received: %s", gutter, types[2], types[1])
	}
	if hint := diagnosticHint(message); hint != "" {
		result += fmt.Sprintf("\n%s = hint: %s", gutter, hint)
	}
	return result
}

func diagnosticHint(message string) string {
	switch {
	case strings.HasSuffix(message, ": non-boolean condition in if statement"):
		return "Use a bool condition, such as count > 0 or value != nil; numbers and strings are not implicitly converted to bool."
	case strings.HasSuffix(message, ": ternary nested expressions require parentheses"):
		return "Parenthesize the nested expression: first ? a : (second ? b : c)."
	case strings.HasSuffix(message, ": ternary requires a condition and two values"):
		return "Use condition ? trueValue : falseValue; both values are required."
	case strings.HasSuffix(message, ": ternary result type cannot be inferred; every arm must produce one typed value"):
		return "Give each branch one value with a compatible type; two untyped nil values cannot establish a result type."
	}
	return ""
}

func expandDiagnosticLine(line string) string {
	var out strings.Builder
	column := 0
	for _, r := range line {
		if r == '\t' {
			spaces := 4 - column%4
			out.WriteString(strings.Repeat(" ", spaces))
			column += spaces
		} else if unicode.IsControl(r) {
			escaped := strconv.QuoteRune(r)
			out.WriteString(escaped)
			column += len(escaped)
		} else {
			out.WriteRune(r)
			column++
		}
	}
	return out.String()
}
