package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
)

func runInit(args []string) int {
	flags := flag.NewFlagSet("init", flag.ContinueOnError)
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() > 1 {
		fmt.Fprintln(os.Stderr, "usage: ghi init [directory]")
		return 2
	}
	dir := "."
	if flags.NArg() == 1 {
		dir = flags.Arg(0)
	}
	if err := initializeProject(dir); err != nil {
		fmt.Fprintln(os.Stderr, "ghi init:", err)
		return 1
	}
	fmt.Printf("Created Ghi project in %s. Run ghi run %q to start.\n", dir, dir)
	return 0
}

func initializeProject(dir string) (err error) {
	root, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	files := []struct{ name, content string }{
		{"main.ghi", "namespace main\n\nimport fmt \"go:fmt\"\n\nfunc main() {\n\tfmt.Println(\"Hello from Ghi!\")\n}\n"},
		{"mojave.json", "{\n  \"version\": 1,\n  \"dependencies\": {}\n}\n"},
	}
	for _, file := range files {
		if _, e := os.Lstat(filepath.Join(root, file.name)); e == nil {
			return fmt.Errorf("%s already exists; no files were changed", file.name)
		} else if !os.IsNotExist(e) {
			return e
		}
	}
	tests := filepath.Join(root, "tests")
	if info, e := os.Lstat(tests); e == nil {
		if !info.IsDir() {
			return fmt.Errorf("tests must be a directory, not a file or symlink")
		}
	} else if !os.IsNotExist(e) {
		return e
	}
	for _, file := range []struct{ name, content string }{
		{".gitignore", ".ghi/\n/bin/\n"}, {"tests/.gitkeep", ""},
	} {
		if _, e := os.Lstat(filepath.Join(root, file.name)); os.IsNotExist(e) {
			files = append(files, file)
		} else if e != nil {
			return e
		}
	}
	var createdFiles, createdDirs []string
	defer func() {
		if err != nil {
			for i := len(createdFiles) - 1; i >= 0; i-- {
				_ = os.Remove(createdFiles[i])
			}
			// Remove only newly created empty directories, never existing contents.
			for i := len(createdDirs) - 1; i >= 0; i-- {
				_ = os.Remove(createdDirs[i])
			}
		}
	}()
	for _, path := range []string{root, tests} {
		if _, e := os.Stat(path); os.IsNotExist(e) {
			if err = os.MkdirAll(path, 0755); err != nil {
				return err
			}
			createdDirs = append(createdDirs, path)
		} else if e != nil {
			return e
		}
	}
	for _, file := range files {
		path := filepath.Join(root, file.name)
		f, e := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
		if e != nil {
			return e
		}
		createdFiles = append(createdFiles, path)
		_, writeErr := f.WriteString(file.content)
		closeErr := f.Close()
		if writeErr != nil {
			return writeErr
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return nil
}
