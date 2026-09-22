package architecture_test

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestKernelPackagesDoNotDependOnCognitionPackages(t *testing.T) {
	root := repoRoot(t)
	kernelDirs := []string{
		"internal/integration",
		"internal/processctl",
		"internal/resourcegov",
		"internal/verification",
		"internal/workspace",
	}
	forbidden := []string{
		"mar/internal/agent",
		"mar/internal/contextengine",
		"mar/internal/model",
	}

	for _, rel := range kernelDirs {
		dir := filepath.Join(root, filepath.FromSlash(rel))
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("read kernel package %s: %v", rel, err)
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") {
				continue
			}
			path := filepath.Join(dir, entry.Name())
			file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
			if err != nil {
				t.Fatalf("parse %s: %v", path, err)
			}
			for _, imp := range file.Imports {
				importPath, err := strconv.Unquote(imp.Path.Value)
				if err != nil {
					t.Fatalf("unquote import in %s: %v", path, err)
				}
				for _, blocked := range forbidden {
					if importPath == blocked || strings.HasPrefix(importPath, blocked+"/") {
						t.Errorf("kernel package %s imports cognition package %s in %s", rel, importPath, entry.Name())
					}
				}
			}
		}
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found")
		}
		dir = parent
	}
}
