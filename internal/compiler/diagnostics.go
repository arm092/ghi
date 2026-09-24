package compiler

import (
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
	for _, ns := range p.Ordered {
		for _, file := range ns.Files {
			for _, e := range file.Unit.Enums {
				for _, c := range e.Cases {
					replacements[enumSymbol(e.Name, c.Name)] = e.Name + "." + c.Name
				}
			}
			for _, selected := range file.Unit.TypeImports {
				for _, spec := range file.Tree.Imports {
					path, _ := strconv.Unquote(spec.Path.Value)
					if path == generatedModule+"/"+strings.ReplaceAll(selected.Namespace, ".", "/") && spec.Name != nil {
						replacements[spec.Name.Name+"."+selected.Name] = selected.Alias
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
	message := strings.NewReplacer(pairs...).Replace(err.Error())
	message = strings.ReplaceAll(message, generatedModule+"/", "")
	return sourceDiagnostic{message, err}
}
