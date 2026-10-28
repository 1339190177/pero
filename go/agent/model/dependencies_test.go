package model

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

func TestProductionPackageHasOnlyStandardLibraryDependencies(t *testing.T) {
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate model package source")
	}
	dir := filepath.Dir(source)
	if root, envOK := os.LookupEnv("BUILD_WORKSPACE_DIRECTORY"); envOK && root != "" {
		// Under Bazel the caller path is workspace-relative; resolve it
		// against the real checkout exported by the test workflow.
		dir = filepath.Join(root, "go", "agent", "model")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", entry.Name(), err)
		}
		for _, spec := range file.Imports {
			importPath, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				t.Fatalf("decode import %s in %s: %v", spec.Path.Value, entry.Name(), err)
			}
			if strings.Contains(importPath, ".") {
				t.Errorf("%s imports non-standard package %q; model must remain dependency-light", entry.Name(), importPath)
			}
		}
	}
}
