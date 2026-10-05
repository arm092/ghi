package cli_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"ghi/internal/compiler"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestExpressionAnalysisOriginalBytesAndOverlay(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "main.ghi")
	disk := []byte("namespace main\nfunc main() {}\n")
	if err := os.WriteFile(file, disk, 0600); err != nil {
		t.Fatal(err)
	}
	source := []byte("\ufeffnamespace main\r\n// Ղ original bytes\r\nfunc twice(x int) int { return x * 2 }\r\nfunc main() {\r\n text := `Ղ`\r\n a := twice(21)\r\n b := a + 1\r\n _ = text; _ = b\r\n}\r\n")
	result := compiler.AnalyzeExpressionTypes(context.Background(), root, file, source)
	if len(result.Diagnostics) != 0 || !hasTypeCapability(result) {
		t.Fatalf("analysis: %+v", result)
	}
	if result.Filename != file || result.SHA256 != fmt.Sprintf("%x", sha256.Sum256(source)) {
		t.Fatalf("buffer identity: %+v", result)
	}
	want := map[string]string{"twice(21)": "int", "a + 1": "int", "`Ղ`": "string"}
	for _, item := range result.ExpressionTypes {
		if item.Start < 0 || item.End <= item.Start || item.End > len(source) {
			t.Fatalf("bad span: %+v", item)
		}
		text := string(source[item.Start:item.End])
		if typ, ok := want[text]; ok {
			if typ != item.Type {
				t.Fatalf("%s: got %s, want %s", text, item.Type, typ)
			}
			delete(want, text)
		}
		if strings.Contains(item.Type, "ghi") || strings.Contains(item.Type, "Ghi") {
			t.Fatalf("lowering type leaked: %+v", item)
		}
	}
	if len(want) != 0 {
		t.Fatalf("missing expressions: %v; got %+v", want, result.ExpressionTypes)
	}
	actual, err := os.ReadFile(file)
	if err != nil || !bytes.Equal(actual, disk) {
		t.Fatal("overlay changed source")
	}
	if _, err := os.Stat(filepath.Join(root, ".ghi")); !os.IsNotExist(err) {
		t.Fatal("analysis populated project cache")
	}
	for _, name := range []string{"go.mod", "go.sum", "mojave.json", "mojave.lock"} {
		if _, err := os.Stat(filepath.Join(root, name)); !os.IsNotExist(err) {
			t.Fatalf("analysis created %s", name)
		}
	}
}

func hasTypeCapability(result compiler.SyntaxAnalysis) bool {
	for _, capability := range result.Capabilities {
		if capability == "expressionTypes" {
			return true
		}
	}
	return false
}

func TestExpressionAnalysisGhiTypesAndSurfaceRewrites(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "model"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "model", "box.ghi"), []byte("namespace model\nclass Box { public value int\n constructor(value int) { this.value = value }\n}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(root, "main.ghi")
	source := []byte("namespace main\nimport model.Box as Alias\nfunc identity(value ?Alias) ?Alias { return value }\nfunc main() {\n value := identity(nil)\n _ = value\n condition := true\n chosen := condition ? 4 : 5\n _ = chosen\n}\n")
	if err := os.WriteFile(file, source, 0600); err != nil {
		t.Fatal(err)
	}
	result := compiler.AnalyzeExpressionTypes(context.Background(), root, file, source)
	if len(result.Diagnostics) != 0 || !hasTypeCapability(result) {
		t.Fatalf("analysis: %+v", result)
	}
	foundAlias, foundAfterRewrite, foundTernary := false, false, false
	for _, item := range result.ExpressionTypes {
		text := string(source[item.Start:item.End])
		if text == "identity(nil)" {
			foundAlias = item.Type == "?Alias"
		}
		if text == "chosen" {
			foundAfterRewrite = item.Type == "int"
		}
		if text == "condition ? 4 : 5" {
			foundTernary = item.Type == "int"
		}
	}
	if !foundAlias || !foundAfterRewrite || !foundTernary {
		t.Fatalf("missing Ghi types: %+v", result.ExpressionTypes)
	}
	qualified := []byte("namespace main\nimport model\nfunc identity(value ?model.Box) ?model.Box { return value }\nfunc main() { value := identity(nil); _ = value }\n")
	result = compiler.AnalyzeExpressionTypes(context.Background(), root, file, qualified)
	if len(result.Diagnostics) != 0 {
		t.Fatalf("qualified analysis: %+v", result)
	}
	foundQualified := false
	for _, item := range result.ExpressionTypes {
		if string(qualified[item.Start:item.End]) == "identity(nil)" && item.Type == "?model.Box" {
			foundQualified = true
		}
	}
	if !foundQualified {
		t.Fatalf("native pointer syntax leaked for qualified nullable class: %+v", result.ExpressionTypes)
	}
}

