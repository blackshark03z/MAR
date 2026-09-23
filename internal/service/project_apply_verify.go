package service

import (
	"context"
	"errors"
	"strings"
)

const (
	maxFastApplyChanges = 64
	maxFastVerifyTasks  = 6
)

type ProjectOwnedChange struct {
	Path           string
	ExpectedSHA256 string
	Search         string
	Replacement    string
	ExpectedCount  int
	Content        *string
}

type ProjectVerifyCommand struct {
	Executable     string
	Args           []string
	Cwd            string
	TimeoutSeconds int
	MaxOutputBytes int
}

type ProjectChangeResult struct {
	Operation string              `json:"operation"`
	Write     *ProjectWriteResult `json:"write,omitempty"`
	Patch     *ProjectPatchResult `json:"patch,omitempty"`
}

type ProjectApplyVerifyResult struct {
	ProjectID          string                 `json:"project_id"`
	Changes            []ProjectChangeResult  `json:"changes"`
	Verification       []ProjectCommandResult `json:"verification"`
	Passed             bool                   `json:"passed"`
	FailedVerification int                    `json:"failed_verification,omitempty"`
}

func (s *TaskService) ApplyAndVerifyProject(ctx context.Context, projectID string, changes []ProjectOwnedChange, verification []ProjectVerifyCommand) (ProjectApplyVerifyResult, error) {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return ProjectApplyVerifyResult{}, errors.New("project_id is required")
	}
	if len(changes) == 0 || len(changes) > maxFastApplyChanges {
		return ProjectApplyVerifyResult{}, errors.New("apply_and_verify requires 1..64 changes")
	}
	if len(verification) > maxFastVerifyTasks {
		return ProjectApplyVerifyResult{}, errors.New("apply_and_verify supports at most 6 verification commands")
	}
	result := ProjectApplyVerifyResult{
		ProjectID:    projectID,
		Changes:      make([]ProjectChangeResult, 0, len(changes)),
		Verification: make([]ProjectCommandResult, 0, len(verification)),
		Passed:       true,
	}
	for _, change := range changes {
		if strings.TrimSpace(change.Path) == "" {
			return ProjectApplyVerifyResult{}, errors.New("change path is required")
		}
		if change.Content != nil {
			writeResult, err := s.WriteProjectFile(ctx, ProjectWriteRequest{
				ProjectID:      projectID,
				Path:           change.Path,
				ExpectedSHA256: change.ExpectedSHA256,
				Content:        *change.Content,
			})
			if err != nil {
				return ProjectApplyVerifyResult{}, err
			}
			result.Changes = append(result.Changes, ProjectChangeResult{Operation: "write", Write: &writeResult})
			continue
		}
		if change.Search == "" {
			return ProjectApplyVerifyResult{}, errors.New("patch change requires search text when content is omitted")
		}
		patchResult, err := s.ApplyProjectPatch(ctx, ProjectPatchRequest{
			ProjectID:      projectID,
			Path:           change.Path,
			ExpectedSHA256: change.ExpectedSHA256,
			Search:         change.Search,
			Replacement:    change.Replacement,
			ExpectedCount:  change.ExpectedCount,
		})
		if err != nil {
			return ProjectApplyVerifyResult{}, err
		}
		result.Changes = append(result.Changes, ProjectChangeResult{Operation: "patch", Patch: &patchResult})
	}
	for index, task := range verification {
		commandResult, err := s.RunProjectCommand(ctx, projectID, task.Executable, task.Args, task.Cwd, task.TimeoutSeconds, task.MaxOutputBytes)
		if err != nil {
			return ProjectApplyVerifyResult{}, err
		}
		result.Verification = append(result.Verification, commandResult)
		if commandResult.ExitCode != 0 || commandResult.TimedOut {
			result.Passed = false
			result.FailedVerification = index + 1
			break
		}
	}
	return result, nil
}
