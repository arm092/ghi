package main

import (
	"context"
	"flag"
	"fmt"
	"ghi/internal/compiler"
	"ghi/internal/toolchain"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"unicode/utf8"
)

var version = "0.2.1-dev"

func main() { os.Exit(run(os.Args[1:])) }

func run(args []string) int {
	if len(args) > 0 && args[0] == "init" {
		return runInit(args[1:])
	}
	if len(args) > 0 && args[0] == "fmt" {
		return runFormat(args[1:])
	}
	if len(args) > 0 && args[0] == "test" {
		return runTests(args[1:])
	}
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
		fmt.Println("Ghi – Go, Hierarchy, Interfaces\n\nUsage:\n  ghi init [directory]\n  ghi check [project-directory]\n  ghi check --stdin --filename /absolute/source.ghi [project-directory]\n  ghi fmt [--check] [project-directory]\n  ghi fmt --stdin [--filename source.ghi]\n  ghi test [-run pattern] [-v] [-timeout 1m] [project-directory]\n  ghi build [--debug] [-o executable] [project-directory]\n  ghi run [--debug] [project-directory] [-- program-arguments...]\n  ghi setup [--managed]\n  ghi version | --version | -v | -V")
		return 0
	}
	if args[0] == "version" || args[0] == "--version" || args[0] == "-v" || args[0] == "-V" {
		if len(args) != 1 {
			fmt.Fprintln(os.Stderr, "version does not accept arguments")
			return 2
		}
		fmt.Println("ghi " + version)
		return 0
	}
	if args[0] == "check" {
		flags := flag.NewFlagSet("check", flag.ContinueOnError)
		stdin := flags.Bool("stdin", false, "check an existing source file using standard input without changing it")
		filename := flags.String("filename", "", "absolute project source filename for --stdin")
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
		options := compiler.Options{Dir: dir, Log: os.Stderr}
		if *stdin {
			if !filepath.IsAbs(*filename) || filepath.Ext(*filename) != ".ghi" {
				fmt.Fprintln(os.Stderr, "--stdin requires --filename with an absolute .ghi path")
				return 2
			}
			source, err := io.ReadAll(os.Stdin)
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				return 1
			}
			if !utf8.Valid(source) {
				fmt.Fprintln(os.Stderr, "standard input must contain UTF-8 source")
				return 1
			}
			options.Overlay = map[string][]byte{filepath.Clean(*filename): source}
		} else if *filename != "" {
			fmt.Fprintln(os.Stderr, "--filename requires --stdin")
			return 2
		}
		if err := compiler.Check(ctx, options); err != nil {
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
	debug := flags.Bool("debug", false, "disable optimization and inlining for debugging")
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
	result, err := compiler.Build(ctx, compiler.Options{Dir: dir, Output: *output, Debug: *debug, Log: os.Stderr})
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
