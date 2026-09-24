package compiler

import (
	"context"
	"encoding/json"
	"fmt"
	"ghi/internal/toolchain"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

type Options struct {
	// Overlay replaces existing project source contents in memory.
	Overlay map[string][]byte
	Dir     string
	Output  string
	Log     io.Writer
	Debug   bool
}

type Result struct{ Executable string }

func Build(ctx context.Context, options Options) (Result, error) {
	prepared, err := prepareProject(ctx, options)
	if err != nil {
		return Result{}, err
	}
	defer prepared.close()
	root, workspace, goPath := prepared.program.Root, prepared.workspace, prepared.goPath
	output := options.Output
	if output == "" {
		name := filepath.Base(root)
		if runtime.GOOS == "windows" {
			name += ".exe"
		}
		output = filepath.Join(root, "bin", name)
	}
	output, err = filepath.Abs(output)
	if err != nil {
		return Result{}, err
	}
	if strings.EqualFold(filepath.Ext(output), ".ghi") {
		return Result{}, fmt.Errorf("output must not overwrite a .ghi source file")
	}
	if err := os.MkdirAll(filepath.Dir(output), 0755); err != nil {
		return Result{}, err
	}
	candidate, err := os.CreateTemp(filepath.Dir(output), ".ghi-output-*")
	if err != nil {
		return Result{}, err
	}
	staging := candidate.Name()
	if err := candidate.Close(); err != nil {
		return Result{}, err
	}
	defer os.Remove(staging)
	args := []string{"build", "-mod=readonly"}
	if options.Debug {
		args = append(args, "-gcflags=all=-N -l")
	}
	args = append(args, "-o", staging, ".")
	command := exec.CommandContext(ctx, goPath, args...)
	command.Dir = workspace
	command.Env = toolchain.Env()
	log, err := command.CombinedOutput()
	if err != nil {
		return Result{}, fmt.Errorf("Go build failed: %w\n%s", err, log)
	}
	if err := os.Rename(staging, output); err != nil {
		return Result{}, fmt.Errorf("install executable: %w", err)
	}
	if options.Debug {
		metadata, err := json.MarshalIndent(prepared.program.debugMetadata(), "", "  ")
		if err != nil {
			return Result{}, fmt.Errorf("debug metadata: %w", err)
		}
		if err := os.WriteFile(output+".ghi-debug.json", append(metadata, '\n'), 0644); err != nil {
			return Result{}, fmt.Errorf("write debug metadata: %w", err)
		}
	}
	return Result{Executable: output}, nil
}
