package compiler

import (
	"bytes"
	"fmt"
	"go/printer"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const generatedModule = "ghi.generated"

func (p *program) resolveImports() error {
	for _, ns := range p.Ordered {
		for _, file := range ns.Files {
			for _, spec := range file.Tree.Imports {
				path, err := strconv.Unquote(spec.Path.Value)
				if err != nil {
					return err
				}
				if strings.HasPrefix(path, "go:") {
					path = strings.TrimPrefix(path, "go:")
					if path == "" {
						return fmt.Errorf("%s: empty Go import", p.Fset.Position(spec.Pos()))
					}
				} else {
					imported, ok := p.Namespaces[path]
					if !ok {
						return fmt.Errorf("%s: unknown namespace %q; use go: for Go packages", p.Fset.Position(spec.Pos()), path)
					}
					if imported.Name == "main" {
						return fmt.Errorf("%s: importing the entry namespace main is not allowed", p.Fset.Position(spec.Pos()))
					}
					ns.Imports = append(ns.Imports, path)
					path = generatedModule + "/" + strings.ReplaceAll(imported.Name, ".", "/")
				}
				spec.Path.Value = strconv.Quote(path)
			}
		}
	}
	state := map[string]int{}
	var visit func(string) error
	visit = func(name string) error {
		if state[name] == 1 {
			return fmt.Errorf("namespace import cycle involving %s", name)
		}
		if state[name] == 2 {
			return nil
		}
		state[name] = 1
		for _, dependency := range p.Namespaces[name].Imports {
			if err := visit(dependency); err != nil {
				return err
			}
		}
		state[name] = 2
		return nil
	}
	for _, ns := range p.Ordered {
		if err := visit(ns.Name); err != nil {
			return err
		}
	}
	return nil
}

func (p *program) generate(dir string) error {
	if err := p.resolveImports(); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module "+generatedModule+"\n\ngo 1.26.0\n"), 0644); err != nil {
		return err
	}
	for _, ns := range p.Ordered {
		target := dir
		if ns.Name != "main" {
			target = filepath.Join(dir, filepath.FromSlash(strings.ReplaceAll(ns.Name, ".", "/")))
		}
		if err := os.MkdirAll(target, 0755); err != nil {
			return err
		}
		for _, file := range ns.Files {
			var buf bytes.Buffer
			config := printer.Config{Mode: printer.SourcePos | printer.UseSpaces | printer.TabIndent, Tabwidth: 8}
			if err := config.Fprint(&buf, p.Fset, file.Tree); err != nil {
				return err
			}
			name := strings.TrimSuffix(filepath.Base(file.Path), ".ghi") + ".go"
			if strings.HasSuffix(name, "_test.go") {
				return fmt.Errorf("%s: source filenames ending in _test.ghi are reserved", file.Path)
			}
			if !token.IsIdentifier(strings.TrimSuffix(name, ".go")) {
				return fmt.Errorf("%s: source filename must be an identifier", file.Path)
			}
			if err := os.WriteFile(filepath.Join(target, name), buf.Bytes(), 0644); err != nil {
				return err
			}
		}
	}
	return nil
}
