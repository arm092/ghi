package compiler

import (
	"errors"
	"go/types"
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
	replacements := map[string]string{}
	var typed types.Error
	diagnosticFile := ""
	message := err.Error()
	if errors.As(err, &typed) {
		position := p.Fset.Position(typed.Pos)
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
	return sourceDiagnostic{message, err}
}
