package main

import (
	"context"
	"flag"
	"fmt"
	"ghi/internal/compiler"
	"os"
	"os/signal"
	"time"
)

func runTests(args []string) int {
	flags := flag.NewFlagSet("test", flag.ContinueOnError)
	filter := flags.String("run", "", "test name regular expression")
	timeout := flags.Duration("timeout", time.Minute, "maximum duration per test suite")
	verbose := flags.Bool("v", false, "show passing tests")
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
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := compiler.Test(ctx, compiler.TestOptions{Dir: dir, Run: *filter, Timeout: *timeout, Verbose: *verbose, Log: os.Stdout}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}
