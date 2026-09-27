package compiler

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"

	"golang.org/x/mod/sumdb/dirhash"
)

type downloadedModule struct{ Path, Version, Sum, Zip, Dir, Error string }

// Go still resolves and downloads modules and verifies download checksums. The
// selected download list gives exact cache paths; bind them to the locked sums,
// then reread every archive entry and extracted file on every compilation.
func downloadedModules(data, sums []byte) ([]downloadedModule, error) {
	locked := map[string]string{}
	for _, line := range strings.Split(string(sums), "\n") {
		parts := strings.Fields(line)
		if len(parts) == 3 {
			locked[parts[0]+"@"+parts[1]] = parts[2]
		}
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	var modules []downloadedModule
	for {
		var m downloadedModule
		err := dec.Decode(&m)
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read downloaded Go modules: %w", err)
		}
		if m.Error != "" {
			return nil, fmt.Errorf("download Go module %s: %s", m.Path, m.Error)
		}
		expected := locked[m.Path+"@"+m.Version]
		if expected == "" || m.Sum != expected || !strings.HasPrefix(expected, "h1:") {
			return nil, fmt.Errorf("Go module %s@%s checksum does not match mojave.lock", m.Path, m.Version)
		}
		if m.Zip == "" || m.Dir == "" {
			return nil, fmt.Errorf("Go module %s@%s has incomplete download metadata", m.Path, m.Version)
		}
		modules = append(modules, m)
	}
	return modules, nil
}

func verifyDownloadedModules(ctx context.Context, modules []downloadedModule) error {
	// Bound open files/CPU across all modules. A large module's archive entries
	// can run concurrently too; go mod verify processes them serially per module.
	limit := min(16, max(1, runtime.GOMAXPROCS(0)))
	semaphore := make(chan struct{}, limit)
	type job struct {
		module downloadedModule
		zip    bool
	}
	jobs := make([]job, 0, len(modules)*2)
	for _, m := range modules {
		jobs = append(jobs, job{m, true}, job{m, false})
	}
	errors := make([]error, len(jobs))
	parallelIndices(len(jobs), min(4, limit), func(i int) {
		j := jobs[i]
		hash := func(files []string, open func(string) (io.ReadCloser, error)) (string, error) {
			return parallelModuleHash(ctx, files, open, semaphore)
		}
		var digest string
		var err error
		kind := "dir"
		if j.zip {
			kind = "zip"
			digest, err = dirhash.HashZip(j.module.Zip, hash)
		} else {
			digest, err = hashModuleDirectory(ctx, j.module.Dir, j.module.Path+"@"+j.module.Version, hash)
		}
		if err != nil {
			errors[i] = fmt.Errorf("verify Go module %s@%s %s: %w", j.module.Path, j.module.Version, kind, err)
		} else if digest != j.module.Sum {
			errors[i] = fmt.Errorf("Go module %s@%s: %s has been modified", j.module.Path, j.module.Version, kind)
		}
	})
	for _, err := range errors {
		if err != nil {
			return err
		}
	}
	return ctx.Err()
}

// WalkDir uses directory-entry metadata and avoids a separate stat for every
// source file. Names and contents still enter the exact same h1 calculation.
func hashModuleDirectory(ctx context.Context, dir, prefix string, hash dirhash.Hash) (string, error) {
	var files []string
	err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err = ctx.Err(); err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		if path == dir {
			return fmt.Errorf("%s is not a directory", dir)
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		files = append(files, prefix+"/"+filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		return "", err
	}
	return hash(files, func(name string) (io.ReadCloser, error) {
		return os.Open(filepath.Join(dir, filepath.FromSlash(strings.TrimPrefix(name, prefix+"/"))))
	})
}

func parallelIndices(count, workers int, work func(int)) {
	var wg sync.WaitGroup
	for worker := 0; worker < min(count, workers); worker++ {
		wg.Add(1)
		go func(start int) {
			defer wg.Done()
			for i := start; i < count; i += workers {
				work(i)
			}
		}(worker)
	}
	wg.Wait()
}

type contextReader struct {
	ctx context.Context
	io.Reader
}

func (r contextReader) Read(data []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.Reader.Read(data)
}

// Implements x/mod's documented h1 format: sorted lines containing each file's
// SHA-256, two spaces and filename. Completion order never affects the digest.
func parallelModuleHash(ctx context.Context, files []string, open func(string) (io.ReadCloser, error), semaphore chan struct{}) (string, error) {
	files = append([]string{}, files...)
	sort.Strings(files)
	digests := make([][]byte, len(files))
	errors := make([]error, len(files))
	parallelIndices(len(files), cap(semaphore), func(i int) {
		if strings.Contains(files[i], "\n") {
			errors[i] = fmt.Errorf("module filename contains newline")
			return
		}
		select {
		case semaphore <- struct{}{}:
		case <-ctx.Done():
			errors[i] = ctx.Err()
			return
		}
		defer func() { <-semaphore }()
		r, err := open(files[i])
		if err != nil {
			errors[i] = err
			return
		}
		h := sha256.New()
		_, err = io.Copy(h, contextReader{ctx, r})
		closeErr := r.Close()
		if err == nil {
			err = closeErr
		}
		errors[i] = err
		digests[i] = h.Sum(nil)
	})
	h := sha256.New()
	for i, file := range files {
		if errors[i] != nil {
			return "", errors[i]
		}
		fmt.Fprintf(h, "%x  %s\n", digests[i], file)
	}
	return "h1:" + base64.StdEncoding.EncodeToString(h.Sum(nil)), ctx.Err()
}
