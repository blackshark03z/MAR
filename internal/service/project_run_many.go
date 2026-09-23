package service

import (
	"context"
	"errors"
)

const maxFastCommandBatch = 6

type ProjectCommandBatchResult struct {
	ProjectID     string                 `json:"project_id"`
	Results       []ProjectCommandResult `json:"results"`
	Passed        bool                   `json:"passed"`
	FailedCommand int                    `json:"failed_command,omitempty"`
}

func (s *TaskService) RunProjectCommands(ctx context.Context, projectID string, commands []ProjectVerifyCommand) (ProjectCommandBatchResult, error) {
	if len(commands) == 0 || len(commands) > maxFastCommandBatch {
		return ProjectCommandBatchResult{}, errors.New("run_many requires 1..6 commands")
	}
	result := ProjectCommandBatchResult{
		ProjectID: projectID,
		Results:   make([]ProjectCommandResult, 0, len(commands)),
		Passed:    true,
	}
	for index, command := range commands {
		commandResult, err := s.RunProjectCommand(ctx, projectID, command.Executable, command.Args, command.Cwd, command.TimeoutSeconds, command.MaxOutputBytes)
		if err != nil {
			return ProjectCommandBatchResult{}, err
		}
		result.Results = append(result.Results, commandResult)
		if commandResult.ExitCode != 0 || commandResult.TimedOut {
			result.Passed = false
			result.FailedCommand = index + 1
			break
		}
	}
	return result, nil
}
