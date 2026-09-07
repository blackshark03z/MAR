package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
)

// Authority describes the bounded authority requested by one Goal Contract.
// The worker runtime may further restrict these permissions; it must never widen them.
type Authority struct {
	LocalFileWrite bool `json:"local_file_write"`
	LocalGitWrite  bool `json:"local_git_write"`
	NetworkAllowed bool `json:"network_allowed"`
	RemoteGitWrite bool `json:"remote_git_write"`
	DeployAllowed  bool `json:"deploy_allowed"`
}

// AcceptanceCheck binds one immutable acceptance criterion to a machine-verifiable
// scenario/oracle. V1 supports only output_contains:<literal> (with one-based
// CommandIndexes into the selected verification profile) and
// file_contains:<relative-path>:<literal> (observed directly on the sealed
// candidate). Missing checks remain UNVERIFIED for backward-compatible durable
// contracts.
type AcceptanceCheck struct {
	CriterionIndex int    `json:"criterion_index"`
	Scenario       string `json:"scenario"`
	Oracle         string `json:"oracle"`
	CommandIndexes []int  `json:"command_indexes"`
}

// GoalContract is immutable task intent. Material changes create a superseding task.
type GoalContract struct {
	Goal                string            `json:"goal"`
	Acceptance          []string          `json:"acceptance"`
	AcceptanceChecks    []AcceptanceCheck `json:"acceptance_checks,omitempty"`
	Boundaries          []string          `json:"boundaries"`
	NonGoals            []string          `json:"non_goals"`
	ProjectID           string            `json:"project_id"`
	BaseRevision        string            `json:"base_revision"`
	Authority           Authority         `json:"authority"`
	VerificationProfile string            `json:"verification_profile"`
	Priority            string            `json:"priority"`
}

func (g GoalContract) Validate() error {
	if strings.TrimSpace(g.Goal) == "" {
		return errors.New("goal is required")
	}
	if len(g.Acceptance) == 0 {
		return errors.New("at least one acceptance criterion is required")
	}
	for _, criterion := range g.Acceptance {
		if strings.TrimSpace(criterion) == "" {
			return errors.New("acceptance criteria cannot be blank")
		}
	}
	if len(g.AcceptanceChecks) > 0 {
		if len(g.AcceptanceChecks) != len(g.Acceptance) {
			return errors.New("acceptance_checks must cover every acceptance criterion exactly once")
		}
		seen := make(map[int]struct{}, len(g.AcceptanceChecks))
		for _, check := range g.AcceptanceChecks {
			if check.CriterionIndex < 1 || check.CriterionIndex > len(g.Acceptance) {
				return errors.New("acceptance check criterion_index is out of range")
			}
			if _, duplicate := seen[check.CriterionIndex]; duplicate {
				return errors.New("acceptance check criterion_index must be unique")
			}
			seen[check.CriterionIndex] = struct{}{}
			if strings.TrimSpace(check.Scenario) == "" || strings.TrimSpace(check.Oracle) == "" {
				return errors.New("acceptance check scenario and oracle are required")
			}
			oracle := strings.TrimSpace(check.Oracle)
			switch {
			case strings.HasPrefix(oracle, "output_contains:"):
				if strings.TrimSpace(strings.TrimPrefix(oracle, "output_contains:")) == "" {
					return errors.New("output_contains acceptance oracle requires a non-blank literal")
				}
				if len(check.CommandIndexes) == 0 {
					return errors.New("output_contains acceptance oracle requires at least one command index")
				}
			case strings.HasPrefix(oracle, "file_contains:"):
				rest := strings.TrimPrefix(oracle, "file_contains:")
				parts := strings.SplitN(rest, ":", 2)
				if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
					return errors.New("file_contains acceptance oracle requires relative-path and non-blank literal")
				}
				if len(check.CommandIndexes) != 0 {
					return errors.New("file_contains acceptance oracle must not declare command indexes")
				}
			default:
				return errors.New("acceptance oracle is unsupported by MAR V1")
			}
			seenCommand := map[int]struct{}{}
			for _, index := range check.CommandIndexes {
				if index <= 0 {
					return errors.New("acceptance check command indexes must be positive")
				}
				if _, duplicate := seenCommand[index]; duplicate {
					return errors.New("acceptance check command indexes must be unique")
				}
				seenCommand[index] = struct{}{}
			}
		}
	}
	if strings.TrimSpace(g.ProjectID) == "" {
		return errors.New("project_id is required")
	}
	if strings.TrimSpace(g.BaseRevision) == "" {
		return errors.New("base_revision is required")
	}
	if strings.TrimSpace(g.VerificationProfile) == "" {
		return errors.New("verification_profile is required")
	}
	if strings.TrimSpace(g.Priority) == "" {
		return errors.New("priority is required")
	}
	return nil
}

// CanonicalJSON uses a fixed struct shape so the contract hash is stable for the
// same serialized contract. Slice order is intentionally significant.
func (g GoalContract) CanonicalJSON() ([]byte, error) {
	return json.Marshal(g)
}

func (g GoalContract) Hash() (string, error) {
	payload, err := g.CanonicalJSON()
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:]), nil
}
