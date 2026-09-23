package compiler

import (
	"fmt"
	"go/ast"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type sourceFile struct {
	Path string
	Tree *ast.File
	Unit *unit
}

type namespace struct {
	Name    string
	Dir     string
	GoName  string
	Files   []*sourceFile
	Imports []string
}

type program struct {
	Root                string
	Fset                *token.FileSet
	Namespaces          map[string]*namespace
	Ordered             []*namespace
	Runtime             *namespace
	Wrapped             map[*ast.CallExpr]bool
	Helpers             map[int]bool
	CheckedDereferences map[*ast.StarExpr]bool
	LoweredReceives     map[*ast.UnaryExpr]bool
	ReceiveID           int
}

func loadProject(root string) (*program, error) {
	p := &program{Root: root, Fset: token.NewFileSet(), Namespaces: map[string]*namespace{}}
	directories := map[string]string{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != root && (strings.HasPrefix(d.Name(), ".") || d.Name() == "bin" || d.Name() == "vendor" || d.Name() == "node_modules") {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".ghi" {
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("source symlinks are not supported: %s", path)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		name, tree, unit, err := parseFile(p.Fset, path, data)
		if err != nil {
			return err
		}
		dir := filepath.Dir(path)
		if previous, ok := directories[dir]; ok && previous != name {
			return fmt.Errorf("%s: one directory cannot declare both %q and %q", path, previous, name)
		}
		directories[dir] = name
		ns, ok := p.Namespaces[name]
		if ok && ns.Dir != dir {
			return fmt.Errorf("namespace %q is declared in multiple directories", name)
		}
		if !ok {
			ns = &namespace{Name: name, Dir: dir, GoName: tree.Name.Name}
			p.Namespaces[name] = ns
			p.Ordered = append(p.Ordered, ns)
		}
		source := &sourceFile{Path: path, Tree: tree, Unit: unit}
		for _, class := range unit.Classes {
			class.File = source
			class.Namespace = ns
		}
		ns.Files = append(ns.Files, source)
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(p.Namespaces) == 0 {
		return nil, fmt.Errorf("no .ghi source files found in %s", root)
	}
	main, ok := p.Namespaces["main"]
	if !ok || main.Dir != root {
		return nil, fmt.Errorf("project root must declare namespace main")
	}
	sort.Slice(p.Ordered, func(i, j int) bool { return p.Ordered[i].Name < p.Ordered[j].Name })
	return p, nil
}
