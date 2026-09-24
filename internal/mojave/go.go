package mojave

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"ghi/internal/toolchain"
	goversion "go/version"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"golang.org/x/mod/modfile"
	"golang.org/x/mod/module"
	"golang.org/x/mod/semver"
)

type GoRequest struct {
	Source  string `json:"source"`
	Path    string `json:"path"`
	Query   string `json:"query"`
	Version string `json:"version"`
}
type GoModule struct {
	Path     string `json:"path"`
	Version  string `json:"version"`
	Sum      string `json:"sum"`
	GoModSum string `json:"goModSum"`
}
type GoLock struct {
	GoVersion string      `json:"goVersion"`
	Requests  []GoRequest `json:"requests"`
	Modules   []GoModule  `json:"modules"`
	Checksums string      `json:"checksums"`
	Integrity string      `json:"integrity"`
}

func validateGoDependency(path, query string) error {
	if e := module.CheckPath(path); e != nil {
		return fmt.Errorf("invalid Go module: %w", e)
	}
	if path == "ghi.generated" || query == "" || strings.HasPrefix(query, "-") || strings.ContainsAny(query, " \t\r\n\x00@") {
		return fmt.Errorf("invalid Go dependency %s@%s", path, query)
	}
	if query == "none" || query == "upgrade" || query == "patch" {
		return fmt.Errorf("Go dependency %s requires a version, ref, or latest", path)
	}
	return nil
}
func AddGo(ctx context.Context, root, path, query string) error {
	if query == "" {
		query = "latest"
	}
	if e := validateGoDependency(path, query); e != nil {
		return e
	}
	return mutate(ctx, root, func(m *Manifest) error {
		if m.GoDependencies == nil {
			m.GoDependencies = map[string]string{}
		}
		m.GoDependencies[path] = query
		return nil
	}, false)
}
func RemoveGo(ctx context.Context, root, path string) error {
	return mutate(ctx, root, func(m *Manifest) error {
		if _, ok := m.GoDependencies[path]; !ok {
			return fmt.Errorf("go:%s is not a direct dependency", path)
		}
		delete(m.GoDependencies, path)
		return nil
	}, false)
}
func goRequests(m Manifest, packages []LockedPackage) []GoRequest {
	requests := []GoRequest{}
	appendRequests := func(source string, deps map[string]string) {
		for _, path := range names(deps) {
			requests = append(requests, GoRequest{Source: source, Path: path, Query: deps[path]})
		}
	}
	appendRequests("root", m.GoDependencies)
	for _, p := range packages {
		appendRequests("package:"+p.Namespace, p.GoDependencies)
	}
	sort.Slice(requests, func(i, j int) bool {
		if requests[i].Source == requests[j].Source {
			return requests[i].Path < requests[j].Path
		}
		return requests[i].Source < requests[j].Source
	})
	return requests
}
func goIntegrity(l *GoLock) string {
	copy := *l
	copy.Integrity = ""
	b, _ := json.Marshal(copy)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
func validGoSum(sum string) bool {
	if !strings.HasPrefix(sum, "h1:") {
		return false
	}
	b, e := base64.StdEncoding.DecodeString(strings.TrimPrefix(sum, "h1:"))
	return e == nil && len(b) == sha256.Size
}
func normalizeSums(data []byte) (string, error) {
	seen := map[string]string{}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 3 {
			return "", fmt.Errorf("invalid Go checksum line")
		}
		version := strings.TrimSuffix(fields[1], "/go.mod")
		if e := module.Check(fields[0], version); e != nil {
			return "", fmt.Errorf("invalid Go checksum module: %w", e)
		}
		if !validGoSum(fields[2]) {
			return "", fmt.Errorf("invalid Go checksum for %s", fields[0])
		}
		key := fields[0] + " " + fields[1]
		if old, ok := seen[key]; ok && old != fields[2] {
			return "", fmt.Errorf("conflicting Go checksums for %s", key)
		}
		seen[key] = fields[2]
	}
	var out strings.Builder
	for _, key := range names(seen) {
		fmt.Fprintf(&out, "%s %s\n", key, seen[key])
	}
	return out.String(), nil
}
func validateGoLock(m Manifest, l Lock) error {
	wanted := goRequests(m, l.Packages)
	if len(wanted) == 0 {
		if l.Go != nil {
			return fmt.Errorf("native Go lock contains undeclared dependencies; run mojave update")
		}
		return nil
	}
	if l.Go == nil {
		return fmt.Errorf("native Go dependencies are not locked; run mojave install or mojave update")
	}
	locked := l.Go
	if locked.Integrity != goIntegrity(locked) {
		return fmt.Errorf("native Go lock integrity mismatch; restore mojave.lock or resolve explicitly with mojave update")
	}
	if !goversion.IsValid("go"+locked.GoVersion) || goversion.Compare(goversion.Lang("go"+locked.GoVersion), goversion.Lang(runtime.Version())) > 0 {
		return fmt.Errorf("native Go lock requires unsupported Go %s", locked.GoVersion)
	}
	if len(locked.Requests) != len(wanted) {
		return fmt.Errorf("native Go lock requirements do not match manifest")
	}
	modules := map[string]GoModule{}
	for _, entry := range locked.Modules {
		if e := module.Check(entry.Path, entry.Version); e != nil {
			return fmt.Errorf("invalid locked Go module: %w", e)
		}
		if _, ok := modules[entry.Path]; ok {
			return fmt.Errorf("duplicate locked Go module %s", entry.Path)
		}
		if !validGoSum(entry.Sum) || !validGoSum(entry.GoModSum) {
			return fmt.Errorf("missing Go checksums for %s", entry.Path)
		}
		modules[entry.Path] = entry
	}
	for i, request := range wanted {
		actual := locked.Requests[i]
		if e := validateGoDependency(request.Path, request.Query); e != nil {
			return e
		}
		if actual.Source != request.Source || actual.Path != request.Path || actual.Query != request.Query {
			return fmt.Errorf("native Go lock requirements do not match manifest")
		}
		if e := module.Check(actual.Path, actual.Version); e != nil {
			return e
		}
		selected, ok := modules[actual.Path]
		if !ok || semver.Compare(selected.Version, actual.Version) < 0 {
			return fmt.Errorf("native Go graph does not satisfy %s@%s", actual.Path, actual.Version)
		}
	}
	sums, e := normalizeSums([]byte(locked.Checksums))
	if e != nil {
		return e
	}
	if sums != locked.Checksums {
		return fmt.Errorf("native Go checksums are not canonical")
	}
	for _, entry := range locked.Modules {
		for _, line := range []string{entry.Path + " " + entry.Version + " " + entry.Sum + "\n", entry.Path + " " + entry.Version + "/go.mod " + entry.GoModSum + "\n"} {
			if !strings.Contains("\n"+sums, "\n"+line) {
				return fmt.Errorf("native Go checksum missing for %s", entry.Path)
			}
		}
	}
	return nil
}

