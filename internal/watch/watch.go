// Package watch rebuilds a project and replaces its development process only
// after successful compilation. It does not deploy or supervise production.
package watch

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"ghi/internal/compiler"
	"ghi/internal/proctree"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

type Options struct {
	Dir                string
	Args               []string
	Debug              bool
	Stdin              io.Reader
	Stdout, Stderr     io.Writer
	Interval, Debounce time.Duration
}

type child struct {
	command *exec.Cmd
	done    chan error
	control proctree.Control
}

func (c *child) stop() {
	c.control.Signal()
	select {
	case <-c.done:
	case <-time.After(time.Second):
		c.control.Kill()
		<-c.done
	}
	c.control.Release()
}

func startChild(o Options, path string) (*child, error) {
	cmd := exec.Command(path, o.Args...)
	cmd.WaitDelay = time.Second
	cmd.Dir = o.Dir
	cmd.Stdin = o.Stdin
	cmd.Stdout = o.Stdout
	cmd.Stderr = o.Stderr
	control, err := proctree.Start(cmd)
	if err != nil {
		return nil, err
	}
	c := &child{cmd, make(chan error, 1), control}
	go func() { c.done <- cmd.Wait() }()
	return c, nil
}

// Snapshot hashes source/configuration content, so atomic saves and same-size,
// same-mtime edits are detected. Outputs, databases and tests do not trigger a
// restart. Installed package source is included separately from build caches.
func snapshot(root string) (string, error) {
	h := sha256.New()
	scan := func(base string) error {
		return filepath.WalkDir(base, func(path string, e fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if e.IsDir() {
				if path != base && (strings.HasPrefix(e.Name(), ".") || e.Name() == "bin" || e.Name() == "vendor" || e.Name() == "node_modules" || path == filepath.Join(base, "tests")) {
					return filepath.SkipDir
				}
				return nil
			}
			if e.Type()&os.ModeSymlink != 0 {
				return nil
			}
			switch filepath.Ext(path) {
			case ".ghi", ".go", ".json", ".lock", ".sql", ".mod", ".sum":
			default:
				return nil
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			fmt.Fprintf(h, "%s\x00%x\n", path, sha256.Sum256(data))
			return nil
		})
	}
	if err := scan(root); err != nil {
		return "", err
	}
	packages := filepath.Join(root, ".ghi", "packages")
	if _, err := os.Stat(packages); err == nil {
		entries, err := os.ReadDir(packages)
		if err != nil {
			return "", err
		}
		for _, entry := range entries {
			if entry.IsDir() && !strings.HasPrefix(entry.Name(), ".") {
				if err := scan(filepath.Join(packages, entry.Name())); err != nil {
					return "", err
				}
			}
		}
	} else if !os.IsNotExist(err) {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func Run(ctx context.Context, o Options) error {
	if o.Stdout == nil {
		o.Stdout = io.Discard
	}
	if o.Stderr == nil {
		o.Stderr = io.Discard
	}
	if o.Interval <= 0 {
		o.Interval = 250 * time.Millisecond
	}
	if o.Debounce <= 0 {
		o.Debounce = 200 * time.Millisecond
	}
	root, err := filepath.Abs(o.Dir)
	if err != nil {
		return err
	}
	o.Dir = root
	info, err := os.Stat(root)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("watch project path must be a directory: %s", root)
	}
	observed, err := snapshot(root)
	if err != nil {
		return err
	}
	staging, err := os.MkdirTemp("", "ghi-watch-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(staging)
	var running *child
	defer func() {
		if running != nil {
			running.stop()
		}
	}()
	type buildResult struct {
		path       string
		err        error
		source     string
		generation uint64
	}
	var builds chan buildResult
	var cancelBuild context.CancelFunc
	defer func() {
		if cancelBuild != nil {
			cancelBuild()
			<-builds
		}
	}()
	sequence := 0
	var generation uint64
	pending := true
	lastChange := time.Now().Add(-o.Debounce)
	startBuild := func() {
		pending = false
		sequence++
		path := filepath.Join(staging, fmt.Sprintf("app-%d", sequence))
		if runtime.GOOS == "windows" {
			path += ".exe"
		}
		buildCtx, cancel := context.WithCancel(ctx)
		cancelBuild = cancel
		builds = make(chan buildResult, 1)
		source := observed
		startedGeneration := generation
		fmt.Fprintln(o.Stderr, "[watch] building")
		go func() {
			_, err := compiler.Build(buildCtx, compiler.Options{Dir: root, Output: path, Debug: o.Debug})
			builds <- buildResult{path, err, source, startedGeneration}
		}()
	}
	ticker := time.NewTicker(o.Interval)
	defer ticker.Stop()
	fmt.Fprintln(o.Stderr, "[watch] watching", root)
	for {
		if pending && builds == nil && time.Since(lastChange) >= o.Debounce {
			startBuild()
		}
		var exited <-chan error
		if running != nil {
			exited = running.done
		}
		select {
		case <-ctx.Done():
			return nil
		case err := <-exited:
			running.control.Release()
			_ = os.Remove(running.command.Path)
			running = nil
			fmt.Fprintf(o.Stderr, "[watch] process exited (%v); waiting for changes\n", err)
		case result := <-builds:
			cancelBuild()
			cancelBuild = nil
			builds = nil
			// Include edits that happened during compilation before its next poll.
			current, scanErr := snapshot(root)
			if scanErr != nil {
				fmt.Fprintln(o.Stderr, "[watch] scan:", scanErr)
				pending = true
				lastChange = time.Now()
				_ = os.Remove(result.path)
				continue
			}
			if current != result.source || generation != result.generation {
				observed = current
				pending = true
				lastChange = time.Now()
				_ = os.Remove(result.path)
				continue
			}
			if result.err != nil {
				fmt.Fprintln(o.Stderr, "[watch] build failed:", result.err)
				continue
			}
			pending = false
			if running != nil {
				old := running.command.Path
				running.stop()
				running = nil
				_ = os.Remove(old)
			}
			running, err = startChild(o, result.path)
			if err != nil {
				fmt.Fprintln(o.Stderr, "[watch] start failed:", err)
				continue
			}
			fmt.Fprintln(o.Stderr, "[watch] started")
		case <-ticker.C:
			current, err := snapshot(root)
			if err != nil {
				fmt.Fprintln(o.Stderr, "[watch] scan:", err)
				continue
			}
			if current != observed {
				generation++
				observed = current
				pending = true
				lastChange = time.Now()
			}
		}
	}
}
