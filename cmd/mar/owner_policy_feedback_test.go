package main

import (
	"strings"
	"testing"
)

func TestOwnerUIPolicySaveFeedbackIsInProductionBundle(t *testing.T) {
	bundle := ownerUIContractText()
	for _, want := range []string{"Đang lưu...", "Đã lưu quyền", "Không thể lưu quyền"} {
		if !strings.Contains(bundle, want) {
			t.Fatalf("production Owner Console bundle missing policy save feedback %q", want)
		}
	}
}
