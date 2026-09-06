package contextengine

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// TestContextRetrievalBenchmarkV1 is a small deterministic localization corpus.
// It intentionally covers direct symbol retrieval and dependency ripple across
// the languages used most often by MAR projects. Keep the corpus frozen once V1
// thresholds are accepted so retrieval work has a finite stop condition.
func TestContextRetrievalBenchmarkV1(t *testing.T) {
	type benchCase struct {
		name  string
		goal  string
		files map[string]string
		gold  []string
	}
	cases := []benchCase{
		{
			name: "python-symbol-over-doc-noise",
			goal: "Fix SessionManager login retry behavior",
			files: map[string]string{
				"docs/session_manager.md": strings.Repeat("SessionManager login retry behavior documentation\n", 24),
				"src/session_manager.py":  "class SessionManager:\n    def login(self):\n        return True\n",
				"src/unrelated.py":        "def unrelated():\n    return 1\n",
			},
			gold: []string{"src/session_manager.py"},
		},
		{
			name: "typescript-symbol-and-dependency-ripple",
			goal: "Repair CheckoutController timeout handling",
			files: map[string]string{
				"docs/checkout.md":       strings.Repeat("CheckoutController timeout handling guide\n", 24),
				"src/checkout.ts":        "import { charge } from './payment_gateway'\n\nexport class CheckoutController {\n  handle(): boolean { return charge() }\n}\n",
				"src/payment_gateway.ts": "export function charge(): boolean { return true }\n",
				"src/other.ts":           "export const other = 1\n",
			},
			gold: []string{"src/checkout.ts", "src/payment_gateway.ts"},
		},
		{
			name: "python-dependency-ripple",
			goal: "Fix SessionManager login flow",
			files: map[string]string{
				"src/session_manager.py": "from .crypto_helpers import verify_signature\n\nclass SessionManager:\n    def login(self, token):\n        return verify_signature(token)\n",
				"src/crypto_helpers.py":  "def verify_signature(token):\n    return bool(token)\n",
				"docs/session.md":        "SessionManager login flow\n",
			},
			gold: []string{"src/session_manager.py", "src/crypto_helpers.py"},
		},
		{
			name: "go-dependency-ripple",
			goal: "Repair StartupCommand behavior",
			files: map[string]string{
				"go.mod":                    "module example.com/brainbench\n\ngo 1.27\n",
				"cmd/app/main.go":           "package main\n\nimport \"example.com/brainbench/internal/worker\"\n\nfunc StartupCommand() { worker.Run() }\n",
				"internal/worker/worker.go": "package worker\n\nfunc Run() {}\n",
			},
			gold: []string{"cmd/app/main.go", "internal/worker/worker.go"},
		},
	}

	var reciprocalRankSum float64
	var recallHits, recallTotal int
	for i, tc := range cases {
		root := t.TempDir()
		files := make([]RepositoryFile, 0, len(tc.files))
		for path, content := range tc.files {
			writeContextFile(t, root, path, content)
			files = append(files, RepositoryFile{Path: path, Status: "clean"})
		}
		revision := fmt.Sprintf("bench-%d", i)
		engine, err := New(fakeRepository{snapshot: RepositorySnapshot{Revision: revision, Files: files}}, Config{MaxPackBytes: 16 << 10, MaxEntries: 6})
		if err != nil {
			t.Fatal(err)
		}
		pack, err := engine.Build(context.Background(), Request{Root: root, Contract: testContract(tc.goal, revision), ExpectedRevision: revision})
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		ranks := make(map[string]int, len(pack.Entries))
		paths := make([]string, 0, len(pack.Entries))
		for rank, entry := range pack.Entries {
			ranks[entry.Path] = rank + 1
			paths = append(paths, entry.Path)
		}
		best := 0
		for _, gold := range tc.gold {
			if rank := ranks[gold]; rank > 0 && (best == 0 || rank < best) {
				best = rank
			}
			if rank := ranks[gold]; rank > 0 && rank <= 3 {
				recallHits++
			}
			recallTotal++
		}
		if best > 0 {
			reciprocalRankSum += 1 / float64(best)
		}
		t.Logf("%s ranked=%v gold=%v", tc.name, paths, tc.gold)
	}
	mrr := reciprocalRankSum / float64(len(cases))
	recallAt3 := float64(recallHits) / float64(recallTotal)
	t.Logf("PROJECT_BRAIN_V1 MRR=%.3f Recall@3=%.3f", mrr, recallAt3)
	if mrr < 0.95 {
		t.Fatalf("Project Brain V1 MRR gate failed: got %.3f want >= 0.950", mrr)
	}
	if recallAt3 < 1.0 {
		t.Fatalf("Project Brain V1 Recall@3 gate failed: got %.3f want 1.000", recallAt3)
	}
}
