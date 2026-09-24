package compiler

import (
	"encoding/hex"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"strings"
)

func (p *program) classes() map[string]*classDecl {
	result := map[string]*classDecl{}
	for _, ns := range p.Ordered {
		for _, file := range ns.Files {
			for _, c := range file.Unit.Classes {
				result[ns.Name+"."+c.Name] = c
			}
		}
	}
	return result
}

func (p *program) classNamed(name string, file *sourceFile, ns *namespace) *classDecl {
	name = genericName(name)
	classes := p.classes()
	if c := classes[ns.Name+"."+name]; c != nil {
		return c
	}
	alias, local, ok := strings.Cut(name, ".")
	if !ok {
		if name == "Exception" || name == "GoError" || name == "StackFrame" {
			return classes[runtimeNamespace+"."+name]
		}
		return nil
	}
	for _, spec := range file.Tree.Imports {
		path, _ := strconv.Unquote(spec.Path.Value)
		for _, other := range p.Ordered {
			if path != generatedModule+"/"+strings.ReplaceAll(other.Name, ".", "/") {
				continue
			}
			importName := other.GoName
			if spec.Name != nil {
				importName = spec.Name.Name
			}
			if importName == alias {
				return classes[other.Name+"."+local]
			}
		}
	}
	return nil
}

func (c *classDecl) method(name string) *functionDecl {
	for _, m := range c.Methods {
		if m.Name == name {
			return m
		}
	}
	if c.Parent != nil {
		return c.Parent.method(name)
	}
	return nil
}
func (c *classDecl) field(name string) *fieldDecl {
	for _, f := range c.Fields {
		if f.Name == name {
			return f
		}
	}
	if c.Parent != nil {
		return c.Parent.field(name)
	}
	return nil
}
func (c *classDecl) allFields() []*fieldDecl {
	var result []*fieldDecl
	if c.Parent != nil {
		result = append(result, c.Parent.allFields()...)
	}
	return append(result, c.Fields...)
}
func (c *classDecl) allMethods() []*functionDecl {
	var result []*functionDecl
	if c.Parent != nil {
		result = append(result, c.Parent.allMethods()...)
	}
	for _, m := range c.Methods {
		replaced := false
		for i, old := range result {
			if old.Name == m.Name {
				result[i] = m
				replaced = true
				break
			}
		}
		if !replaced {
			result = append(result, m)
		}
	}
	return result
}
func (c *classDecl) key() string {
	return hex.EncodeToString([]byte(c.Namespace.Name)) + "_" + c.Name
}
func fieldGet(f *fieldDecl) string    { return "GhiGet_" + f.Owner.key() + "_" + f.Name }
func fieldSet(f *fieldDecl) string    { return "GhiSet_" + f.Owner.key() + "_" + f.Name }
func fieldRef(f *fieldDecl) string    { return "GhiRef_" + f.Owner.key() + "_" + f.Name }
func bodyName(f *functionDecl) string { return "GhiBody_" + f.Owner.Name + "_" + f.Name }

func (p *program) importAlias(from, to *sourceFile, path, suggested string) string {
	for _, spec := range to.Tree.Imports {
		candidate, _ := strconv.Unquote(spec.Path.Value)
		if candidate == path {
			if spec.Name != nil {
				return spec.Name.Name
			}
			return suggested
		}
	}
	alias := fmt.Sprintf("ghi_dependency_%d", len(to.Tree.Imports))
	spec := &ast.ImportSpec{Name: ast.NewIdent(alias), Path: &ast.BasicLit{Kind: token.STRING, Value: strconv.Quote(path)}}
	to.Tree.Imports = append(to.Tree.Imports, spec)
	to.Tree.Decls = append([]ast.Decl{&ast.GenDecl{Tok: token.IMPORT, Specs: []ast.Spec{spec}}}, to.Tree.Decls...)
	return alias
}
func (p *program) classSymbol(c *classDecl, name string, to *sourceFile, ns *namespace) string {
	if c.Namespace == ns {
		return name
	}
	path := generatedModule + "/" + strings.ReplaceAll(c.Namespace.Name, ".", "/")
	return p.importAlias(c.File, to, path, c.Namespace.GoName) + "." + name
}
func (p *program) typeText(typ ast.Expr, owner *classDecl, to *sourceFile, ns *namespace) string {
	copy, _ := parser.ParseExpr(expressionText(typ))
	node := walkNode(copy, func(n ast.Node) ast.Node {
		switch n := n.(type) {
		case *ast.SelectorExpr:
			if id, ok := n.X.(*ast.Ident); ok {
				for _, imp := range owner.File.Tree.Imports {
					path, _ := strconv.Unquote(imp.Path.Value)
					alias := path[strings.LastIndex(path, "/")+1:]
					if imp.Name != nil {
						alias = imp.Name.Name
					}
					if alias == id.Name {
						n.X = ast.NewIdent(p.importAlias(owner.File, to, path, alias))
						break
					}
				}
			}
			return n
		case *ast.Ident:
			if owner.TypeParams != nil {
				for _, field := range owner.TypeParams.List {
					for _, name := range field.Names {
						if name.Name == n.Name {
							return n
						}
					}
				}
			}
			if c := p.classes()[owner.Namespace.Name+"."+n.Name]; c != nil && c.Namespace != ns {
				expression, _ := parser.ParseExpr(p.classSymbol(c, c.Name, to, ns))
				return expression
			}
		}
		return n
	}, true)
	return expressionText(node.(ast.Expr))
}
func (p *program) parameters(f *functionDecl, to *sourceFile, ns *namespace) string {
	var result []string
	for _, param := range f.Node.Type.Params.List {
		if param.Names[0].Name == "this" {
			continue
		}
		typ := expressionText(param.Type)
		if f.Owner != nil {
			typ = p.typeText(param.Type, f.Owner, to, ns)
		}
		result = append(result, param.Names[0].Name+" "+typ)
	}
	return strings.Join(result, ", ")
}
func (p *program) results(f *functionDecl, to *sourceFile, ns *namespace) string {
	if f.Node.Type.Results == nil {
		return ""
	}
	var result []string
	for _, field := range f.Node.Type.Results.List {
		typ := expressionText(field.Type)
		if f.Owner != nil {
			typ = p.typeText(field.Type, f.Owner, to, ns)
		}
		if len(field.Names) == 0 {
			result = append(result, typ)
		} else {
			for _, name := range field.Names {
				result = append(result, name.Name+" "+typ)
			}
		}
	}
	return " (" + strings.Join(result, ", ") + ")"
}
func argumentNames(f *functionDecl) string {
	var names []string
	for _, param := range f.Node.Type.Params.List {
		for _, name := range param.Names {
			if name.Name != "this" {
				names = append(names, name.Name)
			}
		}
	}
	return strings.Join(names, ", ")
}
func (p *program) signature(f *functionDecl, where *classDecl) string {
	var parts []string
	for _, param := range f.Node.Type.Params.List {
		if param.Names[0].Name != "this" {
			parts = append(parts, p.typeText(param.Type, f.Owner, where.File, where.Namespace))
		}
	}
	var results []string
	if f.Node.Type.Results != nil {
		for _, field := range f.Node.Type.Results.List {
			count := len(field.Names)
			if count == 0 {
				count = 1
			}
			for i := 0; i < count; i++ {
				results = append(results, p.typeText(field.Type, f.Owner, where.File, where.Namespace))
			}
		}
	}
	return strings.Join(parts, ",") + "->" + strings.Join(results, ",")
}
func visibilityRank(value string) int {
	switch value {
	case "public":
		return 2
	case "protected":
		return 1
	}
	return 0
}
func accessible(visibility string, owner, caller *classDecl) bool {
	if visibility == "public" {
		return true
	}
	if owner == caller {
		return true
	}
	if visibility == "protected" {
		for c := caller; c != nil; c = c.Parent {
			if c == owner {
				return true
			}
		}
	}
	return false
}