func TestExpressionAnalysisFailureClearsCapabilities(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "main.ghi")
	if err := os.WriteFile(file, []byte("namespace main\nfunc main() {}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, source := range []string{"namespace main\nfunc main() { value := missing; _ = value }\n", "namespace main\nfunc main() {\n"} {
		result := compiler.AnalyzeExpressionTypes(context.Background(), root, file, []byte(source))
		if len(result.Diagnostics) == 0 || len(result.Capabilities) != 0 || len(result.ExpressionTypes) != 0 || len(result.Tokens) != 0 {
			t.Fatalf("partial failed result: %+v", result)
		}
		if result.Filename != file || result.SHA256 != fmt.Sprintf("%x", sha256.Sum256([]byte(source))) {
			t.Fatal("failure lost source identity")
		}
	}
	result := compiler.AnalyzeExpressionTypes(context.Background(), root, filepath.Join(root, "unsaved.ghi"), []byte("namespace main\nfunc main() {}\n"))
	if len(result.Diagnostics) == 0 || hasTypeCapability(result) {
		t.Fatalf("nonexistent overlay accepted: %+v", result)
	}
}

func TestExpressionAnalysisSelectedTypeAliasDoesNotRenamePrefix(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "model"), 0700); err != nil {
		t.Fatal(err)
	}
	model := []byte("namespace model\nclass Box { public value int; constructor(value int) { this.value = value } }\ntype Boxed struct { Value int }\n")
	if err := os.WriteFile(filepath.Join(root, "model", "types.ghi"), model, 0600); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(root, "main.ghi")
	source := []byte("namespace main\nimport model.Box as Alias\nimport model\nfunc use(value ?Alias) ?Alias { return value }\nfunc main() { value := model.Boxed{Value: 1}; _ = value; _ = use(nil) }\n")
	if err := os.WriteFile(file, source, 0600); err != nil {
		t.Fatal(err)
	}
	result := compiler.AnalyzeExpressionTypes(context.Background(), root, file, source)
	if len(result.Diagnostics) != 0 {
		t.Fatalf("analysis: %+v", result)
	}
	foundBoxed, foundAlias := false, false
	for _, item := range result.ExpressionTypes {
		text := string(source[item.Start:item.End])
		if strings.Contains(item.Type, "Aliased") {
			t.Fatalf("invented type from prefix substitution: %+v", item)
		}
		if text == "value" && item.Type == "model.Boxed" {
			foundBoxed = true
		}
		if text == "use(nil)" && item.Type == "?Alias" {
			foundAlias = true
		}
	}
	if !foundBoxed || !foundAlias {
		t.Fatalf("wrong selected/native namespace types: %+v", result.ExpressionTypes)
	}
}

