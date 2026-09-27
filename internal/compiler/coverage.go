package compiler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Probes precede lowering: generated wrappers add no coverage units, and
// specialized copies of method bodies retain the same probe identity.
type coveragePoint struct {
	File         string
	Line, Column int
}
type coveragePlan struct {
	Points  []coveragePoint
	Results []string
}

func (p *program) instrumentCoverage() error {
	ids := map[coveragePoint]int{}
	fragments := map[*token.File]*functionDecl{}
	for _, ns := range p.Ordered {
		for _, file := range ns.Files {
			for _, c := range file.Unit.Classes {
				for _, m := range c.Methods {
					fragments[p.Fset.File(m.Node.Pos())] = m
				}
				if c.Constructor != nil {
					fragments[p.Fset.File(c.Constructor.Node.Pos())] = c.Constructor
				}
			}
		}
	}
	seen := map[ast.Node]bool{}
	for _, ns := range p.Ordered {
		if ns == p.Runtime || p.TestNamespaces[ns.Name] {
			continue
		}
		for _, file := range ns.Files {
			relative, err := filepath.Rel(p.Root, file.Path)
			if err != nil || file.Unit.Native || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || strings.HasPrefix(relative, ".ghi"+string(filepath.Separator)) {
				continue
			}
			tokens, err := lexSource(file.Path, file.Source)
			if err != nil {
				return err
			}
			sourceTokens := map[[2]int]string{}
			for _, t := range tokens {
				prefix := string(file.Source[:t.Start])
				col := t.Start - strings.LastIndex(prefix, "\n")
				sourceTokens[[2]int{t.Line, col}] = t.Text
			}
			probe := func(stmt ast.Stmt) ast.Stmt {
				for {
					label, ok := stmt.(*ast.LabeledStmt)
					if !ok {
						break
					}
					stmt = label.Stmt
				}
				switch stmt.(type) {
				case *ast.BlockStmt, *ast.EmptyStmt, *ast.CaseClause, *ast.CommClause:
					return nil
				}
				pos := p.Fset.Position(stmt.Pos())
				if fn := fragments[p.Fset.File(stmt.Pos())]; fn != nil && fn.SourceTail.IsValid() {
					if pos.Column == 0 {
						pos.Column = p.Fset.PositionFor(stmt.Pos(), false).Column
						if pos.Line == fn.SourceTail.Line {
							pos.Column += fn.SourceTail.Column - 1 - fn.HeaderWidth
						}
					} else if pos.Line == fn.SourceTail.Line {
						pos.Column += fn.SourceTail.Column - 1
					}
				}
				pos = file.Unit.CoverageSource.position(pos)
				word := sourceTokens[[2]int{pos.Line, pos.Column}]
				if pos.Filename != file.Path || word == "" {
					return nil
				}
				var b bytes.Buffer
				if printer.Fprint(&b, token.NewFileSet(), stmt) != nil {
					return nil
				}
				ts, err := lexSource("coverage", b.Bytes())
				if err != nil || len(ts) == 0 {
					return nil
				}
				first := ts[0].Text
				if first == "GhiTry" {
					first = "try"
				}
				if first == "GhiThrow" {
					first = "throw"
				}
				// Normalization inserts catcher dispatch at catch locations. Only genuine
				// source tokens may create probes; do not count generated handler plumbing.
				if first != word {
					return nil
				}
				point := coveragePoint{filepath.ToSlash(relative), pos.Line, pos.Column}
				id, ok := ids[point]
				if !ok {
					id = len(p.Coverage.Points)
					ids[point] = id
					p.Coverage.Points = append(p.Coverage.Points, point)
				}
				call, _ := parser.ParseExpr(fmt.Sprintf("%s(%d)", p.runtimeSymbol("CoverageHit", file, ns), id))
				return &ast.ExprStmt{X: call}
			}
			var visit func(ast.Node)
			visit = func(root ast.Node) {
				ast.Inspect(root, func(n ast.Node) bool {
					if n == nil {
						return false
					}
					if seen[n] {
						return false
					}
					seen[n] = true
					if conditional, ok := n.(*ast.IfStmt); ok {
						if nested, ok := conditional.Else.(*ast.IfStmt); ok {
							conditional.Else = &ast.BlockStmt{List: []ast.Stmt{nested}}
						}
					}
					var list *[]ast.Stmt
					switch n := n.(type) {
					case *ast.BlockStmt:
						list = &n.List
					case *ast.CaseClause:
						list = &n.Body
					case *ast.CommClause:
						list = &n.Body
					}
					if list != nil {
						old := *list
						next := make([]ast.Stmt, 0, len(old)*2)
						for _, stmt := range old {
							visit(stmt)
							if hit := probe(stmt); hit != nil {
								next = append(next, hit)
							}
							// A goto bypasses the probe preceding its target label. Count that
							// target entry at the jump as well, keeping loop/select labels intact.
							target := stmt
							jumps := map[*ast.LabeledStmt]bool{}
							for {
								for {
									label, ok := target.(*ast.LabeledStmt)
									if !ok {
										break
									}
									target = label.Stmt
								}
								jump, ok := target.(*ast.BranchStmt)
								if !ok || jump.Tok != token.GOTO || jump.Label == nil || jump.Label.Obj == nil {
									break
								}
								label, ok := jump.Label.Obj.Decl.(*ast.LabeledStmt)
								if !ok || jumps[label] {
									break
								}
								jumps[label] = true
								if hit := probe(label); hit != nil {
									next = append(next, hit)
								}
								target = label.Stmt
							}
							next = append(next, stmt)
						}
						*list = next
					}
					return true
				})
			}
			visit(file.Tree)
			for _, class := range file.Unit.Classes {
				for _, method := range class.Methods {
					visit(method.Node)
				}
				if class.Constructor != nil {
					visit(class.Constructor.Node)
				}
			}
		}
	}
	source := fmt.Sprintf("package runtime\nimport \"sync/atomic\"\nvar ghiCoverage [%d]atomic.Uint32\nfunc CoverageHit(id int) { ghiCoverage[id].Store(1) }\nfunc CoverageSnapshot() []uint32 {\n values:=make([]uint32,len(ghiCoverage))\n for i:=range values {values[i]=ghiCoverage[i].Load()}\n return values\n}\n", len(p.Coverage.Points))
	tree, err := parser.ParseFile(p.Fset, "ghi-coverage.native", source, parser.SkipObjectResolution)
	if err != nil {
		return err
	}
	p.Runtime.Files = append(p.Runtime.Files, &sourceFile{Path: "ghi-coverage.native", Tree: tree, Unit: &unitDataNative})
	return nil
}

