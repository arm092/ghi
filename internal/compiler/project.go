package compiler

import (
	"fmt"
	"github.com/arm092/mojave/pkg/mojave"
	"go/ast"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type sourceFile struct {
	Path   string
	Source []byte
	Tree   *ast.File
	Unit   *unit
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
	TestNamespaces      map[string]bool
}

func loadProject(root string) (*program, error) {
	return loadProjectMode(root, false)
}

func loadProjectMode(root string, testing bool) (*program, error) {
	return loadProjectOverlay(root, testing, nil)
}

func loadProjectOverlay(root string, testing bool, overlay map[string][]byte) (*program, error) {
	remaining := make(map[string]bool, len(overlay))
	for path := range overlay {
		if !filepath.IsAbs(path) || filepath.Clean(path) != path {
			return nil, fmt.Errorf("overlay filename must be an absolute clean path: %s", path)
		}
		remaining[path] = true
	}
	p := &program{Root: root, Fset: token.NewFileSet(), Namespaces: map[string]*namespace{}, TestNamespaces: map[string]bool{}}
	directories := map[string]string{}
	scanRoot, packageNamespace := root, ""
	visit := func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			// Ghi application tests are isolated from production commands.
			if (!testing || packageNamespace != "") && path == filepath.Join(scanRoot, "tests") {
				return filepath.SkipDir
			}
			if path != scanRoot && (strings.HasPrefix(d.Name(), ".") || d.Name() == "bin" || d.Name() == "vendor" || d.Name() == "node_modules") {
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
		data, replaced := overlay[path]
		if replaced && packageNamespace == "" {
			delete(remaining, path)
		} else {
			data, err = os.ReadFile(path)
			if err != nil {
				return err
			}
		}
		name, tree, unit, err := parseFile(p.Fset, path, data)
		if err != nil {
			return err
		}
		if packageNamespace != "" && name != packageNamespace && !strings.HasPrefix(name, packageNamespace+".") {
			return fmt.Errorf("%s: package %s cannot declare namespace %s", path, packageNamespace, name)
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
		source := &sourceFile{Path: path, Source: data, Tree: tree, Unit: unit}
		for _, class := range unit.Classes {
			class.File = source
			class.Namespace = ns
		}
		ns.Files = append(ns.Files, source)
		rel, _ := filepath.Rel(root, path)
		if packageNamespace == "" && strings.HasPrefix(rel, "tests"+string(filepath.Separator)) {
			if name == "main" {
				return fmt.Errorf("%s: tests must use a test namespace, not main", path)
			}
			p.TestNamespaces[name] = true
		}
		return nil
	}
	err := filepath.WalkDir(root, visit)
	if err != nil {
		return nil, err
	}
	for path := range remaining {
		return nil, fmt.Errorf("overlay target must be an existing production .ghi file in the project: %s", path)
	}
	roots, err := mojave.SourceRoots(root)
	if err != nil {
		return nil, err
	}
	for _, dependency := range roots {
		scanRoot, packageNamespace = dependency.Path, dependency.Namespace
		if err := filepath.WalkDir(scanRoot, visit); err != nil {
			return nil, err
		}
	}
	if len(p.Namespaces) == 0 {
		return nil, fmt.Errorf("no .ghi source files found in %s", root)
	}
	main, ok := p.Namespaces["main"]
	if !testing && (!ok || main.Dir != root) {
		return nil, fmt.Errorf("project root must declare namespace main")
	}
	sort.Slice(p.Ordered, func(i, j int) bool { return p.Ordered[i].Name < p.Ordered[j].Name })
	return p, nil
}
