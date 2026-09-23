package compiler

import (
	"strconv"
	"strings"
)

func (p *program) importedNamespace(alias string, file *sourceFile) *namespace {
	for _, spec := range file.Tree.Imports {
		path, _ := strconv.Unquote(spec.Path.Value)
		for _, ns := range p.Ordered {
			name := ns.GoName
			if spec.Name != nil {
				name = spec.Name.Name
			}
			if name == alias && path == namespacePath(ns) {
				return ns
			}
		}
	}
	return nil
}

func (p *program) functionNamed(name string, file *sourceFile, ns *namespace) *functionDecl {
	if alias, member, ok := strings.Cut(name, "."); ok {
		ns = p.importedNamespace(alias, file)
		if ns == nil {
			return nil
		}
		name = member
	}
	for _, file := range ns.Files {
		if fn := file.Unit.Functions[name]; fn != nil {
			return fn
		}
	}
	return nil
}

func (p *program) classSymbolIfImported(c *classDecl, name string, file *sourceFile, ns *namespace) string {
	if c.Namespace == ns {
		return name
	}
	for _, spec := range file.Tree.Imports {
		path, _ := strconv.Unquote(spec.Path.Value)
		if path == namespacePath(c.Namespace) {
			alias := c.Namespace.GoName
			if spec.Name != nil {
				alias = spec.Name.Name
			}
			return alias + "." + name
		}
	}
	return ""
}