func TestExpressionAnalysisSelectedAliasesUseNamespaceIdentity(t *testing.T) {
	root := t.TempDir()
	for _, prefix := range []string{"first", "second"} {
		dir := filepath.Join(root, prefix, "model")
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "box.ghi"), []byte("namespace "+prefix+".model\nclass Box { constructor() {} }\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	file := filepath.Join(root, "main.ghi")
	source := []byte("namespace main\nimport first.model.Box as First\nimport second.model.Box as Second\nfunc both() (?First, ?Second) { return nil, nil }\nfunc main() { a,b := both(); _ = a; _ = b }\n")
	if err := os.WriteFile(file, source, 0600); err != nil {
		t.Fatal(err)
	}
	result := compiler.AnalyzeExpressionTypes(context.Background(), root, file, source)
	if len(result.Diagnostics) != 0 {
		t.Fatalf("analysis: %+v", result)
	}
	found := false
	for _, item := range result.ExpressionTypes {
		if string(source[item.Start:item.End]) == "both()" && item.Type == "(?First, ?Second)" {
			found = true
		}
	}
	if !found {
		t.Fatalf("same Go package name collapsed distinct source types: %+v", result.ExpressionTypes)
	}
}

func TestExpressionAnalysisNullableClassDoesNotRenameUnicodePointerPrefix(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "main.ghi")
	source := []byte("namespace main\nclass Box { constructor() {} }\ntype BoxՂ struct { Value int }\nfunc use(nullable ?Box, native *BoxՂ) {}\nfunc main() { f := use; _ = f }\n")
	if err := os.WriteFile(file, source, 0600); err != nil {
		t.Fatal(err)
	}
	result := compiler.AnalyzeExpressionTypes(context.Background(), root, file, source)
	if len(result.Diagnostics) != 0 {
		t.Fatalf("analysis: %+v", result)
	}
	found := false
	for _, item := range result.ExpressionTypes {
		text := string(source[item.Start:item.End])
		if strings.Contains(item.Type, "?BoxՂ") {
			t.Fatalf("native Unicode pointer became nullable class: %+v", item)
		}
		if text == "use" && item.Type == "func(nullable ?Box, native *BoxՂ)" {
			found = true
		}
	}
	if !found {
		t.Fatalf("wrong Unicode pointer signature: %+v", result.ExpressionTypes)
	}
}

func TestExpressionAnalysisNativeBridgeAndExplicitTuple(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "main.ghi")
	source := []byte("namespace main\nimport \"go:strconv\"\nfunc pair() (int, string) { return 1, \"value\" }\nfunc wrapped() (int, error) { return 7, nil }\nfunc pointer(value *int) *int { return value }\nfunc main() {\n n := strconv.Atoi(\"42\")\n _ = n\n value := wrapped()\n _ = value\n a,b := pair()\n _ = a; _ = b\n p := pointer(&n)\n _ = p\n}\n")
	if err := os.WriteFile(file, source, 0600); err != nil {
		t.Fatal(err)
	}
	result := compiler.AnalyzeExpressionTypes(context.Background(), root, file, source)
	if len(result.Diagnostics) != 0 {
		t.Fatalf("analysis: %+v", result)
	}
	foundN, foundWrapped, foundPair, foundPointer := false, false, false, false
	for _, item := range result.ExpressionTypes {
		text := string(source[item.Start:item.End])
		if (text == "strconv.Atoi(\"42\")" || text == "wrapped()") && item.Type != "int" {
			t.Fatalf("raw bridged tuple: %+v", item)
		}
		if text == "strconv.Atoi" || text == "wrapped" {
			t.Fatalf("raw bridged function signature: %+v", item)
		}
		if text == "n" && item.Type == "int" {
			foundN = true
		}
		if text == "value" && item.Type == "int" {
			foundWrapped = true
		}
		if text == "pair()" && item.Type == "(int, string)" {
			foundPair = true
		}
		if text == "pointer(&n)" && item.Type == "*int" {
			foundPointer = true
		}
	}
	if !foundN || !foundWrapped || !foundPair || !foundPointer {
		t.Fatalf("lost Ghi tuple or native pointer: %+v", result.ExpressionTypes)
	}
}

