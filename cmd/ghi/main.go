package main

import (
	"context"
	"flag"
	"fmt"
	"ghi/internal/compiler"
	"ghi/internal/toolchain"
	"os"
	"os/exec"
	"os/signal"
)

var version = "0.1.0-dev"

func main() { os.Exit(run(os.Args[1:])) }

func run(args []string) int {
	if len(args) > 0 && args[0] == "setup" {
		flags := flag.NewFlagSet("setup", flag.ContinueOnError)
		managed := flags.Bool("managed", false, "install a managed Go toolchain even if Go is on PATH")
		if err := flags.Parse(args[1:]); err != nil {
			return 2
		}
		if flags.NArg() != 0 {
			fmt.Fprintln(os.Stderr, "unexpected setup arguments")
			return 2
		}
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
		defer stop()
		path, err := (toolchain.Manager{Log: os.Stderr, SkipSystem: *managed}).Ensure(ctx)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		fmt.Println("Go toolchain:", path)
		return 0
	}
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" {
		fmt.Println("Ghi – Go, Hierarchy, Interfaces\n\nUsage:\n  ghi check [project-directory]\n  ghi build [-o executable] [project-directory]\n  ghi run [project-directory] [-- program-arguments...]\n  ghi setup [--managed]\n  ghi version")
		return 0
	}
	if args[0] == "version" {
		fmt.Println("ghi " + version)
		return 0
	}
	if args[0] == "check" {
		flags := flag.NewFlagSet("check", flag.ContinueOnError)
		if err := flags.Parse(args[1:]); err != nil {
			return 2
		}
		if flags.NArg() > 1 {
			fmt.Fprintln(os.Stderr, "expected one project directory")
			return 2
		}
		for _, arg := range args[1:] {
			if arg == "--" {
				fmt.Fprintln(os.Stderr, "program arguments require ghi run")
				return 2
			}
		}
		dir := "."
		if flags.NArg() == 1 {
			dir = flags.Arg(0)
		}
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
		defer stop()
		if err := compiler.Check(ctx, compiler.Options{Dir: dir, Log: os.Stderr}); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		fmt.Println("Check passed")
		return 0
	}
	if args[0] != "build" && args[0] != "run" {
		fmt.Fprintln(os.Stderr, "unknown command:", args[0])
		return 2
	}
	flags := flag.NewFlagSet(args[0], flag.ContinueOnError)
	output := flags.String("o", "", "output executable path")
	var compilerArgs, programArgs []string
	compilerArgs = args[1:]
	for i, arg := range compilerArgs {
		if arg == "--" {
			programArgs = compilerArgs[i+1:]
			compilerArgs = compilerArgs[:i]
			break
		}
	}
	if err := flags.Parse(compilerArgs); err != nil {
		return 2
	}
	if flags.NArg() > 1 {
		fmt.Fprintln(os.Stderr, "expected one project directory")
		return 2
	}
	if args[0] == "build" && len(programArgs) > 0 {
		fmt.Fprintln(os.Stderr, "program arguments require ghi run")
		return 2
	}
	dir := "."
	if flags.NArg() == 1 {
		dir = flags.Arg(0)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	result, err := compiler.Build(ctx, compiler.Options{Dir: dir, Output: *output, Log: os.Stderr})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if args[0] == "build" {
		fmt.Println(result.Executable)
		return 0
	}
	command := exec.CommandContext(ctx, result.Executable, programArgs...)
	command.Dir = dir
	command.Stdin, command.Stdout, command.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := command.Run(); err != nil {
		if exit, ok := err.(*exec.ExitError); ok {
			return exit.ExitCode()
		}
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}
