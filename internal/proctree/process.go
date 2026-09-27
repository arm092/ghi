// Package proctree owns command descendants as well as the direct child.
package proctree

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"time"
)

type Control struct{ Signal, Kill, Release func() }

// Start must receive exec.Command, not exec.CommandContext: cancellation is
// handled by Run or the caller after the process tree has been attached.
func Start(cmd *exec.Cmd) (Control, error) {
	configureProcess(cmd)
	if err := cmd.Start(); err != nil {
		return Control{}, err
	}
	control, err := attachProcess(cmd.Process)
	if err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return Control{}, err
	}
	return control, nil
}

func Run(ctx context.Context, cmd *exec.Cmd) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	// Bound waiting on inherited pipes even if a child explicitly detaches.
	cmd.WaitDelay = time.Second
	// Ordinary synchronous compilation has no cancellation signal. Avoid job
	// setup and thread enumeration for those short-lived Go commands; watch and
	// other cancellable callers always use the controlled tree below.
	if ctx.Done() == nil {
		return cmd.Run()
	}
	control, err := Start(cmd)
	if err != nil {
		return err
	}
	defer control.Release()
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		control.Kill()
		<-done
		return ctx.Err()
	}
}

func CombinedOutput(ctx context.Context, cmd *exec.Cmd) ([]byte, error) {
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &output
	err := Run(ctx, cmd)
	return output.Bytes(), err
}

func Output(ctx context.Context, cmd *exec.Cmd) ([]byte, error) {
	var output, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &stderr
	err := Run(ctx, cmd)
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		exit.Stderr = stderr.Bytes()
	}
	return output.Bytes(), err
}
