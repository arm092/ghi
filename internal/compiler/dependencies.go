package compiler

import (
	"context"
	"encoding/json"
	"fmt"
	"go/version"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"ghi/internal/toolchain"
)

// stageDependencies keeps the original manifest and lockfile unchanged. Go
// resolves pinned dependencies and verifies their checksums in the build dir.
func (p *program) stageDependencies(ctx context.Context, dir, goPath string) error {
	manifest, err := os.ReadFile(filepath.Join(p.Root, "go.mod"))
	if os.IsNotExist(err) {
		return os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module "+generatedModule+"\n\ngo 1.26.0\n"), 0644)
	}
	if err != nil {
		return fmt.Errorf("read project go.mod: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), manifest, 0644); err != nil {
		return err
	}
	sums, err := os.ReadFile(filepath.Join(p.Root, "go.sum"))
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read project go.sum: %w", err)
	}
	if err == nil {
		if err := os.WriteFile(filepath.Join(dir, "go.sum"), sums, 0644); err != nil {
			return err
		}
	}
	run := func(args ...string) ([]byte, error) {
		command := exec.CommandContext(ctx, goPath, args...)
		command.Dir = dir
		command.Env = toolchain.Env()
		output, err := command.CombinedOutput()
		if err != nil {
			return nil, fmt.Errorf("prepare Go dependencies: %w\n%s", err, output)
		}
		return output, nil
	}
	output, err := run("mod", "edit", "-json")
	if err != nil {
		return err
	}
	type moduleRef struct{ Path, Version string }
	var module struct {
		Go      string
		Replace []struct{ Old, New moduleRef }
	}
	if err := json.Unmarshal(output, &module); err != nil {
		return fmt.Errorf("read Go module metadata: %w", err)
	}
	compilerVersion := version.Lang(runtime.Version())
	if module.Go != "" && version.Compare(version.Lang("go"+module.Go), compilerVersion) > 0 {
		return fmt.Errorf("project requires Go %s; this Ghi compiler supports %s", module.Go, compilerVersion)
	}
	args := []string{"mod", "edit", "-module=" + generatedModule, "-go=1.26.0", "-toolchain=none"}
	for _, replace := range module.Replace {
		if replace.New.Version != "" {
			continue
		}
		replacement := replace.New.Path
		if !filepath.IsAbs(replacement) {
			replacement = filepath.Join(p.Root, filepath.FromSlash(replacement))
		}
		old := replace.Old.Path
		if replace.Old.Version != "" {
			old += "@" + replace.Old.Version
		}
		args = append(args, "-replace="+old+"="+filepath.ToSlash(replacement))
	}
	if _, err := run(args...); err != nil {
		return err
	}
	_, err = run("mod", "download", "all")
	return err
}
