package compiler

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"go/ast"
	"go/parser"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Certificates are valid only with the verified generated snapshot. Parsed
// source metadata remains authoritative for visibility, defaults and ancestry;
// cached Go declarations supply types without rechecking function bodies.
type semanticCache struct {
	keys    map[string]string
	reused  map[string]bool
	files   map[string][][]byte
	helpers []int
	names   map[string]string
}

func namespaceDirectory(name string) string {
	if name == "main" {
		return "."
	}
	return filepath.FromSlash(strings.ReplaceAll(name, ".", "/"))
}

func (p *program) prepareSemanticCache(cache *buildCache) *semanticCache {
	s := &semanticCache{keys: map[string]string{}, reused: map[string]bool{}, files: map[string][][]byte{}, names: map[string]string{}}
	// Leaf receiver specialization depends on descendants as well as imports.
	// A changed inheritance graph invalidates every certificate conservatively.
	var graph []string
	for _, ns := range p.Ordered {
		for _, f := range ns.Files {
			for _, c := range f.Unit.Classes {
				graph = append(graph, ns.Name+"."+c.Name+":"+c.ParentName)
			}
		}
	}
	sort.Strings(graph)
	var key func(*namespace) string
	key = func(ns *namespace) string {
		if k := s.keys[ns.Name]; k != "" {
			return k
		}
		h := sha256.New()
		enc := json.NewEncoder(h)
		enc.Encode([]string{p.CacheEnvironment, ns.Name})
		enc.Encode(graph)
		for _, f := range ns.Files {
			enc.Encode([]string{f.Path, string(f.Source)})
		}
		deps := append([]string{}, ns.Imports...)
		sort.Strings(deps)
		for _, name := range deps {
			enc.Encode([]string{name, key(p.Namespaces[name])})
		}
		k := hex.EncodeToString(h.Sum(nil))
		s.keys[ns.Name] = k
		return k
	}
	for _, ns := range p.Ordered {
		key(ns)
	}
	old := cache.snapshot()
	if old == nil {
		return s
	}
	s.helpers = old.Helpers
	for _, ns := range p.Ordered {
		// Runtime grows helpers and frame mappings for newly lowered code.
		if ns == p.Runtime || old.Namespaces[ns.Name] != s.keys[ns.Name] {
			continue
		}
		var paths []string
		for path := range old.Files {
			if filepath.Dir(path) == namespaceDirectory(ns.Name) && filepath.Ext(path) == ".go" {
				paths = append(paths, path)
			}
		}
		sort.Slice(paths, func(i, j int) bool {
			index := func(path string) int {
				n, _ := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(filepath.Base(path), "ghi_source_"), ".go"))
				return n
			}
			return index(paths[i]) < index(paths[j])
		})
		if len(paths) < len(ns.Files) {
			continue
		}
		var contents [][]byte
		for _, path := range paths {
			data, err := os.ReadFile(filepath.Join(cache.workspace, path))
			if err != nil {
				contents = nil
				break
			}
			contents = append(contents, data)
		}
		if len(contents) != len(paths) {
			continue
		}
		s.files[ns.Name] = contents
	}
	for name, display := range old.SpecializedNames {
		s.names[name] = display
	}
	return s
}

func (p *program) reuseSemanticNamespaces() {
	s := p.Semantic
	if s == nil {
		return
	}
	// A changed class needs its ancestors' typed bodies for specialization.
	// Recheck the affected inheritance component, retaining unrelated reuse.
	classes := p.classes()
	for changed := true; changed; {
		changed = false
		for _, c := range classes {
			if c.Parent == nil {
				continue
			}
			a, b := c.Namespace.Name, c.Parent.Namespace.Name
			if (len(s.files[a]) == 0) != (len(s.files[b]) == 0) {
				delete(s.files, a)
				delete(s.files, b)
				changed = true
			}
		}
	}
	for _, ns := range p.Ordered {
		contents := s.files[ns.Name]
		if len(contents) == 0 {
			continue
		}
		var trees []*ast.File
		for i, data := range contents {
			tree, err := parser.ParseFile(p.Fset, ns.Dir+"/ghi_cached_"+strconv.Itoa(i)+".go", data, parser.SkipObjectResolution)
			if err != nil {
				trees = nil
				break
			}
			trees = append(trees, tree)
		}
		if len(trees) != len(contents) {
			continue
		}
		for i, tree := range trees {
			if i >= len(ns.Files) {
				ns.Files = append(ns.Files, &sourceFile{Path: ns.Dir + "/ghi_cached_" + strconv.Itoa(i), Unit: &unit{Native: true}})
			}
			f := ns.Files[i]
			f.Tree = tree
			f.Cached = contents[i]
			// Copy: the runtime alias unit is shared across namespaces.
			u := *f.Unit
			u.Native = true
			f.Unit = &u
		}
		s.reused[ns.Name] = true
	}
	if len(s.reused) > 0 {
		for _, count := range s.helpers {
			p.ensureErrorHelper(count)
		}
	}
}

func (p *program) reusedNamespace(ns *namespace) bool {
	return p.Semantic != nil && p.Semantic.reused[ns.Name]
}