// GoModuleFiles is read-only and never resolves or downloads dependencies.
// found identifies a locked native graph, including requirements of Ghi libraries.
func GoModuleFiles(root string) (mod, sum []byte, found bool, err error) {
	root, err = filepath.Abs(root)
	if err != nil {
		return nil, nil, false, err
	}
	if _, err = os.Stat(filepath.Join(root, "mojave.json")); os.IsNotExist(err) {
		return nil, nil, false, nil
	} else if err != nil {
		return nil, nil, false, err
	}
	m, err := readManifest(root)
	if err != nil {
		return nil, nil, false, err
	}
	if len(m.Dependencies) == 0 && len(m.GoDependencies) == 0 {
		if _, err = os.Stat(filepath.Join(root, "mojave.lock")); os.IsNotExist(err) {
			return nil, nil, false, nil
		}
	}
	l, err := readLock(root, m)
	if err != nil {
		return nil, nil, len(m.GoDependencies) > 0, err
	}
	if l.Go == nil {
		return nil, nil, false, nil
	}
	mod, err = goModBytes(l.Go.GoVersion, l.Go.Modules)
	return mod, []byte(l.Go.Checksums), true, err
}
func goModBytes(version string, modules []GoModule) ([]byte, error) {
	f := new(modfile.File)
	if e := f.AddModuleStmt("ghi.generated"); e != nil {
		return nil, e
	}
	if e := f.AddGoStmt(version); e != nil {
		return nil, e
	}
	sorted := append([]GoModule(nil), modules...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Path < sorted[j].Path })
	for _, m := range sorted {
		if e := f.AddRequire(m.Path, m.Version); e != nil {
			return nil, e
		}
	}
	f.SortBlocks()
	return f.Format()
}
func runGo(ctx context.Context, goPath, dir string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, goPath, args...)
	cmd.Dir = dir
	cmd.Env = toolchain.Env()
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	b, e := cmd.Output()
	if e != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("Mojave Go %s: %w\n%s", strings.Join(args, " "), e, stderr.String())
	}
	return b, nil
}

