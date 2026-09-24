// Package mojave installs Git-distributed Ghi libraries from a reproducible lock.
package mojave

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

type Dependency struct {
	Repository string `json:"repository"`
	Ref        string `json:"ref"`
}
type Manifest struct {
	Version      int                   `json:"version"`
	Dependencies map[string]Dependency `json:"dependencies"`
}
type LockedPackage struct {
	Namespace    string                `json:"namespace"`
	Repository   string                `json:"repository"`
	Ref          string                `json:"ref"`
	Commit       string                `json:"commit"`
	Integrity    string                `json:"integrity"`
	Dependencies map[string]Dependency `json:"dependencies,omitempty"`
}
type Lock struct {
	Version      int             `json:"version"`
	ManifestHash string          `json:"manifestHash"`
	Packages     []LockedPackage `json:"packages"`
}
type SourceRoot struct {
	Namespace string
	Path      string
}

var namespaceRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*(\.[A-Za-z_][A-Za-z0-9_]*)*$`)
var refRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]*$`)
var commitRE = regexp.MustCompile(`^[a-f0-9]{40}([a-f0-9]{24})?$`)

func validateName(name string) error {
	if !namespaceRE.MatchString(name) || strings.EqualFold(strings.Split(name, ".")[0], "main") || strings.EqualFold(strings.Split(name, ".")[0], "ghi") {
		return fmt.Errorf("invalid package namespace %q: use dotted identifiers outside main and ghi", name)
	}
	for _, part := range strings.Split(name, ".") {
		upper := strings.ToUpper(part)
		if upper == "CON" || upper == "PRN" || upper == "AUX" || upper == "NUL" || regexp.MustCompile(`^(COM|LPT)[0-9]$`).MatchString(upper) {
			return fmt.Errorf("namespace %q is reserved by the filesystem", name)
		}
	}
	return nil
}
func validateDependency(d Dependency) error {
	if d.Repository == "" || strings.HasPrefix(d.Repository, "-") || strings.ContainsAny(d.Repository, "\x00\r\n") || strings.Contains(d.Repository, "::") {
		return fmt.Errorf("invalid Git repository %q", d.Repository)
	}
	if strings.Contains(d.Repository, "://") && !strings.HasPrefix(d.Repository, "https://") && !strings.HasPrefix(d.Repository, "ssh://") && !strings.HasPrefix(d.Repository, "file://") {
		return fmt.Errorf("repository must use HTTPS, SSH, file://, or a local Git path")
	}
	if !refRE.MatchString(d.Ref) || strings.Contains(d.Ref, "..") || strings.Contains(d.Ref, "//") || strings.HasSuffix(d.Ref, "/") {
		return fmt.Errorf("invalid Git ref %q: use a tag, branch, or commit", d.Ref)
	}
	return nil
}
func normalizeRepository(base, repository string) string {
	if strings.Contains(repository, "://") || strings.HasPrefix(repository, "git@") {
		return repository
	}
	if filepath.IsAbs(repository) {
		return filepath.Clean(repository)
	}
	return filepath.Join(base, repository)
}
func readJSON(path string, v any) error {
	f, e := os.Open(path)
	if e != nil {
		return e
	}
	defer f.Close()
	d := json.NewDecoder(io.LimitReader(f, 4<<20))
	d.DisallowUnknownFields()
	if e = d.Decode(v); e != nil {
		return fmt.Errorf("%s: %w", path, e)
	}
	var extra any
	if e = d.Decode(&extra); e != io.EOF {
		return fmt.Errorf("%s: expected one JSON object", path)
	}
	return nil
}
func readManifest(root string) (Manifest, error) {
	m := Manifest{Version: 1, Dependencies: map[string]Dependency{}}
	e := readJSON(filepath.Join(root, "mojave.json"), &m)
	if os.IsNotExist(e) {
		return m, nil
	}
	if e != nil {
		return m, e
	}
	if m.Version != 1 {
		return m, fmt.Errorf("unsupported mojave.json version %d", m.Version)
	}
	for name, d := range m.Dependencies {
		if e = validateName(name); e != nil {
			return m, e
		}
		if e = validateDependency(d); e != nil {
			return m, fmt.Errorf("package %s: %w", name, e)
		}
	}
	return m, nil
}
func manifestHash(m Manifest) string {
	b, _ := json.Marshal(m)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
func names[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
func readLock(root string, m Manifest) (Lock, error) {
	var l Lock
	e := readJSON(filepath.Join(root, "mojave.lock"), &l)
	if e != nil {
		return l, fmt.Errorf("read mojave.lock (run mojave install): %w", e)
	}
	if l.Version != 1 || l.ManifestHash != manifestHash(m) {
		return l, fmt.Errorf("mojave.lock does not match mojave.json; run mojave update to resolve explicitly")
	}
	seen := map[string]bool{}
	for _, p := range l.Packages {
		if e = validateName(p.Namespace); e != nil {
			return l, e
		}
		key := strings.ToLower(p.Namespace)
		if seen[key] {
			return l, fmt.Errorf("duplicate locked namespace %s", p.Namespace)
		}
		seen[key] = true
		if e = validateDependency(Dependency{p.Repository, p.Ref}); e != nil {
			return l, e
		}
		if !commitRE.MatchString(p.Commit) || len(p.Integrity) != 64 {
			return l, fmt.Errorf("invalid locked commit or integrity for %s", p.Namespace)
		}
		if _, e = hex.DecodeString(p.Integrity); e != nil {
			return l, fmt.Errorf("invalid integrity for %s", p.Namespace)
		}
	}
	// The lock must contain exactly the dependency graph declared by its manifests.
	packages := map[string]LockedPackage{}
	for _, p := range l.Packages {
		packages[p.Namespace] = p
	}
	visited := map[string]bool{}
	var visit func(string, Dependency) error
	visit = func(n string, d Dependency) error {
		p, ok := packages[n]
		if !ok || p.Repository != normalizeRepository(root, d.Repository) || p.Ref != d.Ref {
			return fmt.Errorf("lock dependency mismatch for %s; run mojave update", n)
		}
		if visited[n] {
			return nil
		}
		visited[n] = true
		for _, child := range names(p.Dependencies) {
			if e := visit(child, p.Dependencies[child]); e != nil {
				return e
			}
		}
		return nil
	}
	for _, n := range names(m.Dependencies) {
		if e = visit(n, m.Dependencies[n]); e != nil {
			return l, e
		}
	}
	if len(visited) != len(packages) {
		return l, fmt.Errorf("lock contains unreachable packages; run mojave update")
	}
	return l, nil
}

// SourceRoots validates all installed content before the compiler consumes it.
// Projects without dependencies do not require a manifest or lock file.
func SourceRoots(root string) ([]SourceRoot, error) {
	root, e := filepath.Abs(root)
	if e != nil {
		return nil, e
	}
	m, e := readManifest(root)
	if e != nil {
		return nil, e
	}
	if len(m.Dependencies) == 0 {
		return nil, nil
	}
	if e = checkSafeDir(filepath.Join(root, ".ghi")); e != nil {
		return nil, e
	}
	if e = checkSafeDir(filepath.Join(root, ".ghi", "packages")); e != nil {
		return nil, e
	}
	if _, e = os.Stat(filepath.Join(root, ".ghi", "mojave-operation")); e == nil {
		return nil, fmt.Errorf("Mojave operation in progress; retry after it completes")
	}
	l, e := readLock(root, m)
	if e != nil {
		return nil, e
	}
	out := make([]SourceRoot, 0, len(l.Packages))
	for _, p := range l.Packages {
		path := filepath.Join(root, ".ghi", "packages", p.Namespace)
		if e = verify(path, p.Integrity); e != nil {
			return nil, fmt.Errorf("package %s: %w; restore the cache with mojave install after removing the damaged package directory", p.Namespace, e)
		}
		out = append(out, SourceRoot{p.Namespace, path})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Namespace < out[j].Namespace })
	return out, nil
}

func Add(ctx context.Context, root, name, repository, ref string) error {
	if ref == "" {
		ref = "HEAD"
	}
	if e := validateName(name); e != nil {
		return e
	}
	d := Dependency{repository, ref}
	if e := validateDependency(d); e != nil {
		return e
	}
	return mutate(ctx, root, func(m *Manifest) error {
		if m.Dependencies == nil {
			m.Dependencies = map[string]Dependency{}
		}
		m.Dependencies[name] = d
		return nil
	}, false)
}
func Remove(ctx context.Context, root, name string) error {
	return mutate(ctx, root, func(m *Manifest) error {
		if _, ok := m.Dependencies[name]; !ok {
			return fmt.Errorf("%s is not a direct dependency", name)
		}
		delete(m.Dependencies, name)
		return nil
	}, false)
}
func Update(ctx context.Context, root string) error { return mutate(ctx, root, nil, true) }
func Install(ctx context.Context, root string) error {
	return operation(ctx, root, func(root string) error {
		m, e := readManifest(root)
		if e != nil {
			return e
		}
		if _, e = os.Stat(filepath.Join(root, "mojave.lock")); os.IsNotExist(e) {
			return resolve(ctx, root, m, nil)
		}
		l, e := readLock(root, m)
		if e != nil {
			return e
		}
		for _, p := range l.Packages {
			path := filepath.Join(root, ".ghi", "packages", p.Namespace)
			if _, e = os.Lstat(path); e == nil {
				if e = verify(path, p.Integrity); e != nil {
					return fmt.Errorf("package %s: %w", p.Namespace, e)
				}
				continue
			} else if !os.IsNotExist(e) {
				return e
			}
			stage, e := os.MkdirTemp(filepath.Join(root, ".ghi"), "mojave-install-")
			if e != nil {
				return e
			}
			_, e = export(ctx, p.Repository, p.Commit, stage)
			if e == nil {
				e = verify(stage, p.Integrity)
			}
			if e == nil {
				e = os.Rename(stage, path)
			}
			os.RemoveAll(stage)
			if e != nil {
				return fmt.Errorf("install %s at %s: %w", p.Namespace, p.Commit, e)
			}
		}
		return nil
	})
}
func mutate(ctx context.Context, root string, change func(*Manifest) error, update bool) error {
	return operation(ctx, root, func(root string) error {
		m, e := readManifest(root)
		if e != nil {
			return e
		}
		var old *Lock
		if _, e = os.Stat(filepath.Join(root, "mojave.lock")); e == nil {
			l, e := readLock(root, m)
			if e != nil && !update {
				return e
			}
			if e == nil && !update {
				old = &l
			}
		} else if !os.IsNotExist(e) {
			return e
		}
		if change != nil {
			if e = change(&m); e != nil {
				return e
			}
		}
		return resolve(ctx, root, m, old)
	})
}
func operation(ctx context.Context, root string, fn func(string) error) error {
	if e := ctx.Err(); e != nil {
		return e
	}
	root, e := filepath.Abs(root)
	if e != nil {
		return e
	}
	cache := filepath.Join(root, ".ghi")
	if e = checkSafeDir(cache); e != nil {
		return e
	}
	if e = os.MkdirAll(cache, 0755); e != nil {
		return e
	}
	guard := filepath.Join(cache, "mojave-operation")
	if e = os.Mkdir(guard, 0700); e != nil {
		return fmt.Errorf("another Mojave operation is active (if interrupted, remove %s): %w", guard, e)
	}
	defer os.Remove(guard)
	if e = checkSafeDir(filepath.Join(cache, "packages")); e != nil {
		return e
	}
	if e = os.MkdirAll(filepath.Join(cache, "packages"), 0755); e != nil {
		return e
	}
	return fn(root)
}
func checkSafeDir(path string) error {
	info, e := os.Lstat(path)
	if os.IsNotExist(e) {
		return nil
	}
	if e != nil {
		return e
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("unsafe package directory %s", path)
	}
	return nil
}

func resolve(ctx context.Context, root string, m Manifest, old *Lock) error {
	stage, e := os.MkdirTemp(filepath.Join(root, ".ghi"), "mojave-resolve-")
	if e != nil {
		return e
	}
	defer os.RemoveAll(stage)
	locked := map[string]LockedPackage{}
	if old != nil {
		for _, p := range old.Packages {
			locked[p.Namespace] = p
		}
	}
	selected := map[string]LockedPackage{}
	claimed := map[string]string{}
	var visit func(string, Dependency) error
	visit = func(name string, d Dependency) error {
		if e := ctx.Err(); e != nil {
			return e
		}
		if e := validateName(name); e != nil {
			return e
		}
		if e := validateDependency(d); e != nil {
			return e
		}
		d.Repository = normalizeRepository(root, d.Repository)
		key := strings.ToLower(name)
		for other, original := range claimed {
			if other != key && (strings.HasPrefix(key, other+".") || strings.HasPrefix(other, key+".")) {
				return fmt.Errorf("namespace conflict: %s overlaps %s", name, original)
			}
		}
		if original, ok := claimed[key]; ok && original != name {
			return fmt.Errorf("namespace conflict: %s and %s differ only by case", name, original)
		}
		claimed[key] = name
		if p, ok := selected[name]; ok {
			if p.Repository != d.Repository || p.Ref != d.Ref {
				return fmt.Errorf("dependency conflict for %s: %s@%s versus %s@%s", name, p.Repository, p.Ref, d.Repository, d.Ref)
			}
			return nil
		}
		path := filepath.Join(stage, name)
		if e := os.Mkdir(path, 0755); e != nil {
			return e
		}
		ref := d.Ref
		prev, keep := locked[name]
		keep = keep && prev.Repository == d.Repository && prev.Ref == d.Ref
		if keep {
			ref = prev.Commit
		}
		commit, e := export(ctx, d.Repository, ref, path)
		if e != nil {
			return fmt.Errorf("resolve %s: %w", name, e)
		}
		integrity, e := treeHash(path)
		if e != nil {
			return e
		}
		if keep && integrity != prev.Integrity {
			return fmt.Errorf("package %s integrity mismatch against lock", name)
		}
		child, e := readManifest(path)
		if e != nil {
			return fmt.Errorf("package %s: %w", name, e)
		}
		// Relative transitive repositories resolve beside their containing local repository.
		for n, dep := range child.Dependencies {
			if !filepath.IsAbs(dep.Repository) && !strings.Contains(dep.Repository, "://") && !strings.HasPrefix(dep.Repository, "git@") {
				if strings.Contains(d.Repository, "://") || strings.HasPrefix(d.Repository, "git@") {
					return fmt.Errorf("package %s dependency %s must use an absolute repository URL", name, n)
				}
				dep.Repository = normalizeRepository(filepath.Dir(d.Repository), dep.Repository)
				child.Dependencies[n] = dep
			}
		}
		selected[name] = LockedPackage{name, d.Repository, d.Ref, commit, integrity, child.Dependencies}
		for _, n := range names(child.Dependencies) {
			if e = visit(n, child.Dependencies[n]); e != nil {
				return e
			}
		}
		return nil
	}
	for _, n := range names(m.Dependencies) {
		if e = visit(n, m.Dependencies[n]); e != nil {
			return e
		}
	}
	if e = ctx.Err(); e != nil {
		return e
	}
	l := Lock{Version: 1, ManifestHash: manifestHash(m), Packages: []LockedPackage{}}
	for _, n := range names(selected) {
		l.Packages = append(l.Packages, selected[n])
	}
	// Swap the fully resolved tree, rolling back on metadata write failures.
	cache := filepath.Join(root, ".ghi", "packages")
	backup := stage + "-previous"
	if e = os.Rename(cache, backup); e != nil {
		return e
	}
	if e = os.Rename(stage, cache); e != nil {
		os.Rename(backup, cache)
		return e
	}
	oldManifest, manifestErr := os.ReadFile(filepath.Join(root, "mojave.json"))
	if e = writeJSON(filepath.Join(root, "mojave.json"), m); e == nil {
		e = writeJSON(filepath.Join(root, "mojave.lock"), l)
	}
	if e != nil {
		os.RemoveAll(cache)
		os.Rename(backup, cache)
		if manifestErr == nil {
			os.WriteFile(filepath.Join(root, "mojave.json"), oldManifest, 0644)
		} else if os.IsNotExist(manifestErr) {
			os.Remove(filepath.Join(root, "mojave.json"))
		}
		return e
	}
	return os.RemoveAll(backup)
}
func writeJSON(path string, v any) error {
	b, e := json.MarshalIndent(v, "", "  ")
	if e != nil {
		return e
	}
	f, e := os.CreateTemp(filepath.Dir(path), ".mojave-json-")
	if e != nil {
		return e
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if _, e = f.Write(append(b, '\n')); e != nil {
		f.Close()
		return e
	}
	if e = f.Close(); e != nil {
		return e
	}
	return os.Rename(tmp, path)
}
func treeHash(root string) (string, error) {
	if e := checkSafeDir(root); e != nil {
		return "", e
	}
	h := sha256.New()
	files := 0
	e := filepath.WalkDir(root, func(path string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if d.IsDir() {
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlinks are not supported: %s", path)
		}
		info, e := d.Info()
		if e != nil {
			return e
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("unsupported package file %s", path)
		}
		rel, e := filepath.Rel(root, path)
		if e != nil {
			return e
		}
		fmt.Fprintf(h, "%s\x00%d\x00", filepath.ToSlash(rel), info.Size())
		f, e := os.Open(path)
		if e != nil {
			return e
		}
		_, e = io.Copy(h, f)
		closeErr := f.Close()
		if e != nil {
			return e
		}
		files++
		return closeErr
	})
	if e != nil {
		return "", e
	}
	if files == 0 {
		return "", errors.New("package contains no files")
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
func verify(root, expected string) error {
	actual, e := treeHash(root)
	if e != nil {
		return fmt.Errorf("integrity verification failed: %w", e)
	}
	if actual != expected {
		return fmt.Errorf("integrity mismatch for %s", root)
	}
	return nil
}
