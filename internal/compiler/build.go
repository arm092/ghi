package compiler

import (
	"context"
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
	Dir    string
	Output string
	Log    io.Writer
}

type Result struct{ Executable string }

func Build(ctx context.Context, options Options) (Result, error) {
	root := options.Dir
	if root == "" {
		root = "."
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return Result{}, err
	}
	info, err := os.Stat(root)
	if err != nil {
		return Result{}, err
	}
	if !info.IsDir() {
		return Result{}, fmt.Errorf("project path must be a directory: %s", root)
	}
	p, err := loadProject(root)
	if err != nil {
		return Result{}, err
	}
	workspace, err := os.MkdirTemp("", "ghi-build-")
	if err != nil {
		return Result{}, err
	}
	defer os.RemoveAll(workspace)
	goPath, err := (toolchain.Manager{Log: options.Log}).Ensure(ctx)
	if err != nil {
		return Result{}, err
	}
	if err := p.generate(ctx, workspace, goPath); err != nil {
		return Result{}, err
	}
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
	command := exec.CommandContext(ctx, goPath, "build", "-mod=readonly", "-o", staging, ".")
	command.Dir = workspace
	command.Env = toolchain.Env()
	log, err := command.CombinedOutput()
	if err != nil {
		return Result{}, fmt.Errorf("Go build failed: %w\n%s", err, log)
	}
	if err := os.Rename(staging, output); err != nil {
		return Result{}, fmt.Errorf("install executable: %w", err)
	}
	return Result{Executable: output}, nil
}
