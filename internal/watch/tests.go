package watch

import (
	"context"
	"fmt"
	"ghi/internal/compiler"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type TestOptions struct {
	Dir, Filter        string
	Timeout            time.Duration
	Verbose            bool
	Cover              bool
	CoverProfile       string
	Race               bool
	Bench, BenchTime   string
	BenchMem           bool
	Count              int
	Log                io.Writer
	Interval, Debounce time.Duration
}

type lockedWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (w *lockedWriter) Write(data []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.w.Write(data)
}

// Tests serializes test runs and coalesces edits made during an active run.
// A failing test or compiler diagnostic leaves the watcher ready for fixes.
func Tests(ctx context.Context, o TestOptions) error {
	if o.Log == nil {
		o.Log = io.Discard
	}
	o.Log = &lockedWriter{w: o.Log}
	if err := compiler.ValidateTestOptions(compiler.TestOptions{Run: o.Filter, Timeout: o.Timeout, CoverProfile: o.CoverProfile, Race: o.Race, Bench: o.Bench, BenchTime: o.BenchTime, BenchMem: o.BenchMem, Count: o.Count}); err != nil {
		return err
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
	info, err := os.Stat(root)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("test watch project path must be a directory: %s", root)
	}
	observed, err := snapshot(root, true)
	if err != nil {
		return err
	}
	var done chan error
	var cancel context.CancelFunc
	defer func() {
		if cancel != nil {
			cancel()
			<-done
		}
	}()
	pending := true
	lastChange := time.Now().Add(-o.Debounce)
	ticker := time.NewTicker(o.Interval)
	defer ticker.Stop()
	fmt.Fprintln(o.Log, "[test-watch] watching", root)
	for {
		if pending && done == nil && time.Since(lastChange) >= o.Debounce {
			pending = false
			runCtx, stopRun := context.WithCancel(ctx)
			cancel = stopRun
			done = make(chan error, 1)
			fmt.Fprintln(o.Log, "[test-watch] running")
			go func(result chan<- error) {
				defer stopRun()
				result <- compiler.Test(runCtx, compiler.TestOptions{Dir: root, Run: o.Filter, Timeout: o.Timeout, Verbose: o.Verbose, Cover: o.Cover, CoverProfile: o.CoverProfile, Race: o.Race, Bench: o.Bench, BenchTime: o.BenchTime, BenchMem: o.BenchMem, Count: o.Count, Log: o.Log})
			}(done)
		}
		select {
		case <-ctx.Done():
			return nil
		case result := <-done:
			cancel()
			cancel, done = nil, nil
			current, scanErr := snapshot(root, true)
			if scanErr != nil {
				fmt.Fprintln(o.Log, "[test-watch] scan:", scanErr)
				pending = true
				lastChange = time.Now()
			} else if current != observed {
				observed, pending = current, true
				lastChange = time.Now()
			}
			if pending {
				fmt.Fprintln(o.Log, "[test-watch] sources changed; running again")
			} else if result != nil {
				fmt.Fprintln(o.Log, "[test-watch] failed:", compiler.FormatDiagnostic(result, root, nil))
			} else {
				fmt.Fprintln(o.Log, "[test-watch] passed")
			}
		case <-ticker.C:
			current, err := snapshot(root, true)
			if err != nil {
				fmt.Fprintln(o.Log, "[test-watch] scan:", err)
				continue
			}
			if current != observed {
				observed, pending = current, true
				lastChange = time.Now()
			}
		}
	}
}
