package compiler

import (
	"errors"
	"go/ast"
	"go/token"
	"go/types"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

type sourceDiagnostic struct {
	message string
	cause   error
}

func (e sourceDiagnostic) Error() string { return e.message }
func (e sourceDiagnostic) Unwrap() error { return e.cause }

func (p *program) sourceError(err error) error {
	if err == nil {
		return nil
	}
	var original sourceDiagnostic
	if errors.As(err, &original) {
		return err
	}
	replacements := map[string]string{}
	var typed types.Error
	diagnosticFile := ""
	message := err.Error()
	var position token.Position
	if errors.As(err, &typed) {
		position = p.Fset.Position(typed.Pos)
		message = p.ternaryDiagnostic(typed, message)
	} else if location := diagnosticLocation.FindStringSubmatch(message); location != nil {
		position.Filename = location[1]
		position.Line, _ = strconv.Atoi(location[2])
		position.Column, _ = strconv.Atoi(location[3])
	}
	if position.IsValid() {
		diagnosticFile = position.Filename
		for _, ns := range p.Ordered {
			for _, file := range ns.Files {
				if file.Path == diagnosticFile {
					original := file.Unit.CoverageSource.position(position)
					if original.IsValid() {
						message = strings.Replace(message, position.String()+":", original.String()+":", 1)
					}
				}
			}
		}
	}
	for _, ns := range p.Ordered {
		for _, file := range ns.Files {
			for _, e := range file.Unit.Enums {
				for _, c := range e.Cases {
					replacements[enumSymbol(e.Name, c.Name)] = e.Name + "." + c.Name
				}
			}
			for _, selected := range file.Unit.TypeImports {
				if diagnosticFile != "" && file.Path != diagnosticFile {
					continue
				}
				// go/types prints the imported package name, which may differ
				// from the synthetic import identifier used by the lowering.
				if imported := p.Namespaces[selected.Namespace]; imported != nil {
					replacements[imported.GoName+"."+selected.Name] = selected.Alias
				}
				for _, spec := range file.Tree.Imports {
					path, _ := strconv.Unquote(spec.Path.Value)
					if path == generatedModule+"/"+strings.ReplaceAll(selected.Namespace, ".", "/") && spec.Name != nil {
						replacements[spec.Name.Name+"."+selected.Name] = selected.Alias
						// NewReplacer makes one pass: map qualified generated names
						// directly instead of expecting a second alias replacement.
						prefix := spec.Name.Name + "."
						replacements[prefix+"GhiNew_"+selected.Name] = selected.Alias
						replacements[prefix+"ghiData_"+selected.Name] = selected.Alias
						replacements[prefix+"GhiInit_"+selected.Name] = selected.Alias + ".constructor"
					}
				}
			}
			for _, c := range file.Unit.Classes {
				replacements["GhiIs_"+c.key()] = c.Name
				replacements["GhiNew_"+c.Name] = c.Name
				replacements["GhiInit_"+c.Name] = c.Name + ".constructor"
				replacements["ghiData_"+c.Name] = c.Name
				for _, field := range c.Fields {
					for _, name := range []string{fieldGet(field), fieldSet(field), fieldRef(field)} {
						replacements[name] = c.Name + "." + field.Name
					}
				}
				for _, method := range c.Methods {
					replacements[bodyName(method)] = c.Name + "." + method.Name
					replacements["GhiM_"+method.Name] = method.Name
					replacements["GhiPublic_"+method.Name] = "public " + method.Name
				}
			}
		}
	}
	keys := make([]string, 0, len(replacements))
	for k := range replacements {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if len(keys[i]) == len(keys[j]) {
			return keys[i] < keys[j]
		}
		return len(keys[i]) > len(keys[j])
	})
	pairs := make([]string, 0, len(keys)*2)
	for _, k := range keys {
		pairs = append(pairs, k, replacements[k])
	}
	message = strings.NewReplacer(pairs...).Replace(message)
	message = strings.ReplaceAll(message, generatedModule+"/", "")
	// A pointer to a Ghi class represents ?Class. Keep native Go pointer types
	// intact and translate only classes visible in the diagnostic namespace.
	for _, ns := range p.Ordered {
		for _, file := range ns.Files {
			if file.Path != diagnosticFile {
				continue
			}
			names := []string{}
			for _, peer := range ns.Files {
				for _, class := range peer.Unit.Classes {
					if !class.Interface {
						names = append(names, class.Name)
					}
				}
			}
			for _, selected := range file.Unit.TypeImports {
				if imported := p.Namespaces[selected.Namespace]; imported != nil {
					for _, peer := range imported.Files {
						for _, class := range peer.Unit.Classes {
							if class.Name == selected.Name && !class.Interface {
								names = append(names, selected.Alias)
							}
						}
					}
				}
			}
			for _, name := range names {
				pattern := regexp.MustCompile(`\*` + regexp.QuoteMeta(name) + `\b`)
				message = pattern.ReplaceAllString(message, "?"+name)
			}
		}
	}
	message = regexp.MustCompile(`: \?[^\n:]+ does not implement [^\n]+ \(type \?[^\n:]+ is pointer to interface, not interface\)$`).ReplaceAllString(message, "")
	return sourceDiagnostic{message, err}
}

func (p *program) ternaryDiagnostic(typed types.Error, message string) string {
	var selected *ast.FuncLit
	for fn := range p.TernaryFunctions {
		if typed.Pos < fn.Pos() || typed.Pos >= fn.End() {
			continue
		}
		if selected == nil || fn.End()-fn.Pos() < selected.End()-selected.Pos() {
			selected = fn
		}
	}
	if selected == nil {
		return message
	}
	insideUserFunction := false
	ast.Inspect(selected.Body, func(node ast.Node) bool {
		if fn, ok := node.(*ast.FuncLit); ok && typed.Pos >= fn.Pos() && typed.Pos < fn.End() && !p.TernaryFunctions[fn] {
			insideUserFunction = true
			return false
		}
		return true
	})
	if insideUserFunction {
		return message
	}
	for _, statement := range selected.Body.List {
		if condition, ok := statement.(*ast.IfStmt); ok && typed.Pos >= condition.Cond.Pos() && typed.Pos < condition.Cond.End() && strings.Contains(message, "non-boolean condition in if statement") {
			return strings.Replace(message, "non-boolean condition in if statement", "ternary condition must be bool", 1)
		}
	}
	ast.Inspect(selected.Body, func(node ast.Node) bool {
		if _, nested := node.(*ast.FuncLit); nested {
			return false
		}
		if ret, ok := node.(*ast.ReturnStmt); ok {
			for _, value := range ret.Results {
				if typed.Pos >= value.Pos() && typed.Pos < value.End() {
					message = strings.Replace(message, "in return statement", "in ternary branch", 1)
				}
			}
		}
		return true
	})
	return message
}
