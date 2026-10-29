package model

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestProductionPackageHasOnlyStandardLibraryDependencies(t *testing.T) {
	// Both go test and rules_go run tests from the package directory.
	// Bazel declares the inspected production sources as test data.
	dir := "."
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	inspected := 0
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		inspected++
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
	if inspected == 0 {
		t.Fatal("no production Go sources available for dependency inspection")
	}
}
