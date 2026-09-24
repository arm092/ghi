package compiler

import "strings"

// Debug metadata uses exact compiler symbols; names containing underscores must
// never be reconstructed by splitting a generated identifier.
type debugMetadata struct {
	Version   int                      `json:"version"`
	Fields    map[string]string        `json:"fields"`
	Types     map[string]string        `json:"types"`
	Functions map[string]debugFunction `json:"functions"`
}

type debugFunction struct {
	Name   string `json:"name"`
	Helper bool   `json:"helper"`
}

func (p *program) debugMetadata() debugMetadata {
	m := debugMetadata{1, map[string]string{}, map[string]string{}, map[string]debugFunction{}}
	for _, c := range p.classes() {
		prefix := generatedModule + "/" + strings.ReplaceAll(c.Namespace.Name, ".", "/")
		if c.Namespace.Name == "main" {
			prefix = "main"
		}
		name := c.Namespace.Name + "." + c.Name
		m.Types[prefix+"."+c.Name] = name
		m.Types[prefix+".ghiData_"+c.Name] = name
		if c.Interface {
			continue
		}
		receiver := prefix + ".(*ghiData_" + c.Name + ")."
		for _, f := range c.allFields() {
			m.Fields["F_"+f.Owner.key()+"_"+f.Name] = f.Name
			for _, symbol := range []string{fieldGet(f), fieldSet(f), fieldRef(f)} {
				m.Functions[receiver+symbol] = debugFunction{name + "." + f.Name, true}
			}
		}
		for _, method := range c.Methods {
			m.Functions[prefix+"."+bodyName(method)] = debugFunction{name + "." + method.Name, false}
		}
		for _, method := range c.allMethods() {
			m.Functions[receiver+"GhiM_"+method.Name] = debugFunction{name + "." + method.Name, true}
		}
		for ancestor := c; ancestor != nil; ancestor = ancestor.Parent {
			m.Functions[receiver+"GhiIs_"+ancestor.key()] = debugFunction{name, true}
		}
		m.Functions[prefix+".GhiInit_"+c.Name] = debugFunction{name + ".constructor", false}
		m.Functions[prefix+".GhiNew_"+c.Name] = debugFunction{name + ".constructor", true}
	}
	return m
}
