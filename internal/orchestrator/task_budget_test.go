//go:build windows

package orchestrator

import (
	"testing"
	"time"

	"mar/internal/agent"
	"mar/internal/service"
)

func TestTaskWideConvergenceBudgetAccumulatesAcrossReplacementAttempts(t *testing.T) {
	remaining := service.TaskConvergenceBudget{
		RemainingModelDecisions:  5,
		RemainingWorkerToolCalls: 7,
		RemainingModelTokens:     9000,
		RemainingActiveExecution: 2 * time.Minute,
		RemainingAttempts:        1,
	}
	cfg := boundedAgentConfig(agent.Config{}, remaining)
	if cfg.MaxTurns != 5 || cfg.MaxToolCalls != 7 || cfg.MaxTotalTokens != 9000 || cfg.MaxDuration != 2*time.Minute {
		t.Fatalf("replacement did not inherit cumulative residual budget: %+v", cfg)
	}
	if cfg.MaxEpisodePayloadBytes != int(service.DefaultWebEpisodeMaxPayloadBytes) {
		t.Fatalf("episode payload cap=%d want=%d", cfg.MaxEpisodePayloadBytes, service.DefaultWebEpisodeMaxPayloadBytes)
	}
}

func TestTaskWideConvergenceGuardStopsRepeatedNoProgress(t *testing.T) {
	budget := service.TaskConvergenceBudget{NoProgress: true, ExhaustedReason: ""}
	if got := convergenceStopReason(budget); got != "no_progress" {
		t.Fatalf("no-progress convergence guard must stop before another attempt: %q", got)
	}
	budget = service.TaskConvergenceBudget{ExhaustedReason: "attempt_limit"}
	if got := convergenceStopReason(budget); got != "attempt_limit" {
		t.Fatalf("explicit convergence exhaustion reason lost: %q", got)
	}
}
