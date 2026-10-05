package compiler

import (
	"bytes"
	"context"
	"fmt"
	"ghi/internal/toolchain"
	"go/ast"
	"go/printer"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// AnalyzeExpressionTypes checks a complete production project, replacing one
// existing source file in memory. Unlike AnalyzeSource it requires Go and the
// project's resolved dependencies. Failure never advertises partial results.
func AnalyzeExpressionTypes(ctx context.Context, project, filename string, source []byte) (result SyntaxAnalysis) {
	result = AnalyzeSource(filename, source)
	if len(result.Diagnostics) != 0 {
		return
	}
	fail := func(err error) {
		result.Namespace = ""
		result.Capabilities = []string{}
		result.Tokens = []SyntaxToken{}
		result.ExpressionTypes = nil
		result.Diagnostics = []SyntaxDiagnostic{{Kind: "semantic", Message: err.Error()}}
	}
	defer func() {
		if problem := recover(); problem != nil {
			fail(fmt.Errorf("%s: analysis failed: %v", filename, problem))
		}
	}()
	root, err := filepath.Abs(project)
	if err != nil {
		fail(err)
		return
	}
	target, err := filepath.Abs(filename)
	if err != nil {
		fail(err)
		return
	}
	if project == "" || filepath.Ext(target) != ".ghi" {
		fail(fmt.Errorf("expression type analysis requires a project and a .ghi filename"))
		return
	}
	p, err := loadProjectOverlay(root, false, map[string][]byte{target: source})
	if err != nil {
		fail(err)
		return
	}
	p.AnalyzeTypes = true
	var selected *sourceFile
	var selectedNamespace *namespace
	for _, ns := range p.Ordered {
		for _, file := range ns.Files {
			if file.Path == target {
				selected, selectedNamespace = file, ns
			}
		}
	}
	if selected == nil {
		fail(fmt.Errorf("source is not in the production project: %s", filename))
		return
	}
	candidates := p.expressionCandidates(selected)
	// Match check/build's rejection of bodyless production declarations.
	for _, ns := range p.Ordered {
		for _, file := range ns.Files {
			for _, decl := range file.Tree.Decls {
				if fn, ok := decl.(*ast.FuncDecl); ok && fn.Body == nil {
					fail(fmt.Errorf("%s: function %s requires a body", p.Fset.Position(fn.Pos()), fn.Name.Name))
					return
				}
			}
		}
	}
	workspace, err := os.MkdirTemp("", "ghi-analyze-")
	if err != nil {
		fail(err)
		return
	}
	defer os.RemoveAll(workspace)
	goPath, err := (toolchain.Manager{}).Ensure(ctx)
	if err != nil {
		fail(err)
		return
	}
	defer func() {
		if p.verification != nil {
			p.verification.cancel()
			_ = p.verifyDependencies()
		}
	}()
	if err = p.prepareGeneration(ctx, workspace, goPath); err == nil {
		err = p.lower(ctx, goPath, workspace)
	}
	if verificationErr := p.verifyDependencies(); verificationErr != nil {
		err = verificationErr
	}
	if err == nil {
		err = p.validateEntry()
	}
	if err != nil {
		fail(p.sourceError(err))
		return
	}
	if p.AnalysisInfo == nil {
		fail(fmt.Errorf("expression type analysis did not complete"))
		return
	}
	seen := map[[2]int]bool{}
	bridgedFunctions := map[ast.Expr]bool{}
	for call := range p.Wrapped {
		ast.Inspect(call.Fun, func(node ast.Node) bool {
			if expr, ok := node.(ast.Expr); ok {
				bridgedFunctions[expr] = true
			}
			return true
		})
	}
	for expr, span := range candidates {
		// A bridged native call survives inside a helper as a raw Go tuple.
		// That inner node does not describe the original Ghi call result.
		if call, ok := expr.(*ast.CallExpr); ok && p.Wrapped[call] {
			continue
		}
		if bridgedFunctions[expr] {
			continue
		}
		value, ok := p.AnalysisInfo.Types[expr]
		if !ok || value.Type == nil || value.IsType() || value.IsVoid() {
			continue
		}
		typ := p.analysisType(value.Type, selected, selectedNamespace)
		if typ == "" || seen[span] {
			continue
		}
		seen[span] = true
		result.ExpressionTypes = append(result.ExpressionTypes, ExpressionType{Start: span[0], End: span[1], Type: typ})
	}
	sort.Slice(result.ExpressionTypes, func(i, j int) bool {
		a, b := result.ExpressionTypes[i], result.ExpressionTypes[j]
		if a.Start != b.Start {
			return a.Start < b.Start
		}
		return a.End < b.End
	})
	result.Capabilities = append(result.Capabilities, "expressionTypes")
	return
}

// Capture user nodes before lowering mutates names and injects expressions.
// Both endpoint origins and the complete token sequence must match the source.
// Width-changing rewrites with inserted bytes are deliberately omitted.
func (p *program) expressionCandidates(file *sourceFile) map[ast.Expr][2]int {
	result := map[ast.Expr][2]int{}
	// User line directives can override fragment positions. Do not interpret
	// those logical coordinates as physical offsets into an editor buffer.
	if tokens, err := scanFormatTokens(file.Path, file.Source); err == nil {
		for _, item := range tokens {
			if item.kind == token.COMMENT && (strings.HasPrefix(item.text, "//line ") || strings.HasPrefix(item.text, "/*line ")) {
				return result
			}
		}
	}
	lines := []int{0}
	for i, b := range file.Source {
		if b == '\n' {
			lines = append(lines, i+1)
		}
	}
	fragments := map[*token.File]*functionDecl{}
	nodes := []ast.Node{file.Tree}
	for _, class := range file.Unit.Classes {
		for _, fn := range class.Methods {
			if fn.Node != nil {
				fragments[p.Fset.File(fn.Node.Pos())] = fn
				nodes = append(nodes, fn.Node.Body)
			}
		}
		if fn := class.Constructor; fn != nil && fn.Node != nil {
			fragments[p.Fset.File(fn.Node.Pos())] = fn
			nodes = append(nodes, fn.Node.Body)
		}
	}
	offset := func(pos token.Pos) int {
		parsedFile := p.Fset.File(pos)
		if parsedFile != p.Fset.File(file.Tree.Pos()) && fragments[parsedFile] == nil {
			return -1
		}
		location := p.Fset.Position(pos)
		if fn := fragments[p.Fset.File(pos)]; fn != nil && fn.SourceTail.IsValid() {
			if location.Column == 0 {
				location.Column = p.Fset.PositionFor(pos, false).Column
				if location.Line == fn.SourceTail.Line {
					location.Column += fn.SourceTail.Column - 1 - fn.HeaderWidth
				}
			} else if location.Line == fn.SourceTail.Line {
				location.Column += fn.SourceTail.Column - 1
			}
		}
		location = file.Unit.CoverageSource.position(location)
		// The namespace header and optional BOM change first-line widths.
		// Current parser origins do not expose that header transformation.
		if location.Line == 1 {
			return -1
		}
		if location.Filename != file.Path || location.Line < 1 || location.Line > len(lines) || location.Column < 1 {
			return -1
		}
		return lines[location.Line-1] + location.Column - 1
	}
	for _, root := range nodes {
		if root == nil {
			continue
		}
		ast.Inspect(root, func(node ast.Node) bool {
			expr, ok := node.(ast.Expr)
			if !ok || !expr.Pos().IsValid() || expr.End() <= expr.Pos() {
				return true
			}
			start, last := offset(expr.Pos()), offset(expr.End()-1)
			if start < 0 || last < start || last >= len(file.Source) {
				return true
			}
			// Reject any injected byte or discontinuity inside the expression.
			for pos := expr.Pos(); pos < expr.End(); pos++ {
				if offset(pos) != start+int(pos-expr.Pos()) {
					return true
				}
			}
			var printed bytes.Buffer
			if printer.Fprint(&printed, p.Fset, expr) != nil || !sameExpressionTokens(file.Source[start:last+1], printed.Bytes()) {
				return true
			}
			result[expr] = [2]int{start, last + 1}
			return true
		})
	}
	return result
}

func sameExpressionTokens(source, printed []byte) bool {
	left, err := lexSource("source", source)
	if err != nil {
		return false
	}
	right, err := lexSource("expression", printed)
	if err != nil {
		return false
	}
	words := func(tokens []lexeme) []string {
		var result []string
		for _, item := range tokens {
			if item.Kind != token.EOF && item.Kind != token.SEMICOLON {
				text := item.Text
				if text == "" {
					text = item.Kind.String()
				}
				result = append(result, text)
			}
		}
		return result
	}
	return strings.Join(words(left), "\x00") == strings.Join(words(right), "\x00")
}

func (p *program) analysisType(typ types.Type, file *sourceFile, ns *namespace) string {
	displayQualifier := func(pkg *types.Package) string {
		if pkg.Path() == namespacePath(ns) {
			return ""
		}
		for _, spec := range file.Tree.Imports {
			path, _ := strconv.Unquote(spec.Path.Value)
			if path == pkg.Path() && spec.Name != nil && spec.Name.Name != "." && !strings.HasPrefix(spec.Name.Name, "ghi_") {
				return spec.Name.Name
			}
		}
		return pkg.Name()
	}
	// Distinct namespaces can have identical Go package names. Use private
	// placeholders while formatting, then resolve selected types by package
	// identity before rendering each remaining source package qualifier.
	packageQualifiers := map[string]string{}
	qualifierNames := map[string]string{}
	qualify := func(pkg *types.Package) string {
		if pkg.Path() == namespacePath(ns) {
			return ""
		}
		if existing := packageQualifiers[pkg.Path()]; existing != "" {
			return existing
		}
		placeholder := fmt.Sprintf("ghi_analysis_package_%d", len(packageQualifiers))
		packageQualifiers[pkg.Path()] = placeholder
		qualifierNames[placeholder] = displayQualifier(pkg)
		return placeholder
	}
	text := types.TypeString(typ, qualify)
	selectedNames := map[string]string{}
	for _, selected := range file.Unit.TypeImports {
		if imported := p.Namespaces[selected.Namespace]; imported != nil {
			pkg := types.NewPackage(namespacePath(imported), imported.GoName)
			selectedNames[qualify(pkg)+"."+selected.Name] = selected.Alias
		}
	}
	// Nullable class references also appear inside containers/signatures and
	// through whole-namespace imports. Preserve native pointers everywhere else.
	var nullablePointers func(types.Type)
	seen := map[types.Type]bool{}
	nullablePointers = func(typ types.Type) {
		if typ == nil || seen[typ] {
			return
		}
		seen[typ] = true
		switch value := typ.(type) {
		case *types.Pointer:
			if p.classType(value.Elem()) != nil {
				original := types.TypeString(value, qualify)
				pattern := regexp.MustCompile(regexp.QuoteMeta(original) + `($|[^\pL\pN_])`)
				text = pattern.ReplaceAllStringFunc(text, func(match string) string {
					return "?" + types.TypeString(value.Elem(), qualify) + strings.TrimPrefix(match, original)
				})
			}
			nullablePointers(value.Elem())
		case *types.Slice:
			nullablePointers(value.Elem())
		case *types.Array:
			nullablePointers(value.Elem())
		case *types.Map:
			nullablePointers(value.Key())
			nullablePointers(value.Elem())
		case *types.Chan:
			nullablePointers(value.Elem())
		case *types.Signature:
			nullablePointers(value.Params())
			nullablePointers(value.Results())
		case *types.Tuple:
			for i := 0; i < value.Len(); i++ {
				nullablePointers(value.At(i).Type())
			}
		case *types.Named:
			if value.TypeArgs() != nil {
				for i := 0; i < value.TypeArgs().Len(); i++ {
					nullablePointers(value.TypeArgs().At(i))
				}
			}
		case *types.Struct:
			for i := 0; i < value.NumFields(); i++ {
				nullablePointers(value.Field(i).Type())
			}
		}
	}
	nullablePointers(typ)
	// Type names are complete lexical identifiers, never diagnostic substrings:
	// selecting model.Box as Alias must not rename model.Boxed to Aliased.
	if tokens, err := lexSource("type", []byte(text)); err == nil {
		for i := len(tokens) - 3; i >= 0; i-- {
			if tokens[i].Kind != token.IDENT || tokens[i+1].Kind != token.PERIOD || tokens[i+2].Kind != token.IDENT {
				continue
			}
			if alias := selectedNames[tokens[i].Text+"."+tokens[i+2].Text]; alias != "" {
				text = text[:tokens[i].Start] + alias + text[tokens[i+2].End:]
			}
		}
	}
	if tokens, err := lexSource("type", []byte(text)); err == nil {
		for i := len(tokens) - 1; i >= 0; i-- {
			if qualifier, ok := qualifierNames[tokens[i].Text]; ok && tokens[i].Kind == token.IDENT {
				text = text[:tokens[i].Start] + qualifier + text[tokens[i].End:]
			}
		}
	}
	// Internal runtime representations do not belong in the source API.
	if strings.Contains(text, "ghi_") || strings.Contains(text, "ghiData_") || strings.Contains(text, "Ghi") || strings.Contains(text, generatedModule) {
		return ""
	}
	return text
}
