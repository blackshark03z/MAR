//go:build windows

package orchestrator

import (
	"mar/internal/agent"
	"mar/internal/service"
)

func convergenceStopReason(budget service.TaskConvergenceBudget) string {
	if budget.NoProgress {
		return "no_progress"
	}
	return budget.ExhaustedReason
}

func boundedAgentConfig(base agent.Config, budget service.TaskConvergenceBudget) agent.Config {
	cfg := base
	turns := cfg.MaxTurns
	if turns <= 0 {
		turns = int(service.DefaultTaskMaxModelDecisions)
	}
	if turns > int(service.DefaultWebEpisodeMaxDecisions) {
		turns = int(service.DefaultWebEpisodeMaxDecisions)
	}
	if int64(turns) > budget.RemainingModelDecisions {
		turns = int(budget.RemainingModelDecisions)
	}
	cfg.MaxTurns = turns
	tools := cfg.MaxToolCalls
	if tools <= 0 {
		tools = int(service.DefaultTaskMaxWorkerToolCalls)
	}
	if int64(tools) > budget.RemainingWorkerToolCalls {
		tools = int(budget.RemainingWorkerToolCalls)
	}
	cfg.MaxToolCalls = tools
	tokens := cfg.MaxTotalTokens
	if tokens <= 0 {
		tokens = service.DefaultTaskMaxModelTotalTokens
	}
	if tokens > budget.RemainingModelTokens {
		tokens = budget.RemainingModelTokens
	}
	cfg.MaxTotalTokens = tokens
	duration := cfg.MaxDuration
	if duration <= 0 {
		duration = service.DefaultTaskMaxActiveExecution
	}
	if duration > budget.RemainingActiveExecution {
		duration = budget.RemainingActiveExecution
	}
	cfg.MaxDuration = duration
	if cfg.MaxEpisodePayloadBytes <= 0 || cfg.MaxEpisodePayloadBytes > int(service.DefaultWebEpisodeMaxPayloadBytes) {
		cfg.MaxEpisodePayloadBytes = int(service.DefaultWebEpisodeMaxPayloadBytes)
	}
	return cfg
}
