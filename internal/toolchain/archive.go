package toolchain

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func archivePath(root, name string) (string, error) {
	if !strings.HasPrefix(name, "go/") || strings.ContainsAny(name, "\\:\x00") {
		return "", fmt.Errorf("unsafe archive path %q", name)
	}
	for _, part := range strings.Split(name, "/") {
		if part == ".." {
			return "", fmt.Errorf("unsafe archive path %q", name)
		}
	}
	local := filepath.FromSlash(name)
	if !filepath.IsLocal(local) {
		return "", fmt.Errorf("unsafe archive path %q", name)
	}
	return filepath.Join(root, local), nil
}

func extract(path, root string) error {
	const maxUnpacked = int64(2 << 30)
	remaining := maxUnpacked
	write := func(name string, mode os.FileMode, size int64, r io.Reader) error {
		target, err := archivePath(root, name)
		if err != nil {
			return err
		}
		if mode.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		if !mode.IsRegular() {
			return fmt.Errorf("archive links and special files are not allowed: %s", name)
		}
		if size < 0 || size > remaining {
			return fmt.Errorf("Go archive exceeds unpacked size limit")
		}
		remaining -= size
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return err
		}
		permissions := os.FileMode(0644)
		if mode&0111 != 0 {
			permissions = 0755
		}
		file, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, permissions)
		if err != nil {
			return err
		}
		n, copyErr := io.Copy(file, io.LimitReader(r, size+1))
		closeErr := file.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
		if n != size {
			return fmt.Errorf("truncated archive entry %s", name)
		}
		return nil
	}
	if strings.HasSuffix(path, ".zip") {
		zipFile, err := zip.OpenReader(path)
		if err != nil {
			return err
		}
		defer zipFile.Close()
		for _, file := range zipFile.File {
			reader, err := file.Open()
			if err != nil {
				return err
			}
			err = write(file.Name, file.Mode(), int64(file.UncompressedSize64), reader)
			reader.Close()
			if err != nil {
				return err
			}
		}
		return nil
	}
	if !strings.HasSuffix(path, ".tar.gz") {
		return fmt.Errorf("unsupported Go archive format")
	}
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	gz, err := gzip.NewReader(file)
	if err != nil {
		return err
	}
	defer gz.Close()
	reader := tar.NewReader(gz)
	for {
		header, err := reader.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeDir {
			return fmt.Errorf("archive links and special files are not allowed: %s", header.Name)
		}
		if err := write(header.Name, header.FileInfo().Mode(), header.Size, reader); err != nil {
			return err
		}
	}
}
