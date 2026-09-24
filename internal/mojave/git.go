package mojave

import (
	"archive/tar"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
)

func git(ctx context.Context, dir string, args ...string) ([]byte, error) {
	args = append([]string{"-c", "core.hooksPath=" + os.DevNull, "-c", "protocol.ext.allow=never", "-c", "core.autocrlf=false"}, args...)
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	for _, v := range os.Environ() {
		upper := strings.ToUpper(v)
		if !strings.HasPrefix(upper, "GIT_") || strings.HasPrefix(upper, "GIT_SSH_COMMAND=") {
			cmd.Env = append(cmd.Env, v)
		}
	}
	cmd.Env = append(cmd.Env, "GIT_TERMINAL_PROMPT=0", "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	b, e := cmd.Output()
	if e != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("git %s: %w: %s", args[len(args)-1], e, strings.TrimSpace(stderr.String()))
	}
	return b, nil
}

// Export uses Git objects directly; it never checks out files or invokes package scripts.
func export(ctx context.Context, repository, ref, destination string) (string, error) {
	if e := validateDependency(Dependency{repository, ref}); e != nil {
		return "", e
	}
	tmp, e := os.MkdirTemp("", "ghi-mojave-git-")
	if e != nil {
		return "", e
	}
	defer os.RemoveAll(tmp)
	bare := filepath.Join(tmp, "repository.git")
	if _, e = git(ctx, tmp, "clone", "--bare", "--no-local", "--", repository, bare); e != nil {
		return "", fmt.Errorf("cannot clone %s (check repository access and Git installation): %w", repository, e)
	}
	b, e := git(ctx, bare, "rev-parse", "--verify", ref+"^{commit}")
	if e != nil {
		return "", fmt.Errorf("cannot resolve %s: %w", ref, e)
	}
	commit := strings.TrimSpace(string(b))
	if !commitRE.MatchString(commit) {
		return "", fmt.Errorf("invalid Git commit %q", commit)
	}
	entries, e := git(ctx, bare, "ls-tree", "-r", commit)
	if e != nil {
		return "", e
	}
	for _, line := range strings.Split(string(entries), "\n") {
		if strings.HasPrefix(line, "160000 ") {
			return "", fmt.Errorf("Git submodules are unsupported; declare transitive packages in mojave.json")
		}
	}
	archive, e := git(ctx, bare, "archive", "--format=tar", commit)
	if e != nil {
		return "", e
	}
	tr := tar.NewReader(bytes.NewReader(archive))
	var total int64
	seen := map[string]bool{}
	for {
		if e = ctx.Err(); e != nil {
			return "", e
		}
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
		if h.Typeflag == tar.TypeXGlobalHeader {
			continue
		}
		name := strings.TrimSuffix(h.Name, "/")
		if name == "" || path.Clean(name) != name || strings.HasPrefix(name, "/") || name == ".." || strings.HasPrefix(name, "../") || strings.ContainsAny(name, "\\:\x00") {
			return "", fmt.Errorf("unsafe archive path %q", h.Name)
		}
		for _, part := range strings.Split(name, "/") {
			base := strings.ToUpper(strings.Split(part, ".")[0])
			device := base == "CON" || base == "PRN" || base == "AUX" || base == "NUL" || (len(base) == 4 && (strings.HasPrefix(base, "COM") || strings.HasPrefix(base, "LPT")) && base[3] >= '0' && base[3] <= '9')
			if device || strings.EqualFold(part, ".git") || strings.HasSuffix(part, ".") || strings.HasSuffix(part, " ") {
				return "", fmt.Errorf("unsafe archive path %q", h.Name)
			}
		}
		key := strings.ToLower(name)
		if seen[key] {
			return "", fmt.Errorf("archive contains colliding paths %s", name)
		}
		seen[key] = true
		file := filepath.Join(destination, filepath.FromSlash(name))
		switch h.Typeflag {
		case tar.TypeDir:
			if e = os.MkdirAll(file, 0755); e != nil {
				return "", e
			}
		case tar.TypeReg, tar.TypeRegA:
			total += h.Size
			if total > 256<<20 {
				return "", fmt.Errorf("package exceeds 256 MiB extracted size limit")
			}
			if e = os.MkdirAll(filepath.Dir(file), 0755); e != nil {
				return "", e
			}
			f, e := os.OpenFile(file, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
			if e != nil {
				return "", e
			}
			_, copyErr := io.Copy(f, tr)
			closeErr := f.Close()
			if copyErr != nil {
				return "", copyErr
			}
			if closeErr != nil {
				return "", closeErr
			}
		default:
			return "", fmt.Errorf("unsupported archive entry %s (symlinks and special files are not allowed)", name)
		}
	}
	return commit, nil
}
