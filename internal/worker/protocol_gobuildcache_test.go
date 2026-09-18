package worker

import (
	"path/filepath"
	"strings"
	"testing"

	"mar/internal/agent"
	"mar/internal/domain"
)

func TestStartRequestRejectsRelativeGoBuildCache(t *testing.T) {
	req := StartRequest{
		Task: domain.Task{
			ID:       "task-relative-cache",
			State:    domain.TaskRunning,
			RunEpoch: 1,
		},
		Attempt: domain.ExecutionAttempt{
			ID:       "attempt-relative-cache",
			TaskID:   "task-relative-cache",
			RunEpoch: 1,
		},
		WorkspacePath: "C:\\workspace",
		Provider:      ProviderConfig{BrainMode: BrainWeb},
		AgentProfile:  agent.Profile{Model: "test-model", BaseInstructions: "test"},
		GoBuildCache:  filepath.Join("relative", "go-build-cache"),
	}
	err := req.Validate()
	if err == nil || !strings.Contains(err.Error(), "shared Go build cache must be an absolute path") {
		t.Fatalf("relative GoBuildCache validation = %v, want absolute-path rejection", err)
	}
}