func TestExpressionAnalysisClassBodyAndConservativeOrigins(t *testing.T) {
	root := t.TempDir()
	main := filepath.Join(root, "main.ghi")
	if err := os.WriteFile(main, []byte("namespace main\nfunc main() {}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "model"), 0700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(root, "model", "box.ghi")
	source := []byte("namespace model\nclass Box {\n public value int\n constructor(value int) { this.value = value }\n public func twice(value int) int { return value * 2 }\n}\n")
	if err := os.WriteFile(file, source, 0600); err != nil {
		t.Fatal(err)
	}
	result := compiler.AnalyzeExpressionTypes(context.Background(), root, file, source)
	if len(result.Diagnostics) != 0 {
		t.Fatalf("analysis: %+v", result)
	}
	found := false
	for _, item := range result.ExpressionTypes {
		if string(source[item.Start:item.End]) == "value * 2" && item.Type == "int" {
			found = true
		}
	}
	if !found {
		t.Fatalf("class fragment origin omitted: %+v", result.ExpressionTypes)
	}
	directive := []byte("namespace main\n//line " + main + ":1\nfunc main() { value := 1; _ = value }\n")
	result = compiler.AnalyzeExpressionTypes(context.Background(), root, main, directive)
	if len(result.Diagnostics) != 0 || len(result.ExpressionTypes) != 0 {
		t.Fatalf("logical directive became physical editor span: %+v", result)
	}
}

func TestExpressionAnalysisClassFieldReadsRetainExactSpans(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "main.ghi")
	source := []byte("\ufeffnamespace main\r\n// Ղ repeated field reads\r\nclass Node { constructor() {} }\r\nclass Box[T any] {\r\n public value T\r\n private secret int\r\n constructor(value T) { this.value = value }\r\n}\r\nfunc main() {\r\n number := new Box[int](7)\r\n maybe := new Box[?Node](nil)\r\n outer := new Box[Box[int]](number)\r\n _ = number.value\r\n _ = number.value\r\n _ = maybe.value\r\n _ = outer.value.value\r\n number.value = 8\r\n}\r\n")
	if err := os.WriteFile(file, source, 0600); err != nil {
		t.Fatal(err)
	}
	result := compiler.AnalyzeExpressionTypes(context.Background(), root, file, source)
	if len(result.Diagnostics) != 0 {
		t.Fatalf("analysis: %+v", result)
	}
	want := map[[2]int]string{}
	for text, typ := range map[string]string{"_ = number.value": "int", "_ = maybe.value": "?Node", "_ = outer.value.value": "int"} {
		prefix := []byte("_ = ")
		for rest, base := source, 0; ; {
			index := bytes.Index(rest, []byte(text))
			if index < 0 {
				break
			}
			start := base + index + len(prefix)
			want[[2]int{start, base + index + len(text)}] = typ
			base += index + len(text)
			rest = source[base:]
		}
	}
	writeStart := bytes.Index(source, []byte("number.value ="))
	for _, item := range result.ExpressionTypes {
		span := [2]int{item.Start, item.End}
		if typ, ok := want[span]; ok {
			if item.Type != typ {
				t.Fatalf("%s type: %s, want %s", source[item.Start:item.End], item.Type, typ)
			}
			delete(want, span)
		}
		if item.Start == writeStart && item.End == writeStart+len("number.value") {
			t.Fatalf("field write claimed read hover: %+v", item)
		}
	}
	if len(want) != 0 {
		t.Fatalf("missing exact class field read spans: %v; got %+v", want, result.ExpressionTypes)
	}
	broken := bytes.Replace(source, []byte("_ = maybe.value"), []byte("_ = number.secret"), 1)
	failed := compiler.AnalyzeExpressionTypes(context.Background(), root, file, broken)
	if len(failed.Diagnostics) == 0 || len(failed.Capabilities) != 0 || len(failed.ExpressionTypes) != 0 || len(failed.Tokens) != 0 {
		t.Fatalf("private field error exposed partial types: %+v", failed)
	}
	privateStart := bytes.Index(broken, []byte("number.secret"))
	line := bytes.Count(broken[:privateStart], []byte("\n")) + 1
	column := privateStart - bytes.LastIndexByte(broken[:privateStart], '\n')
	wantDiagnostic := fmt.Sprintf("%s:%d:%d: field secret is private", file, line, column)
	if failed.Diagnostics[0].Message != wantDiagnostic {
		t.Fatalf("private field diagnostic lost original position: got %q; want %q", failed.Diagnostics[0].Message, wantDiagnostic)
	}
}

func TestExpressionAnalysisCompleteCallsAndSurfaceExpressions(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "main.ghi")
	source := []byte("\ufeffnamespace main\r\nimport \"go:strconv\"\r\n// Ղ complete expression identities\r\nfunc identity(value int) int { return value }\r\nfunc pair() (int, string, error) { return 7, \"value\", nil }\r\nfunc nothing() {}\r\nfunc main() {\r\n condition := true\r\n _ = strconv.Atoi(\"42\")\r\n a,b := pair(); _ = a; _ = b\r\n _ = condition ? 4 : 5\r\n _ = condition ? 4 : 5\r\n _ = match 1 { 1 => 4, default => 5, }\r\n _ = match 1 { 1 => 4, default => 5, }\r\n _ = identity(condition ? 4 : 5)\r\n _ = identity(match 1 { 1 => 4, default => 5, })\r\n _ = condition ? (condition ? 4 : 5) : 6\r\n _ = match 1 { 1 => match 2 { 2 => 4, default => 5, }, default => 6, }\r\n nothing()\r\n}\r\n")
	source = bytes.Replace(source, []byte("func nothing() {}\r\n"), []byte("func nothing() {}\r\nfunc onlyError() error { return nil }\r\n"), 1)
	source = bytes.Replace(source, []byte(" nothing()\r\n"), []byte(" var cause error\r\n _ = condition ? cause : cause\r\n _ = match 1 { 1 => cause, default => cause, }\r\n onlyError()\r\n nothing()\r\n"), 1)
	if err := os.WriteFile(file, source, 0600); err != nil {
		t.Fatal(err)
	}
	result := compiler.AnalyzeExpressionTypes(context.Background(), root, file, source)
	if len(result.Diagnostics) != 0 {
		t.Fatalf("analysis: %+v", result)
	}
	wanted := map[string]string{
		"strconv.Atoi(\"42\")": "int", "pair()": "(int, string)",
		"condition ? 4 : 5": "int", "match 1 { 1 => 4, default => 5, }": "int",
		"identity(condition ? 4 : 5)": "int", "identity(match 1 { 1 => 4, default => 5, })": "int",
		"condition ? (condition ? 4 : 5) : 6":                               "int",
		"match 1 { 1 => match 2 { 2 => 4, default => 5, }, default => 6, }": "int",
		"match 2 { 2 => 4, default => 5, }":                                 "int",
		"condition ? cause : cause":                                         "error", "match 1 { 1 => cause, default => cause, }": "error",
	}
	for text, typ := range wanted {
		for rest, base := source, 0; ; {
			index := bytes.Index(rest, []byte(text))
			if index < 0 {
				break
			}
			start, end := base+index, base+index+len(text)
			// pair's declaration name is not a value call expression.
			if text == "pair()" && bytes.HasPrefix(source[start:], []byte("pair() (")) {
				base = end
				rest = source[base:]
				continue
			}
			found := false
			for _, item := range result.ExpressionTypes {
				if item.Start == start && item.End == end {
					if item.Type != typ {
						t.Fatalf("%q at %d type %s, want %s", text, start, item.Type, typ)
					}
					found = true
				}
			}
			if !found {
				t.Fatalf("missing full span %q [%d,%d); got %+v", text, start, end, result.ExpressionTypes)
			}
			base = end
			rest = source[base:]
		}
	}
	for _, item := range result.ExpressionTypes {
		if text := string(source[item.Start:item.End]); text == "nothing()" || text == "onlyError()" {
			t.Fatalf("void call exposed value type: %+v", item)
		}
	}
}

