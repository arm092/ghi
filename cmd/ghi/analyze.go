package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"ghi/internal/compiler"
	"io"
	"os"
	"path/filepath"
)

func runAnalyze(args []string) int {
	flags := flag.NewFlagSet("analyze", flag.ContinueOnError)
	jsonOutput := flags.Bool("json", false, "emit versioned syntax analysis JSON")
	stdin := flags.Bool("stdin", false, "read UTF-8 source from standard input")
	filename := flags.String("filename", "", "diagnostic filename for --stdin")
	types := flags.Bool("types", false, "resolve expression types using the project Go toolchain and dependencies")
	project := flags.String("project", "", "project directory required by --types")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if !*jsonOutput || (*stdin && flags.NArg() != 0) || (!*stdin && (flags.NArg() != 1 || *filename != "")) || (*types != (*project != "")) || (*types && *stdin && !filepath.IsAbs(*filename)) {
		fmt.Fprintln(os.Stderr, "usage: ghi analyze --json [--types --project DIR] source.ghi | ghi analyze --json [--types --project DIR] --stdin [--filename ABS_FILE.ghi]")
		return 2
	}
	var source []byte
	var err error
	if *stdin {
		if *filename == "" {
			*filename = "stdin.ghi"
		}
		source, err = io.ReadAll(os.Stdin)
	} else {
		*filename = flags.Arg(0)
		if filepath.Ext(*filename) != ".ghi" {
			fmt.Fprintln(os.Stderr, "analyze expects one .ghi source file")
			return 2
		}
		source, err = os.ReadFile(*filename)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	var result compiler.SyntaxAnalysis
	if *types {
		result = compiler.AnalyzeExpressionTypes(context.Background(), *project, *filename, source)
	} else {
		result = compiler.AnalyzeSource(*filename, source)
	}
	if err := json.NewEncoder(os.Stdout).Encode(result); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if len(result.Diagnostics) != 0 {
		return 1
	}
	return 0
}
