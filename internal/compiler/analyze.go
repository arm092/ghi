package compiler

import (
	"crypto/sha256"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"unicode/utf8"
)

// SyntaxAnalysis is a versioned source tooling contract. It deliberately does
// not expose the lowered Go AST as if it were the original Ghi syntax tree.
type SyntaxAnalysis struct {
	SchemaVersion int                `json:"schemaVersion"`
	Filename      string             `json:"filename"`
	Namespace     string             `json:"namespace"`
	SHA256        string             `json:"sha256"`
	Capabilities  []string           `json:"capabilities"`
	Tokens        []SyntaxToken      `json:"tokens"`
	Diagnostics   []SyntaxDiagnostic `json:"diagnostics"`
}

// Offsets are zero-based UTF-8 byte offsets; End is exclusive. Line and Column
// are one-based byte positions. Matching is the paired delimiter's token index,
// or -1. Implicit semicolons have zero width and Text "\n".
type SyntaxToken struct {
	Kind     string `json:"kind"`
	Text     string `json:"text"`
	Start    int    `json:"start"`
	End      int    `json:"end"`
	Line     int    `json:"line"`
	Column   int    `json:"column"`
	Implicit bool   `json:"implicit"`
	Matching int    `json:"matching"`
}

type SyntaxDiagnostic struct {
	Kind    string `json:"kind"`
	Message string `json:"message"`
}

// AnalyzeSource validates syntax without installing Go, resolving dependencies,
// lowering a project, or changing files. Symbols are not type-resolved. Failed
// input returns a diagnostic and no partial token stream or capabilities.
func AnalyzeSource(filename string, source []byte) (result SyntaxAnalysis) {
	result = SyntaxAnalysis{
		SchemaVersion: 1, Filename: filename,
		SHA256:       fmt.Sprintf("%x", sha256.Sum256(source)),
		Capabilities: []string{}, Tokens: []SyntaxToken{}, Diagnostics: []SyntaxDiagnostic{},
	}
	fail := func(err error) {
		result.Namespace = ""
		result.Capabilities = []string{}
		result.Tokens = []SyntaxToken{}
		result.Diagnostics = []SyntaxDiagnostic{{Kind: "syntax", Message: err.Error()}}
	}
	defer func() {
		if problem := recover(); problem != nil {
			fail(fmt.Errorf("%s: invalid source: %v", filename, problem))
		}
	}()
	if !utf8.Valid(source) {
		fail(fmt.Errorf("%s: source must be UTF-8", filename))
		return
	}
	namespace, _, unit, err := parseFile(token.NewFileSet(), filename, source)
	if err != nil {
		fail(fmt.Errorf("%s: %w", filename, err))
		return
	}
	// Classes are extracted before the final Go parse. Check their erased type
	// fragments in type context; ParseExpr alone also accepts values such as A+B.
	for _, class := range unit.Classes {
		types := append([]string{}, class.InterfaceNames...)
		if class.ParentName != "" {
			types = append(types, class.ParentName)
		}
		for _, field := range class.Fields {
			types = append(types, expressionText(field.Type))
		}
		for _, typ := range types {
			if _, exprErr := parser.ParseExpr(typ); exprErr != nil {
				fail(fmt.Errorf("%s:%d: invalid type in class %s: %v", filename, class.Line, class.Name, exprErr))
				return
			}
			tree, typeErr := parser.ParseFile(token.NewFileSet(), filename, "package syntax\ntype Checked "+typ, parser.AllErrors)
			// An injected second declaration must not turn a malformed type into
			// an otherwise parseable synthetic file.
			if typeErr == nil && (len(tree.Decls) != 1 || len(tree.Decls[0].(*ast.GenDecl).Specs) != 1) {
				typeErr = fmt.Errorf("expected one type")
			}
			if typeErr == nil {
				spec := tree.Decls[0].(*ast.GenDecl).Specs[0].(*ast.TypeSpec)
				if spec.Assign.IsValid() || spec.TypeParams != nil {
					typeErr = fmt.Errorf("expected a type, not a declaration")
				}
			}
			if typeErr != nil {
				fail(fmt.Errorf("%s:%d: invalid type in class %s: %v", filename, class.Line, class.Name, typeErr))
				return
			}
		}
	}
	tokens, err := scanFormatTokens(filename, source)
	if err != nil {
		fail(err)
		return
	}
	line, column, offset := 1, 1, 0
	stack := []int{}
	for _, item := range tokens {
		for offset < item.start {
			if source[offset] == '\n' {
				line, column = line+1, 1
			} else {
				column++
			}
			offset++
		}
		kind := item.kind.String()
		if item.text == "?" {
			kind = "?"
		}
		index := len(result.Tokens)
		result.Tokens = append(result.Tokens, SyntaxToken{
			Kind: kind, Text: item.text, Start: item.start, End: item.end,
			Line: line, Column: column, Implicit: item.implicit, Matching: -1,
		})
		switch item.kind {
		case token.LPAREN, token.LBRACK, token.LBRACE:
			stack = append(stack, index)
		case token.RPAREN, token.RBRACK, token.RBRACE:
			if len(stack) == 0 {
				fail(fmt.Errorf("%s:%d:%d: unmatched delimiter", filename, line, column))
				return
			}
			open := stack[len(stack)-1]
			want := map[token.Token]string{token.RPAREN: "(", token.RBRACK: "[", token.RBRACE: "{"}[item.kind]
			if result.Tokens[open].Kind != want {
				fail(fmt.Errorf("%s:%d:%d: mismatched delimiter", filename, line, column))
				return
			}
			result.Tokens[index].Matching = open
			result.Tokens[open].Matching = index
			stack = stack[:len(stack)-1]
		}
	}
	if len(stack) != 0 {
		fail(fmt.Errorf("%s: unclosed delimiter", filename))
		return
	}
	result.Namespace = namespace
	result.Capabilities = []string{"syntax", "tokens", "delimiterPairs"}
	return
}
