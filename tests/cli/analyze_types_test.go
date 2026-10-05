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
	foundAlias, foundAfterRewrite := false, false
	for _, item := range result.ExpressionTypes {
		text := string(source[item.Start:item.End])
		if text == "identity(nil)" {
			foundAlias = item.Type == "?Alias"
		}
		if text == "chosen" {
			foundAfterRewrite = item.Type == "int"
		}
		if strings.Contains(text, "condition ?") {
			t.Fatalf("synthetic ternary claimed full source span: %+v", item)
		}
	}
	if !foundAlias || !foundAfterRewrite {
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
	source := []byte("namespace main\nimport \"go:strconv\"\nfunc pair() (int, string) { return 1, \"value\" }\nfunc pointer(value *int) *int { return value }\nfunc main() {\n n := strconv.Atoi(\"42\")\n _ = n\n a,b := pair()\n _ = a; _ = b\n p := pointer(&n)\n _ = p\n}\n")
	if err := os.WriteFile(file, source, 0600); err != nil {
		t.Fatal(err)
	}
	result := compiler.AnalyzeExpressionTypes(context.Background(), root, file, source)
	if len(result.Diagnostics) != 0 {
		t.Fatalf("analysis: %+v", result)
	}
	foundN, foundPair, foundPointer := false, false, false
	for _, item := range result.ExpressionTypes {
		text := string(source[item.Start:item.End])
		if text == "strconv.Atoi(\"42\")" && item.Type != "int" {
			t.Fatalf("raw bridged tuple: %+v", item)
		}
		if text == "strconv.Atoi" {
			t.Fatalf("raw bridged function signature: %+v", item)
		}
		if text == "n" && item.Type == "int" {
			foundN = true
		}
		if text == "pair()" && item.Type == "(int, string)" {
			foundPair = true
		}
		if text == "pointer(&n)" && item.Type == "*int" {
			foundPointer = true
		}
	}
	if !foundN || !foundPair || !foundPointer {
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