func TestExpressionAnalysisClassMethodSignaturesAndCalls(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "model"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "model", "node.ghi"), []byte("namespace model\nclass Node { constructor() {} }\n"), 0600); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(root, "main.ghi")
	source := []byte("namespace main\nimport model.Node as Alias\nclass Box[T any] {\n constructor() {}\n public func echo(value T) T { return value }\n public func nullable(value ?Alias = nil) ?Alias { return value }\n private func hidden(value int) int { return value }\n}\nfunc main() {\n box := new Box[?Alias]()\n _ = box.echo\n _ = box.echo(nil)\n _ = box.nullable\n _ = box.nullable()\n}\n")
	source = bytes.Replace(source, []byte(" private func hidden"), []byte(" public func choose(condition bool) int { return condition ? (match 1 { 1 => 4, default => 5, }) : 6 }\n private func hidden"), 1)
	source = bytes.Replace(source, []byte(" private func hidden"), []byte(" public func result(value ?Alias) (?Alias, error) { return value, nil }\n private func hidden"), 1)
	source = bytes.Replace(source, []byte(" _ = box.nullable()\n"), []byte(" _ = box.nullable()\n _ = box.result\n _ = box.result(nil)\n"), 1)
	source = bytes.Replace(source, []byte("namespace main\n"), []byte("\ufeffnamespace main\n// Ղ class fragments\n"), 1)
	source = bytes.ReplaceAll(source, []byte("\n"), []byte("\r\n"))
	if err := os.WriteFile(file, source, 0600); err != nil {
		t.Fatal(err)
	}
	result := compiler.AnalyzeExpressionTypes(context.Background(), root, file, source)
	if len(result.Diagnostics) != 0 {
		t.Fatalf("analysis: %+v", result)
	}
	wanted := map[string]string{"new Box[?Alias]()": "Box[?Alias]", "box.echo": "func(value ?Alias) ?Alias", "box.echo(nil)": "?Alias", "box.nullable": "func(value ?Alias) ?Alias", "box.nullable()": "?Alias"}
	wanted["condition ? (match 1 { 1 => 4, default => 5, }) : 6"] = "int"
	wanted["match 1 { 1 => 4, default => 5, }"] = "int"
	wanted["box.result"] = "func(value ?Alias) (?Alias, error)"
	wanted["box.result(nil)"] = "?Alias"
	for _, item := range result.ExpressionTypes {
		text := string(source[item.Start:item.End])
		if typ, ok := wanted[text]; ok {
			if item.Type != typ {
				t.Fatalf("%s: %s, want %s", text, item.Type, typ)
			}
			delete(wanted, text)
		}
	}
	if len(wanted) != 0 {
		t.Fatalf("missing signatures/full calls: %v; got %+v", wanted, result.ExpressionTypes)
	}
	callSelectorStart := bytes.Index(source, []byte("box.result(nil)"))
	foundCallSelector := false
	for _, item := range result.ExpressionTypes {
		if item.Start == callSelectorStart && item.End == callSelectorStart+len("box.result") && item.Type == "func(value ?Alias) (?Alias, error)" {
			foundCallSelector = true
		}
	}
	if !foundCallSelector {
		t.Fatalf("bridged class call lost authored method selector signature: %+v", result.ExpressionTypes)
	}
	broken := bytes.Replace(source, []byte("_ = box.echo\r\n"), []byte("_ = box.hidden\r\n"), 1)
	failed := compiler.AnalyzeExpressionTypes(context.Background(), root, file, broken)
	if len(failed.Diagnostics) == 0 || len(failed.Capabilities) != 0 || len(failed.ExpressionTypes) != 0 {
		t.Fatalf("private method exposed partial signature: %+v", failed)
	}
}

