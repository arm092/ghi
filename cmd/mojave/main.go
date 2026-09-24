package main

import (
	"context"
	"fmt"
	"ghi/internal/mojave"
	"os"
	"os/signal"
	"strings"
)

func run(ctx context.Context, args []string) error {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" {
		fmt.Println("Mojave – Ghi and Go dependencies\nUsage: mojave add OWNER/PACKAGE REPOSITORY [REF]\n       mojave add NAMESPACE REPOSITORY [REF]\n       mojave add go:MODULE[@VERSION]\n       mojave install\n       mojave update\n       mojave remove OWNER/PACKAGE|NAMESPACE|go:MODULE\nRun in the directory containing mojave.json. Git REF defaults to HEAD; Go VERSION defaults to latest.")
		return nil
	}
	root, e := os.Getwd()
	if e != nil {
		return e
	}
	switch args[0] {
	case "add":
		if len(args) == 2 && strings.HasPrefix(args[1], "go:") {
			path, query, _ := strings.Cut(strings.TrimPrefix(args[1], "go:"), "@")
			e = mojave.AddGo(ctx, root, path, query)
			break
		}
		if len(args) != 3 && len(args) != 4 {
			return fmt.Errorf("usage: mojave add OWNER/PACKAGE|NAMESPACE REPOSITORY [REF] or mojave add go:MODULE[@VERSION]")
		}
		ref := "HEAD"
		if len(args) == 4 {
			ref = args[3]
		}
		e = mojave.Add(ctx, root, args[1], args[2], ref)
	case "install":
		if len(args) != 1 {
			return fmt.Errorf("usage: mojave install")
		}
		e = mojave.Install(ctx, root)
	case "update":
		if len(args) != 1 {
			return fmt.Errorf("usage: mojave update")
		}
		e = mojave.Update(ctx, root)
	case "remove":
		if len(args) != 2 {
			return fmt.Errorf("usage: mojave remove OWNER/PACKAGE|NAMESPACE|go:MODULE")
		}
		if strings.HasPrefix(args[1], "go:") {
			e = mojave.RemoveGo(ctx, root, strings.TrimPrefix(args[1], "go:"))
		} else {
			e = mojave.Remove(ctx, root, args[1])
		}
	default:
		return fmt.Errorf("unknown command %q; run mojave help", args[0])
	}
	if e != nil {
		return e
	}
	fmt.Println("Mojave: dependencies ready.")
	return nil
}
func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	if e := run(ctx, os.Args[1:]); e != nil {
		fmt.Fprintln(os.Stderr, "mojave:", e)
		os.Exit(1)
	}
}
