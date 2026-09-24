package language_test

import (
	"bytes"
	"context"
	"ghi/internal/compiler"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func importProject(t *testing.T, main string) string {
	t.Helper()
	dir := t.TempDir()
	for name, source := range map[string]string{
		"main.ghi": "namespace main\n" + main,
		"models/models.ghi": `namespace models
class User { public func name() string { return "Ada" } }
func Label() string { return "models" }
`,
		"config/config.ghi": `namespace infrastructure.config
func Load() int { return 42 }
`,
		"httpapi/httpapi.ghi": `namespace presentation.httpapi
func Serve() string { return "http" }
`,
	} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0700); err != nil {
			t.Fatal(err)
		}
		writePackageTestFile(t, dir, name, source)
	}
	return dir
}

func TestUnifiedNamespaceImports(t *testing.T) {
	dir := importProject(t, `import models
import models.User
import infrastructure.config as cfg
import presentation.httpapi
import infrastructure.config as old
import fmt "go:fmt"
class Consumer {
 public func value() int { return cfg.Load() }
}
func shadow(cfg string) string { return cfg }
func main() {
 user := new User()
 other := new models.User()
 fmt.Println(models.Label(), user.name(), other.name(), cfg.Load(), old.Load(), httpapi.Serve(), new Consumer().value(), shadow("local"))
}
`)
	path := filepath.Join(dir, "main.ghi")
	source, _ := os.ReadFile(path)
	formatted, err := compiler.FormatSource(path, source)
	if err != nil {
		t.Fatal(err)
	}
	again, err := compiler.FormatSource(path, formatted)
	if err != nil || !bytes.Equal(formatted, again) {
		t.Fatalf("format not stable: %v", err)
	}
	if err := os.WriteFile(path, formatted, 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	result, err := compiler.Build(ctx, compiler.Options{Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	output, err := exec.CommandContext(ctx, result.Executable).CombinedOutput()
	if err != nil || strings.TrimSpace(string(output)) != "models Ada Ada 42 42 http 42 local" {
		t.Fatalf("run: %v\n%s", err, output)
	}
}

func TestUnifiedNamespaceImportErrors(t *testing.T) {
	for name, source := range map[string]string{
		"quoted Ghi alias":          "import cfg \"infrastructure.config\"\nfunc main(){cfg.Load()}",
		"quoted Ghi path":           "import \"models\"\nfunc main(){models.Label()}",
		"grouped quoted Ghi":        "import (cfg \"infrastructure.config\")\nfunc main(){cfg.Load()}",
		"unknown":                   "import missing\nfunc main(){}",
		"duplicate":                 "import models\nimport models\nfunc main(){models.Label()}",
		"type alias collision":      "import models.User as cfg\nimport infrastructure.config as cfg\nfunc main(){cfg.Load()}",
		"namespace alias collision": "import models as cfg\nimport infrastructure.config as cfg\nfunc main(){cfg.Load()}",
		"declaration collision":     "import infrastructure.config as cfg\nfunc cfg(){}\nfunc main(){}",
		"unused":                    "import infrastructure.config\nfunc main(){}",
		"original name hidden":      "import infrastructure.config as cfg\nfunc main(){cfg.Load();config.Load()}",
		"self":                      "import main\nfunc main(){}",
		"invalid alias":             "import models as _\nfunc main(){}",
	} {
		t.Run(name, func(t *testing.T) {
			dir := importProject(t, source)
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			err := compiler.Check(ctx, compiler.Options{Dir: dir})
			if err == nil || !strings.Contains(err.Error(), "main.ghi:") {
				t.Fatalf("expected located diagnostic: %v", err)
			}
			if strings.Contains(name, "quoted") && !strings.Contains(err.Error(), "quoted imports are only for Go") {
				t.Fatalf("expected migration diagnostic: %v", err)
			}
		})
	}
}

func TestUnifiedImportAmbiguity(t *testing.T) {
	dir := importProject(t, "import models.User\nfunc main(){}")
	if err := os.Mkdir(filepath.Join(dir, "ambiguous"), 0700); err != nil {
		t.Fatal(err)
	}
	writePackageTestFile(t, dir, "ambiguous/value.ghi", "namespace models.User\nfunc Name() string {return \"namespace\"}\n")
	err := compiler.Check(context.Background(), compiler.Options{Dir: dir})
	if err == nil || !strings.Contains(err.Error(), "ambiguous import models.User") {
		t.Fatalf("expected ambiguity diagnostic: %v", err)
	}
}
