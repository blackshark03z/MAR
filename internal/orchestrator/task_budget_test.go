//go:build windows

package orchestrator

import (
	"testing"
	"time"

	"mar/internal/agent"
	"mar/internal/service"
	"mar/internal/worker"
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

func TestHarnessConvergenceIgnoresCompatibilityCognitionCeilingsButKeepsKernelGuards(t *testing.T) {
	cognitionExhausted := service.TaskConvergenceBudget{
		RemainingModelDecisions:  0,
		RemainingWorkerToolCalls: 0,
		RemainingModelTokens:     0,
		RemainingActiveExecution: time.Minute,
		RemainingAttempts:        1,
		ExhaustedReason:          "model_decision_limit",
	}
	if got := convergenceStopReasonForMode(worker.BrainHarness, cognitionExhausted); got != "" {
		t.Fatalf("external harness was blocked by MAR cognition budget: %q", got)
	}
	if got := convergenceStopReasonForMode(worker.BrainWeb, cognitionExhausted); got != "model_decision_limit" {
		t.Fatalf("web compatibility lost cognition budget guard: %q", got)
	}
	if got := convergenceStopReasonForMode(worker.BrainProvider, cognitionExhausted); got != "model_decision_limit" {
		t.Fatalf("provider compatibility lost cognition budget guard: %q", got)
	}

	for _, tc := range []struct {
		name   string
		budget service.TaskConvergenceBudget
		want   string
	}{
		{name: "no-progress", budget: service.TaskConvergenceBudget{NoProgress: true, RemainingActiveExecution: time.Minute, RemainingAttempts: 1}, want: "no_progress"},
		{name: "active-execution", budget: service.TaskConvergenceBudget{RemainingActiveExecution: 0, RemainingAttempts: 1}, want: "active_execution_limit"},
		{name: "attempt", budget: service.TaskConvergenceBudget{RemainingActiveExecution: time.Minute, RemainingAttempts: 0}, want: "attempt_limit"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := convergenceStopReasonForMode(worker.BrainHarness, tc.budget); got != tc.want {
				t.Fatalf("harness kernel guard=%q want=%q", got, tc.want)
			}
		})
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
