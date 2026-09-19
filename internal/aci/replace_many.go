package aci

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
)

const MaxReplaceManyExactOperations = 16

type ExactReplacement struct {
	Search        string `json:"search"`
	Replacement   string `json:"replacement"`
	ExpectedCount int    `json:"expected_count"`
}

type ReplacementReceipt struct {
	Index         int `json:"index"`
	ExpectedCount int `json:"expected_count"`
	MatchedCount  int `json:"matched_count"`
}

type ReplaceManyResult struct {
	Path         string               `json:"path"`
	BeforeSHA256 string               `json:"before_sha256"`
	FinalSHA256  string               `json:"final_sha256"`
	Replacements []ReplacementReceipt `json:"replacements"`
}

// ReplaceManyExact applies one bounded ordered batch to one file. All exact
// match preconditions are evaluated against an in-memory candidate first;
// atomicWrite is called once only after every replacement has succeeded.
func (r *Runtime) ReplaceManyExact(rel, expectedSHA256 string, replacements []ExactReplacement) (ReplaceManyResult, error) {
	if len(replacements) == 0 {
		return ReplaceManyResult{}, errors.New("at least one exact replacement is required")
	}
	if len(replacements) > MaxReplaceManyExactOperations {
		return ReplaceManyResult{}, fmt.Errorf("replacement batch exceeds maximum of %d", MaxReplaceManyExactOperations)
	}
	path, err := r.resolveExistingForWrite(rel)
	if err != nil {
		return ReplaceManyResult{}, err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return ReplaceManyResult{}, err
	}
	if len(b) > r.cfg.MaxWriteBytes {
		return ReplaceManyResult{}, errors.New("file exceeds configured mutation byte limit")
	}
	before := sha256.Sum256(b)
	beforeHash := hex.EncodeToString(before[:])
	if !strings.EqualFold(expectedSHA256, beforeHash) {
		return ReplaceManyResult{}, errors.New("file revision/hash precondition failed")
	}

	current := string(b)
	receipts := make([]ReplacementReceipt, 0, len(replacements))
	for index, replacement := range replacements {
		if replacement.ExpectedCount <= 0 || replacement.Search == "" {
			return ReplaceManyResult{}, fmt.Errorf("replacement %d: search text and positive expected_count are required", index+1)
		}
		count := strings.Count(current, replacement.Search)
		if count != replacement.ExpectedCount {
			return ReplaceManyResult{}, fmt.Errorf("replacement %d: expected %d exact match(es), found %d", index+1, replacement.ExpectedCount, count)
		}
		current = strings.Replace(current, replacement.Search, replacement.Replacement, replacement.ExpectedCount)
		if len(current) > r.cfg.MaxWriteBytes {
			return ReplaceManyResult{}, fmt.Errorf("replacement %d: updated file exceeds configured mutation byte limit", index+1)
		}
		receipts = append(receipts, ReplacementReceipt{Index: index + 1, ExpectedCount: replacement.ExpectedCount, MatchedCount: count})
	}

	updated := []byte(current)
	if err := atomicWrite(path, updated); err != nil {
		return ReplaceManyResult{}, err
	}
	after := sha256.Sum256(updated)
	return ReplaceManyResult{
		Path:         r.relative(path),
		BeforeSHA256: beforeHash,
		FinalSHA256:  hex.EncodeToString(after[:]),
		Replacements: receipts,
	}, nil
}
