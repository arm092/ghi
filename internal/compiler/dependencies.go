package compiler

import (
	"context"
	"fmt"
	"ghi/internal/proctree"
	"go/version"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"ghi/internal/toolchain"
	"github.com/arm092/mojave/pkg/mojave"
	"golang.org/x/mod/modfile"
)

// stageDependencies keeps the original manifest and lockfile unchanged. Go
// resolves pinned dependencies and verifies their checksums in the build dir.
func (p *program) stageDependencies(ctx context.Context, dir, goPath string) error {
	manifest, sums, managed, err := mojave.GoModuleFiles(p.Root)
	if err != nil {
		return fmt.Errorf("prepare Mojave Go dependencies: %w", err)
	}
	if managed {
		for _, name := range []string{"go.mod", "go.sum"} {
			if _, err := os.Stat(filepath.Join(p.Root, name)); err == nil {
				return fmt.Errorf("Mojave manages Go dependencies: remove the legacy %s after migrating its dependencies to mojave.json", name)
			} else if !os.IsNotExist(err) {
				return err
			}
		}
	} else {
		manifest, err = os.ReadFile(filepath.Join(p.Root, "go.mod"))
		if os.IsNotExist(err) {
			return os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module "+generatedModule+"\n\ngo 1.26.0\n"), 0644)
		}
		if err != nil {
			return fmt.Errorf("read project go.mod: %w", err)
		}
		sums, err = os.ReadFile(filepath.Join(p.Root, "go.sum"))
		if err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("read project go.sum: %w", err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), manifest, 0644); err != nil {
		return err
	}
	if len(sums) > 0 {
		if err := os.WriteFile(filepath.Join(dir, "go.sum"), sums, 0644); err != nil {
			return err
		}
	}
	run := func(args ...string) ([]byte, error) {
		command := exec.Command(goPath, args...)
		command.Dir = dir
		command.Env = toolchain.Env()
		output, err := proctree.CombinedOutput(ctx, command)
		if err != nil {
			return nil, fmt.Errorf("prepare Go dependencies: %w\n%s", err, output)
		}
		return output, nil
	}
	module, err := modfile.Parse("go.mod", manifest, nil)
	if err != nil {
		return fmt.Errorf("read Go module metadata: %w", err)
	}
	moduleGo := ""
	if module.Go != nil {
		moduleGo = module.Go.Version
	}
	compilerVersion := version.Lang(runtime.Version())
	if moduleGo != "" && version.Compare(version.Lang("go"+moduleGo), compilerVersion) > 0 {
		return fmt.Errorf("project requires Go %s; this Ghi compiler supports %s", moduleGo, compilerVersion)
	}
	targetGo := "1.26.0"
	if managed && moduleGo != "" {
		targetGo = moduleGo
	}
	if err := module.AddModuleStmt(generatedModule); err != nil {
		return err
	}
	if err := module.AddGoStmt(targetGo); err != nil {
		return err
	}
	module.DropToolchainStmt()
	for _, replace := range module.Replace {
		if replace.New.Version != "" {
			continue
		}
		replacement := replace.New.Path
		if !filepath.IsAbs(replacement) {
			replacement = filepath.Join(p.Root, filepath.FromSlash(replacement))
		}
		if err := module.AddReplace(replace.Old.Path, replace.Old.Version, filepath.ToSlash(replacement), ""); err != nil {
			return err
		}
	}
	manifest, err = module.Format()
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), manifest, 0644); err != nil {
		return err
	}
	args := []string{"mod", "download"}
	if managed {
		args = append(args, "-json")
	}
	output, err := run(append(args, "all")...)
	if err != nil {
		return err
	}
	if managed {
		p.verification, err = startDependencyVerification(ctx, output, sums)
	}
	return err
}

// Verification only reads downloaded modules; lowering writes a separate build
// tree. Both must finish successfully before generated output is published.
type dependencyVerification struct {
	cancel context.CancelFunc
	done   chan struct{}
	err    error
}

func startDependencyVerification(ctx context.Context, downloads, sums []byte) (*dependencyVerification, error) {
	modules, err := downloadedModules(downloads, sums)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(ctx)
	v := &dependencyVerification{cancel: cancel, done: make(chan struct{})}
	go func() {
		defer close(v.done)
		v.err = verifyDownloadedModules(ctx, modules)
	}()
	return v, nil
}

func (p *program) verifyDependencies() error {
	if p.verification == nil {
		return nil
	}
	<-p.verification.done
	return p.verification.err
}
