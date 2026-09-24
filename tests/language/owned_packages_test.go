package language_test

import (
	"context"
	"ghi/internal/compiler"
	"ghi/internal/mojave"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Two independently versioned packages with the same basename must retain
// separate classes, imports and source roots throughout installation and build.
func TestOwnerQualifiedPackages(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	project := t.TempDir()
	for _, owner := range []string{"arm092", "someone"} {
		repository := t.TempDir()
		source := "namespace " + owner + ".migrations\nclass Migrator { public func owner() string { return \"" + owner + "\" } }\n"
		writePackageTestFile(t, repository, "migrator.ghi", source)
		// Dependency tests must not enter the consuming application's build.
		if err := os.Mkdir(filepath.Join(repository, "tests"), 0700); err != nil {
			t.Fatal(err)
		}
		writePackageTestFile(t, repository, "tests/invalid.ghi", "this is deliberately not production source")
		for _, args := range [][]string{
			{"init", "-b", "master"}, {"add", "."},
			{"-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "-m", "fixture"},
			{"tag", "v1.0.0"},
		} {
			command := exec.CommandContext(ctx, "git", args...)
			command.Dir = repository
			command.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1")
			if output, err := command.CombinedOutput(); err != nil {
				t.Fatalf("git: %v\n%s", err, output)
			}
		}
		if err := mojave.Add(ctx, project, owner+"/migrations", repository, "v1.0.0"); err != nil {
			t.Fatal(err)
		}
	}
	source := `namespace main
import arm092.migrations.Migrator
import someone.migrations.Migrator as OtherMigrator
func main() {
 first := new Migrator()
 second := new OtherMigrator()
 println(first.owner(), second.owner())
}
`
	writePackageTestFile(t, project, "main.ghi", source)
	lock, err := os.ReadFile(filepath.Join(project, "mojave.lock"))
	if err != nil {
		t.Fatal(err)
	}
	// Only remove the dependency cache in this test-owned temporary directory.
	if err := os.RemoveAll(filepath.Join(project, ".ghi")); err != nil {
		t.Fatal(err)
	}
	if err := mojave.Install(ctx, project); err != nil {
		t.Fatal(err)
	}
	restored, err := os.ReadFile(filepath.Join(project, "mojave.lock"))
	if err != nil || string(lock) != string(restored) {
		t.Fatalf("locked reinstall changed resolution: %v", err)
	}
	result, err := compiler.Build(ctx, compiler.Options{Dir: project})
	if err != nil {
		t.Fatal(err)
	}
	output, err := exec.CommandContext(ctx, result.Executable).CombinedOutput()
	if err != nil || strings.TrimSpace(string(output)) != "arm092 someone" {
		t.Fatalf("run: %v\n%s", err, output)
	}
	writePackageTestFile(t, project, "main.ghi", strings.Replace(source, " as OtherMigrator", "", 1))
	if err := compiler.Check(ctx, compiler.Options{Dir: project}); err == nil || !strings.Contains(err.Error(), "collision") {
		t.Fatalf("unaliased duplicate import: %v", err)
	}
}

func writePackageTestFile(t *testing.T, root, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}
