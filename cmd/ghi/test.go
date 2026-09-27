package main

import (
	"context"
	"flag"
	"fmt"
	"ghi/internal/compiler"
	"ghi/internal/watch"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func runTests(args []string) int {
	flags := flag.NewFlagSet("test", flag.ContinueOnError)
	filter := flags.String("run", "", "test name regular expression")
	timeout := flags.Duration("timeout", time.Minute, "maximum duration per test suite")
	watching := flags.Bool("watch", false, "rerun tests after source or test changes")
	verbose := flags.Bool("v", false, "show passing tests")
	cover := flags.Bool("cover", false, "report Ghi source statement coverage")
	profile := flags.String("coverprofile", "", "write Ghi coverage profile (implies --cover)")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() > 1 {
		fmt.Fprintln(os.Stderr, "expected one project directory")
		return 2
	}
	if *timeout <= 0 {
		fmt.Fprintln(os.Stderr, "timeout must be positive")
		return 2
	}
	dir := "."
	if flags.NArg() == 1 {
		dir = flags.Arg(0)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if *watching {
		if err := watch.Tests(ctx, watch.TestOptions{Dir: dir, Filter: *filter, Timeout: *timeout, Verbose: *verbose, Cover: *cover, CoverProfile: *profile, Log: os.Stdout}); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		return 0
	}
	if err := compiler.Test(ctx, compiler.TestOptions{Dir: dir, Run: *filter, Timeout: *timeout, Verbose: *verbose, Cover: *cover, CoverProfile: *profile, Log: os.Stdout}); err != nil {
		fmt.Fprintln(os.Stderr, compiler.FormatDiagnostic(err, dir, nil))
		return 1
	}
	return 0
}
