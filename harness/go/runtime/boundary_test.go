package runtime

import (
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestRuntimeHasNoExternalDependencies(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	inspected := 0
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		inspected++
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, spec := range file.Imports {
			name, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(strings.Split(name, "/")[0], ".") {
				t.Errorf("%s imports non-standard dependency %s", path, name)
			}
		}
	}
	if inspected == 0 {
		t.Fatal("no production Go sources available for dependency inspection")
	}
}

func TestApplicationRejectsFileIncludes(t *testing.T) {
	if _, err := DecodeApplicationJSON([]byte(`{"include":["external.json"],"entries":[]}`)); err == nil {
		t.Fatal("headless application must reject unsupported file includes")
	}
	if _, err := DecodeApplicationJSON([]byte(`{"entries":[]}`)); err != nil {
		t.Fatalf("valid in-memory application: %v", err)
	}
}
