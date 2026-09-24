package main

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"ghi/internal/mojave"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestGoCommandsManageManifest(t *testing.T) {
	const modulePath = "example.test/cli"
	const mod = "module example.test/cli\n\ngo 1.26.0\n"
	var archive bytes.Buffer
	z := zip.NewWriter(&archive)
	for name, source := range map[string]string{"go.mod": mod, "cli.go": "package cli\n"} {
		w, e := z.Create(modulePath + "@v1.0.0/" + name)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = w.Write([]byte(source)); e != nil {
			t.Fatal(e)
		}
	}
	if e := z.Close(); e != nil {
		t.Fatal(e)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/example.test/cli/@v/list":
			w.Write([]byte("v1.0.0\n"))
		case "/example.test/cli/@v/v1.0.0.info", "/example.test/cli/@latest":
			w.Write([]byte(`{"Version":"v1.0.0","Time":"2026-01-01T00:00:00Z"}`))
		case "/example.test/cli/@v/v1.0.0.mod":
			w.Write([]byte(mod))
		case "/example.test/cli/@v/v1.0.0.zip":
			w.Write(archive.Bytes())
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	t.Setenv("GOPROXY", server.URL)
	t.Setenv("GOSUMDB", "off")
	t.Setenv("GOFLAGS", "-modcacherw")
	t.Setenv("GOMODCACHE", t.TempDir())
	root := t.TempDir()
	t.Chdir(root)
	for _, suffix := range []string{"@v1.0.0", ""} {
		if e := run(context.Background(), []string{"add", "go:" + modulePath + suffix}); e != nil {
			t.Fatal(e)
		}
		data, e := os.ReadFile(filepath.Join(root, "mojave.json"))
		if e != nil {
			t.Fatal(e)
		}
		var manifest mojave.Manifest
		if e = json.Unmarshal(data, &manifest); e != nil {
			t.Fatal(e)
		}
		expected := "v1.0.0"
		if suffix == "" {
			expected = "latest"
		}
		if manifest.GoDependencies[modulePath] != expected {
			t.Fatalf("CLI stored incorrect requested version: %s", data)
		}
		if _, _, found, e := mojave.GoModuleFiles(root); e != nil || !found {
			t.Fatalf("CLI add did not lock Go module: %v %v", found, e)
		}
		if e := run(context.Background(), []string{"remove", "go:" + modulePath}); e != nil {
			t.Fatal(e)
		}
		if _, _, found, e := mojave.GoModuleFiles(root); e != nil || found {
			t.Fatalf("remove last Go module left native graph: %v %v", found, e)
		}
	}
	if e := run(context.Background(), []string{"add", "go:../escape@v1.0.0"}); e == nil {
		t.Fatal("invalid Go module accepted")
	}
}
