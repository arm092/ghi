package main

import (
	"context"
	"flag"
	"fmt"
	"ghi/internal/watch"
	"os"
	"os/signal"
	"syscall"
)

func runWatch(args []string) int {
	flags := flag.NewFlagSet("watch", flag.ContinueOnError)
	debug := flags.Bool("debug", false, "disable optimization and inlining")
	var programArgs []string
	for i, arg := range args {
		if arg == "--" {
			programArgs = args[i+1:]
			args = args[:i]
			break
		}
	}
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() > 1 {
		fmt.Fprintln(os.Stderr, "expected one project directory")
		return 2
	}
	dir := "."
	if flags.NArg() == 1 {
		dir = flags.Arg(0)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := watch.Run(ctx, watch.Options{Dir: dir, Args: programArgs, Debug: *debug, Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}
