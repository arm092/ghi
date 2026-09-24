package main

import (
	"context"
	"flag"
	"fmt"
	"ghi/internal/compiler"
	"io"
	"os"
	"os/signal"
)

func runFormat(args []string) int {
	flags := flag.NewFlagSet("fmt", flag.ContinueOnError)
	check := flags.Bool("check", false, "report files needing formatting without modifying them")
	stdin := flags.Bool("stdin", false, "format standard input to standard output without changing files")
	filename := flags.String("filename", "", "diagnostic filename for --stdin")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if *stdin {
		if *check || flags.NArg() != 0 {
			fmt.Fprintln(os.Stderr, "--stdin cannot be combined with --check or a project directory")
			return 2
		}
		if *filename == "" {
			*filename = "stdin.ghi"
		}
		source, err := io.ReadAll(os.Stdin)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		formatted, err := compiler.FormatSource(*filename, source)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		if _, err = os.Stdout.Write(formatted); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		return 0
	}
	if *filename != "" {
		fmt.Fprintln(os.Stderr, "--filename requires --stdin")
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
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	changed, err := compiler.FormatProject(ctx, dir, *check)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	for _, path := range changed {
		fmt.Println(path)
	}
	if *check && len(changed) > 0 {
		return 1
	}
	return 0
}