func (p *program) prepareClasses() error {
	classes := p.classes()
	seen := map[string]bool{}
	for _, ns := range p.Ordered {
		for _, file := range ns.Files {
			for _, c := range file.Unit.Classes {
				key := ns.Name + "." + c.Name
				if seen[key] {
					return fmt.Errorf("duplicate class %s", key)
				}
				seen[key] = true
				if c.ParentName != "" {
					if genericName(c.ParentName) != c.ParentName {
						return fmt.Errorf("%s:%d: type arguments are not supported in class inheritance", file.Path, c.Line)
					}
					c.Parent = p.classNamed(c.ParentName, file, ns)
					if c.Parent == nil || c.Parent.Interface {
						return fmt.Errorf("class %s: unknown parent class %s", c.Name, c.ParentName)
					}
					if c.TypeParams != nil || c.Parent.TypeParams != nil {
						return fmt.Errorf("%s:%d: generic class inheritance is not supported yet", file.Path, c.Line)
					}
				}
				for _, name := range c.InterfaceNames {
					target := p.classNamed(name, file, ns)
					if target == nil || !target.Interface {
						return fmt.Errorf("class %s: unknown interface %s", c.Name, name)
					}
					c.Interfaces = append(c.Interfaces, target)
				}
			}
		}
	}
	for _, c := range classes {
		ancestors := map[*classDecl]bool{}
		for a := c; a != nil; a = a.Parent {
			if ancestors[a] {
				return fmt.Errorf("inheritance cycle at %s", c.Name)
			}
			ancestors[a] = true
		}
	}
	for _, c := range classes {
		for _, method := range c.Methods {
			var base *functionDecl
			if c.Parent != nil {
				base = c.Parent.method(method.Name)
			}
			if method.Override && (base == nil || base.Visibility == "private") {
				return fmt.Errorf("%s.%s: override requires an accessible parent method", c.Name, method.Name)
			}
			if base != nil {
				if !method.Override {
					return fmt.Errorf("%s.%s: overriding a parent method requires override", c.Name, method.Name)
				}
				if p.signature(method, c) != p.signature(base, c) {
					return fmt.Errorf("%s.%s: incompatible override signature", c.Name, method.Name)
				}
				if visibilityRank(method.Visibility) < visibilityRank(base.Visibility) {
					return fmt.Errorf("%s.%s: override reduces visibility", c.Name, method.Name)
				}
			}
		}
		for _, iface := range c.Interfaces {
			for _, requirement := range iface.Methods {
				method := c.method(requirement.Name)
				if method == nil || method.Visibility != "public" || (iface.TypeParams == nil && p.signature(method, c) != p.signature(requirement, c)) {
					return fmt.Errorf("class %s does not implement %s.%s", c.Name, iface.Name, requirement.Name)
				}
			}
		}
		if !c.Interface && c.Constructor == nil {
			fn, err := parseFunction(p.Fset, c.File.Path, "constructor", "", " {}", 1)
			if err != nil {
				return err
			}
			fn.Owner = c
			c.Constructor = fn
		}
	}
	return nil
}