// Go set-profile syntax, one unit per original statement entry. One-column
// spans anchor the probe without inventing ranges across normalized syntax.
func (c *coveragePlan) report(log io.Writer, output string) error {
	counts := make([]uint32, len(c.Points))
	for _, path := range c.Results {
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("coverage result: %w", err)
		}
		var values []uint32
		if err := json.Unmarshal(data, &values); err != nil {
			return fmt.Errorf("coverage result: %w", err)
		}
		if len(values) != len(counts) {
			return fmt.Errorf("incomplete coverage result")
		}
		for i, v := range values {
			if v > 0 {
				counts[i] = 1
			}
		}
	}
	order := make([]int, len(counts))
	for i := range order {
		order[i] = i
	}
	sort.Slice(order, func(i, j int) bool {
		a, b := c.Points[order[i]], c.Points[order[j]]
		if a.File != b.File {
			return a.File < b.File
		}
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		return a.Column < b.Column
	})
	type total struct{ hit, all int }
	files := map[string]total{}
	sum := total{}
	var profile strings.Builder
	profile.WriteString("mode: set\n")
	for _, id := range order {
		p := c.Points[id]
		v := files[p.File]
		v.all++
		v.hit += int(counts[id])
		files[p.File] = v
		sum.all++
		sum.hit += int(counts[id])
		fmt.Fprintf(&profile, "%s:%d.%d,%d.%d 1 %d\n", p.File, p.Line, p.Column, p.Line, p.Column+1, counts[id])
	}
	if output != "" {
		if err := writeCoverageProfile(output, []byte(profile.String())); err != nil {
			return err
		}
	}
	if log != nil {
		names := make([]string, 0, len(files))
		for name := range files {
			names = append(names, name)
		}
		sort.Strings(names)
		fmt.Fprintln(log, "Ghi statement coverage:")
		for _, name := range names {
			v := files[name]
			fmt.Fprintf(log, "  %s: %.1f%% (%d/%d)\n", name, 100*float64(v.hit)/float64(v.all), v.hit, v.all)
		}
		if sum.all == 0 {
			fmt.Fprintln(log, "  total: no executable statements")
		} else {
			fmt.Fprintf(log, "  total: %.1f%% (%d/%d)\n", 100*float64(sum.hit)/float64(sum.all), sum.hit, sum.all)
		}
	}
	return nil
}

func validateCoverageProfile(path string) error {
	if path == "" {
		return nil
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".ghi", ".go", ".json", ".lock", ".mod", ".sum", ".sql":
		return fmt.Errorf("coverage profile must not overwrite project inputs")
	}
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("coverage profile must be a regular file")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if !bytes.HasPrefix(data, []byte("mode: set\n")) {
		return fmt.Errorf("refusing to overwrite a non-coverage file: %s", path)
	}
	return nil
}
func writeCoverageProfile(path string, data []byte) error {
	if err := validateCoverageProfile(path); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".ghi-coverage-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
