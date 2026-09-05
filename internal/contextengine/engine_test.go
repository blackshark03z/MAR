package contextengine

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"mar/internal/domain"
)

type fakeRepository struct {
	snapshot RepositorySnapshot
	err      error
}

func (f fakeRepository) Snapshot(context.Context, string) (RepositorySnapshot, error) {
	if f.err != nil {
		return RepositorySnapshot{}, f.err
	}
	return f.snapshot, nil
}

func TestEngineRanksSymbolContextDeterministically(t *testing.T) {
	root := t.TempDir()
	writeContextFile(t, root, "go.mod", "module example.com/project\n\ngo 1.27.0\n")
	writeContextFile(t, root, "internal/scheduler/retry.go", "package scheduler\n\nfunc RetryTask() error {\n\treturn nil\n}\n")
	writeContextFile(t, root, "README.md", "The retry task flow is documented here.\n")
	writeContextFile(t, root, "internal/other/other.go", "package other\n\nfunc Unrelated() {}\n")
	repo := fakeRepository{snapshot: RepositorySnapshot{
		Revision: "abc123",
		Files: []RepositoryFile{
			{Path: "README.md", Status: "clean"},
			{Path: "go.mod", Status: "clean"},
			{Path: "internal/other/other.go", Status: "clean"},
			{Path: "internal/scheduler/retry.go", Status: "modified"},
		},
	}}
	engine, err := New(repo, Config{MaxPackBytes: 8 << 10, MaxEntries: 4})
	if err != nil {
		t.Fatal(err)
	}
	req := Request{Root: root, Contract: testContract("Fix RetryTask scheduler retry behavior", "abc123"), ExpectedRevision: "abc123"}
	first, err := engine.Build(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	second, err := engine.Build(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("same revision/content produced nondeterministic packs:\nfirst=%+v\nsecond=%+v", first, second)
	}
	if len(first.Entries) == 0 || first.Entries[0].Path != "internal/scheduler/retry.go" {
		t.Fatalf("expected symbol-bearing scheduler file first, got %+v", first.Entries)
	}
	if !containsReasonPrefix(first.Entries[0].Reasons, "symbol:RetryTask") {
		t.Fatalf("expected symbol reason, got %v", first.Entries[0].Reasons)
	}
	if first.Revision != "abc123" || first.GoalHash == "" {
		t.Fatalf("missing revision/goal identity: %+v", first)
	}
	if first.Bytes != len(first.Render()) || first.Bytes > 8<<10 {
		t.Fatalf("pack byte accounting invalid: bytes=%d rendered=%d", first.Bytes, len(first.Render()))
	}
}

func TestEngineAddsLocalGoDependencyContext(t *testing.T) {
	root := t.TempDir()
	writeContextFile(t, root, "go.mod", "module example.com/project\n\ngo 1.27.0\n")
	writeContextFile(t, root, "cmd/app/main.go", "package main\n\nimport \"example.com/project/internal/worker\"\n\nfunc StartupCommand() { worker.Run() }\n")
	writeContextFile(t, root, "internal/worker/worker.go", "package worker\n\nfunc Run() {}\n")
	writeContextFile(t, root, "internal/worker/state.go", "package worker\n\ntype State struct{}\n")
	repo := fakeRepository{snapshot: RepositorySnapshot{Revision: "dep123", Files: []RepositoryFile{
		{Path: "go.mod", Status: "clean"},
		{Path: "cmd/app/main.go", Status: "clean"},
		{Path: "internal/worker/worker.go", Status: "clean"},
		{Path: "internal/worker/state.go", Status: "clean"},
	}}}
	engine, err := New(repo, Config{MaxPackBytes: 8 << 10, MaxEntries: 6})
	if err != nil {
		t.Fatal(err)
	}
	pack, err := engine.Build(context.Background(), Request{Root: root, Contract: testContract("Repair StartupCommand behavior", "dep123"), ExpectedRevision: "dep123"})
	if err != nil {
		t.Fatal(err)
	}
	foundDependency := false
	for _, entry := range pack.Entries {
		if strings.HasPrefix(entry.Path, "internal/worker/") && containsReasonPrefix(entry.Reasons, "dependency:example.com/project/internal/worker") {
			foundDependency = true
			break
		}
	}
	if !foundDependency {
		t.Fatalf("local import dependency was not added to context pack: %+v", pack.Entries)
	}
}

func TestEngineRejectsUnsafePathMetadataAndSkipsBinaryText(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	writeContextFile(t, root, "safe.txt", "safe context needle\n")
	if err := os.WriteFile(filepath.Join(root, "binary.dat"), []byte{0xff, 0xfe, 0x00, 0x01}, 0o644); err != nil {
		t.Fatal(err)
	}
	writeContextFile(t, outside, "escape.txt", "needle outside\n")
	engine, err := New(fakeRepository{snapshot: RepositorySnapshot{Revision: "safe", Files: []RepositoryFile{
		{Path: "safe.txt", Status: "clean"},
		{Path: "binary.dat", Status: "modified"},
		{Path: "../" + filepath.Base(outside) + "/escape.txt", Status: "modified"},
		{Path: "bad\ninjected.txt", Status: "modified"},
	}}}, Config{MaxEntries: 8})
	if err != nil {
		t.Fatal(err)
	}
	pack, err := engine.Build(context.Background(), Request{Root: root, Contract: testContract("safe context needle", "safe"), ExpectedRevision: "safe"})
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range pack.Entries {
		if entry.Path != "safe.txt" {
			t.Fatalf("unsafe/binary path entered context pack: %+v", entry)
		}
	}
	if len(pack.Entries) != 1 {
		t.Fatalf("expected exactly one safe text entry, got %+v", pack.Entries)
	}
}

func TestEngineRevisionMismatchFailsClosed(t *testing.T) {
	root := t.TempDir()
	writeContextFile(t, root, "README.md", "context\n")
	engine, err := New(fakeRepository{snapshot: RepositorySnapshot{Revision: "actual", Files: []RepositoryFile{{Path: "README.md"}}}}, Config{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = engine.Build(context.Background(), Request{Root: root, Contract: testContract("Inspect context", "base"), ExpectedRevision: "expected"})
	if !errors.Is(err, ErrRevisionMismatch) {
		t.Fatalf("expected revision mismatch, got %v", err)
	}
}

func TestEngineEnforcesScanAndPackBudgets(t *testing.T) {
	root := t.TempDir()
	files := make([]RepositoryFile, 0, 8)
	for i := 0; i < 8; i++ {
		name := filepath.ToSlash(filepath.Join("pkg", "context_file_"+string(rune('a'+i))+".txt"))
		writeContextFile(t, root, name, strings.Repeat("context engine ranking evidence line\n", 80))
		files = append(files, RepositoryFile{Path: name, Status: "modified"})
	}
	engine, err := New(fakeRepository{snapshot: RepositorySnapshot{Revision: "budget", Files: files}}, Config{
		MaxPackBytes:    1200,
		MaxEntries:      3,
		MaxScanFiles:    4,
		MaxScanBytes:    64 << 10,
		MaxFileBytes:    32 << 10,
		MaxSnippetBytes: 900,
	})
	if err != nil {
		t.Fatal(err)
	}
	pack, err := engine.Build(context.Background(), Request{Root: root, Contract: testContract("context engine ranking", "budget"), ExpectedRevision: "budget"})
	if err != nil {
		t.Fatal(err)
	}
	if !pack.Truncated || pack.ScannedFiles > 4 || len(pack.Entries) > 3 {
		t.Fatalf("budgets were not enforced: %+v", pack)
	}
	if got := len(pack.Render()); got > 1200 || pack.Bytes != got {
		t.Fatalf("rendered pack exceeded byte budget: bytes=%d rendered=%d", pack.Bytes, got)
	}
}

func TestEnginePrioritizesModifiedFileWhenGoalHasNoLexicalMatch(t *testing.T) {
	root := t.TempDir()
	writeContextFile(t, root, "README.md", "repository overview\n")
	writeContextFile(t, root, "pkg/changed.go", "package pkg\n\nfunc Value() int { return 1 }\n")
	engine, err := New(fakeRepository{snapshot: RepositorySnapshot{Revision: "mod", Files: []RepositoryFile{
		{Path: "README.md", Status: "clean"},
		{Path: "pkg/changed.go", Status: "modified"},
	}}}, Config{MaxEntries: 2})
	if err != nil {
		t.Fatal(err)
	}
	pack, err := engine.Build(context.Background(), Request{Root: root, Contract: testContract("zqxv opaque intent", "mod"), ExpectedRevision: "mod"})
	if err != nil {
		t.Fatal(err)
	}
	if len(pack.Entries) == 0 || pack.Entries[0].Path != "pkg/changed.go" {
		t.Fatalf("modified file was not prioritized: %+v", pack.Entries)
	}
}

func TestEngineHardBoundsPackWithOversizedIntentTerms(t *testing.T) {
	root := t.TempDir()
	writeContextFile(t, root, "pkg/relevant.go", "package pkg\n\nfunc RelevantContext() {}\n")
	longTerms := make([]string, 0, 200)
	for i := 0; i < 200; i++ {
		longTerms = append(longTerms, strings.Repeat(string(rune('a'+i%20)), 140)+string(rune('A'+i%26)))
	}
	contract := testContract(strings.Join(longTerms, " ")+" RelevantContext", "oversized")
	engine, err := New(fakeRepository{snapshot: RepositorySnapshot{Revision: "oversized", Files: []RepositoryFile{{Path: "pkg/relevant.go", Status: "modified"}}}}, Config{
		MaxPackBytes:    512,
		MaxEntries:      2,
		MaxSnippetBytes: 256,
		MaxTerms:        32,
	})
	if err != nil {
		t.Fatal(err)
	}
	pack, err := engine.Build(context.Background(), Request{Root: root, Contract: contract, ExpectedRevision: "oversized"})
	if err != nil {
		t.Fatal(err)
	}
	if got := len(pack.Render()); got > 512 || pack.Bytes != got {
		t.Fatalf("hard pack bound violated: bytes=%d rendered=%d terms=%d entries=%d", pack.Bytes, got, len(pack.Terms), len(pack.Entries))
	}
	for _, term := range pack.Terms {
		if len([]rune(term)) > maxContextTermRunes {
			t.Fatalf("oversized intent token leaked into context terms: %q", term)
		}
	}
	if len(pack.Entries) == 0 || pack.Entries[0].Path != "pkg/relevant.go" {
		t.Fatalf("bounded tokenization lost relevant evidence: %+v", pack.Entries)
	}
}

func TestAnalysisCacheIsContentHashBounded(t *testing.T) {
	root := t.TempDir()
	files := []RepositoryFile{}
	for i, name := range []string{"a.go", "b.go", "c.go"} {
		writeContextFile(t, root, name, "package p\n\nfunc ContextSymbol"+string(rune('A'+i))+"() {}\n")
		files = append(files, RepositoryFile{Path: name})
	}
	engine, err := New(fakeRepository{snapshot: RepositorySnapshot{Revision: "cache", Files: files}}, Config{CacheEntries: 2})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Build(context.Background(), Request{Root: root, Contract: testContract("ContextSymbol", "cache"), ExpectedRevision: "cache"}); err != nil {
		t.Fatal(err)
	}
	if got := engine.cache.size(); got > 2 {
		t.Fatalf("analysis cache exceeded configured bound: %d", got)
	}
}

func testContract(goal, base string) domain.GoalContract {
	return domain.GoalContract{
		Goal:                goal,
		Acceptance:          []string{"context pack is correct and bounded"},
		ProjectID:           "project-test",
		BaseRevision:        base,
		VerificationProfile: "test",
		Priority:            "P2",
	}
}

func writeContextFile(t *testing.T, root, rel, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func containsReasonPrefix(reasons []string, want string) bool {
	for _, reason := range reasons {
		if reason == want || strings.HasPrefix(reason, want) {
			return true
		}
	}
	return false
}
