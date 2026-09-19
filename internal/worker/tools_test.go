package worker

import "testing"

func TestReplaceManyExactCompatibilityContract(t *testing.T) {
	if ReplaceExactToolName != "replace_exact" {
		t.Fatalf("replace_exact compatibility changed: %q", ReplaceExactToolName)
	}
	if ReplaceManyExactToolName != "replace_many_exact" {
		t.Fatalf("replace_many_exact compatibility changed: %q", ReplaceManyExactToolName)
	}
}
