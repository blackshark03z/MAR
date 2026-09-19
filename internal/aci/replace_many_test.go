package aci

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mar/internal/model"
)

func replaceManyHash(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

func TestReplaceManyExactSuccessAndOrderedDependency(t *testing.T) {
	r, root := newTestRuntime(t, nil, false)
	path := filepath.Join(root, "batch.txt")
	const original = "alpha beta gamma"
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := r.ReplaceManyExact("batch.txt", replaceManyHash(original), []ExactReplacement{
		{Search: "alpha", Replacement: "delta", ExpectedCount: 1},
		{Search: "delta beta", Replacement: "epsilon", ExpectedCount: 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "epsilon gamma" {
		t.Fatalf("unexpected final file %q", got)
	}
	if result.BeforeSHA256 != replaceManyHash(original) || result.FinalSHA256 != replaceManyHash("epsilon gamma") || len(result.Replacements) != 2 {
		t.Fatalf("unexpected receipt: %+v", result)
	}
}

func TestReplaceManyExactFailuresDoNotPartiallyWrite(t *testing.T) {
	cases := []struct {
		name         string
		expectedHash string
		replacements []ExactReplacement
	}{
		{name: "starting hash", expectedHash: "wrong", replacements: []ExactReplacement{{Search: "alpha", Replacement: "omega", ExpectedCount: 1}}},
		{name: "ambiguous match", replacements: []ExactReplacement{{Search: "a", Replacement: "x", ExpectedCount: 1}}},
		{name: "missing later match", replacements: []ExactReplacement{{Search: "alpha", Replacement: "omega", ExpectedCount: 1}, {Search: "missing", Replacement: "x", ExpectedCount: 1}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, root := newTestRuntime(t, nil, false)
			path := filepath.Join(root, "batch.txt")
			const original = "alpha beta alpha"
			if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
				t.Fatal(err)
			}
			hash := tc.expectedHash
			if hash == "" {
				hash = replaceManyHash(original)
			}
			if _, err := r.ReplaceManyExact("batch.txt", hash, tc.replacements); err == nil {
				t.Fatal("expected replacement batch to fail")
			}
			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != original {
				t.Fatalf("failed batch partially mutated file: %q", got)
			}
		})
	}
}

func TestReplaceManyExactEnforcesBatchBound(t *testing.T) {
	r, root := newTestRuntime(t, nil, false)
	path := filepath.Join(root, "batch.txt")
	const original = "alpha"
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	replacements := make([]ExactReplacement, MaxReplaceManyExactOperations+1)
	for i := range replacements {
		replacements[i] = ExactReplacement{Search: "alpha", Replacement: "alpha", ExpectedCount: 1}
	}
	if _, err := r.ReplaceManyExact("batch.txt", replaceManyHash(original), replacements); err == nil {
		t.Fatal("oversized replacement batch was accepted")
	}
	got, _ := os.ReadFile(path)
	if string(got) != original {
		t.Fatalf("oversized batch mutated file: %q", got)
	}
}

func TestReplaceManyExactToolMatchesPrimitiveFinalBytesWithOneToolCall(t *testing.T) {
	primitive, primitiveRoot := newTestRuntime(t, nil, false)
	compound, compoundRoot := newTestRuntime(t, nil, false)
	const original = "alpha beta gamma"
	for _, root := range []string{primitiveRoot, compoundRoot} {
		if err := os.WriteFile(filepath.Join(root, "batch.txt"), []byte(original), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	steps := []ExactReplacement{
		{Search: "alpha", Replacement: "delta", ExpectedCount: 1},
		{Search: "beta", Replacement: "epsilon", ExpectedCount: 1},
		{Search: "gamma", Replacement: "zeta", ExpectedCount: 1},
	}

	currentHash := replaceManyHash(original)
	for _, step := range steps {
		args, _ := json.Marshal(map[string]any{
			"path": "batch.txt", "expected_sha256": currentHash,
			"search": step.Search, "replacement": step.Replacement, "expected_count": step.ExpectedCount,
		})
		out, err := primitive.ExecuteTool(context.Background(), model.ToolCall{Name: "replace_exact", Arguments: string(args)})
		if err != nil || !strings.Contains(out, `"ok":true`) {
			t.Fatalf("primitive replace failed: %s %v", out, err)
		}
		var envelope struct {
			Result MutationResult `json:"result"`
		}
		if err := json.Unmarshal([]byte(out), &envelope); err != nil {
			t.Fatal(err)
		}
		currentHash = envelope.Result.AfterHash
	}

	compoundArgs, _ := json.Marshal(map[string]any{
		"path": "batch.txt", "expected_sha256": replaceManyHash(original), "replacements": steps,
	})
	out, err := compound.ExecuteTool(context.Background(), model.ToolCall{Name: "replace_many_exact", Arguments: string(compoundArgs)})
	if err != nil || !strings.Contains(out, `"ok":true`) {
		t.Fatalf("compound replace failed: %s %v", out, err)
	}
	primitiveBytes, _ := os.ReadFile(filepath.Join(primitiveRoot, "batch.txt"))
	compoundBytes, _ := os.ReadFile(filepath.Join(compoundRoot, "batch.txt"))
	if string(primitiveBytes) != string(compoundBytes) {
		t.Fatalf("compound result differs from three primitive tool calls: primitive=%q compound=%q", primitiveBytes, compoundBytes)
	}
}