type goModuleInfo struct {
	Path, Version, Sum, GoModSum string
	Main                         bool
	Replace                      *goModuleInfo
	Error                        any
}

func decodeGoModules(data []byte) ([]goModuleInfo, error) {
	var result []goModuleInfo
	d := json.NewDecoder(bytes.NewReader(data))
	for {
		var entry goModuleInfo
		e := d.Decode(&entry)
		if e == io.EOF {
			return result, nil
		}
		if e != nil {
			return nil, e
		}
		if entry.Error != nil {
			return nil, fmt.Errorf("Go module %s: %v", entry.Path, entry.Error)
		}
		if entry.Replace != nil {
			return nil, fmt.Errorf("Go replacements are unsupported in Mojave locks")
		}
		if !entry.Main {
			result = append(result, entry)
		}
	}
}

// Go's pruned module graph depends on which requirements are graph roots.
// Our generated module pins every selected module, so resolve against that
// same root set until promoting indirect requirements stops changing MVS.
func stableGoGraph(ctx context.Context, goPath, dir, version string, required []GoModule) ([]goModuleInfo, error) {
	for round := 0; round < 64; round++ {
		mod, e := goModBytes(version, required)
		if e != nil {
			return nil, e
		}
		if e = os.WriteFile(filepath.Join(dir, "go.mod"), mod, 0644); e != nil {
			return nil, e
		}
		output, e := runGo(ctx, goPath, dir, "list", "-m", "-mod=mod", "-json", "all")
		if e != nil {
			return nil, e
		}
		graph, e := decodeGoModules(output)
		if e != nil {
			return nil, e
		}
		current := map[string]string{}
		for _, entry := range required {
			current[entry.Path] = entry.Version
		}
		stable := len(current) == len(graph)
		for _, entry := range graph {
			if current[entry.Path] != entry.Version {
				stable = false
			}
		}
		if stable {
			return graph, nil
		}
		actual, e := os.ReadFile(filepath.Join(dir, "go.mod"))
		if e != nil {
			return nil, e
		}
		parsed, e := modfile.Parse("go.mod", actual, nil)
		if e != nil {
			return nil, e
		}
		version = parsed.Go.Version
		required = make([]GoModule, 0, len(graph))
		for _, entry := range graph {
			required = append(required, GoModule{Path: entry.Path, Version: entry.Version})
		}
	}
	return nil, fmt.Errorf("Go dependency graph did not stabilize after 64 resolution rounds")
}
func resolveGo(ctx context.Context, m Manifest, packages []LockedPackage, old *Lock) (*GoLock, error) {
	requests := goRequests(m, packages)
	if len(requests) == 0 {
		return nil, nil
	}
	goPath, e := (toolchain.Manager{}).Ensure(ctx)
	if e != nil {
		return nil, e
	}
	dir, e := os.MkdirTemp("", "mojave-go-resolve-")
	if e != nil {
		return nil, e
	}
	defer os.RemoveAll(dir)
	version := strings.TrimPrefix(toolchain.MinimumVersion, "go")
	initial, _ := goModBytes(version, nil)
	if e = os.WriteFile(filepath.Join(dir, "go.mod"), initial, 0644); e != nil {
		return nil, e
	}
	previous := map[string]GoRequest{}
	if old != nil && old.Go != nil {
		for _, r := range old.Go.Requests {
			previous[r.Source+"\x00"+r.Path] = r
		}
	}
	selected := map[string]string{}
	for i := range requests {
		r := &requests[i]
		if e = validateGoDependency(r.Path, r.Query); e != nil {
			return nil, e
		}
		prior, ok := previous[r.Source+"\x00"+r.Path]
		if ok && prior.Query == r.Query {
			r.Version = prior.Version
		} else {
			output, e := runGo(ctx, goPath, dir, "list", "-m", "-json", r.Path+"@"+r.Query)
			if e != nil {
				return nil, e
			}
			var resolved goModuleInfo
			if e = json.Unmarshal(output, &resolved); e != nil {
				return nil, e
			}
			if resolved.Path != r.Path || resolved.Error != nil {
				return nil, fmt.Errorf("cannot resolve Go module %s@%s", r.Path, r.Query)
			}
			if e = module.Check(r.Path, resolved.Version); e != nil {
				return nil, e
			}
			r.Version = resolved.Version
		}
		if current := selected[r.Path]; current == "" || semver.Compare(r.Version, current) > 0 {
			selected[r.Path] = r.Version
		}
	}
	required := []GoModule{}
	for _, path := range names(selected) {
		required = append(required, GoModule{Path: path, Version: selected[path]})
	}
	if old != nil && old.Go != nil {
		if e = os.WriteFile(filepath.Join(dir, "go.sum"), []byte(old.Go.Checksums), 0644); e != nil {
			return nil, e
		}
	}
	graph, e := stableGoGraph(ctx, goPath, dir, version, required)
	if e != nil {
		return nil, e
	}
	output, e := runGo(ctx, goPath, dir, "mod", "download", "-json", "all")
	if e != nil {
		return nil, e
	}
	downloads, e := decodeGoModules(output)
	if e != nil {
		return nil, e
	}
	checks := map[string]goModuleInfo{}
	for _, entry := range downloads {
		checks[entry.Path+"@"+entry.Version] = entry
	}
	locked := &GoLock{Requests: requests, Modules: []GoModule{}}
	for _, entry := range graph {
		download, ok := checks[entry.Path+"@"+entry.Version]
		if !ok {
			return nil, fmt.Errorf("missing downloaded checksum for %s@%s", entry.Path, entry.Version)
		}
		locked.Modules = append(locked.Modules, GoModule{Path: entry.Path, Version: entry.Version, Sum: download.Sum, GoModSum: download.GoModSum})
	}
	sort.Slice(locked.Modules, func(i, j int) bool { return locked.Modules[i].Path < locked.Modules[j].Path })
	actualMod, e := os.ReadFile(filepath.Join(dir, "go.mod"))
	if e != nil {
		return nil, e
	}
	parsed, e := modfile.Parse("go.mod", actualMod, nil)
	if e != nil {
		return nil, e
	}
	locked.GoVersion = parsed.Go.Version
	sums, e := os.ReadFile(filepath.Join(dir, "go.sum"))
	if e != nil {
		return nil, e
	}
	locked.Checksums, e = normalizeSums(sums)
	if e != nil {
		return nil, e
	}
	locked.Integrity = goIntegrity(locked)
	if e = validateGoLock(m, Lock{Packages: packages, Go: locked}); e != nil {
		return nil, e
	}
	return locked, nil
}
func installGo(ctx context.Context, l *GoLock) error {
	if l == nil {
		return nil
	}
	goPath, e := (toolchain.Manager{}).Ensure(ctx)
	if e != nil {
		return e
	}
	dir, e := os.MkdirTemp("", "mojave-go-install-")
	if e != nil {
		return e
	}
	defer os.RemoveAll(dir)
	mod, e := goModBytes(l.GoVersion, l.Modules)
	if e != nil {
		return e
	}
	if e = os.WriteFile(filepath.Join(dir, "go.mod"), mod, 0644); e != nil {
		return e
	}
	if e = os.WriteFile(filepath.Join(dir, "go.sum"), []byte(l.Checksums), 0644); e != nil {
		return e
	}
	if _, e = runGo(ctx, goPath, dir, "mod", "download", "all"); e != nil {
		return e
	}
	if _, e = runGo(ctx, goPath, dir, "mod", "verify"); e != nil {
		return e
	}
	output, e := runGo(ctx, goPath, dir, "list", "-m", "-mod=readonly", "-json", "all")
	if e != nil {
		return e
	}
	graph, e := decodeGoModules(output)
	if e != nil {
		return e
	}
	selected := map[string]string{}
	for _, entry := range graph {
		selected[entry.Path] = entry.Version
	}
	if len(selected) != len(l.Modules) {
		return fmt.Errorf("locked Go graph changed during install; run mojave update explicitly")
	}
	for _, entry := range l.Modules {
		if selected[entry.Path] != entry.Version {
			return fmt.Errorf("locked Go version changed for %s", entry.Path)
		}
	}
	return nil
}