func TestExpressionAnalysisCLIContract(t *testing.T) {
	root := t.TempDir()
	binary := filepath.Join(t.TempDir(), "ghi")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	if output, err := exec.Command("go", "build", "-o", binary, "../../cmd/ghi").CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, output)
	}
	file := filepath.Join(root, "main.ghi")
	original := []byte("namespace main\nfunc main() { value := 1; _ = value }\n")
	if err := os.WriteFile(file, original, 0600); err != nil {
		t.Fatal(err)
	}
	for _, stdin := range []bool{false, true} {
		args := []string{"analyze", "--json", "--types", "--project", root}
		if stdin {
			args = append(args, "--stdin", "--filename", file)
		} else {
			args = append(args, file)
		}
		command := exec.Command(binary, args...)
		command.Stdin = strings.NewReader(string(original))
		out, err := command.CombinedOutput()
		var result compiler.SyntaxAnalysis
		if err != nil || json.Unmarshal(out, &result) != nil || !hasTypeCapability(result) {
			t.Fatalf("CLI: %v\n%s", err, out)
		}
	}
	for _, args := range [][]string{
		{"analyze", "--json", "--types", file},
		{"analyze", "--json", "--project", root, file},
		{"analyze", "--json", "--types", "--project", root, "--stdin"},
		{"analyze", "--json", "--types", "--project", root, "--stdin", "--filename", "main.ghi"},
	} {
		out, err := exec.Command(binary, args...).Output()
		exit, ok := err.(*exec.ExitError)
		if !ok || exit.ExitCode() != 2 || len(out) != 0 {
			t.Fatalf("usage: %v => %v %s", args, err, out)
		}
	}
	command := exec.Command(binary, "analyze", "--json", "--types", "--project", root, "--stdin", "--filename", file)
	command.Stdin = strings.NewReader("namespace main\nfunc main() { _ = missing }\n")
	out, err := command.Output()
	exit, ok := err.(*exec.ExitError)
	var result compiler.SyntaxAnalysis
	if !ok || exit.ExitCode() != 1 || json.Unmarshal(out, &result) != nil || len(result.Diagnostics) == 0 || len(result.Capabilities) != 0 {
		t.Fatalf("semantic failure: %v %s", err, out)
	}
}
