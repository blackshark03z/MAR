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

// GoalContract is immutable task intent. Material changes create a superseding task.
type GoalContract struct {
	Goal                string    `json:"goal"`
	Acceptance          []string  `json:"acceptance"`
	Boundaries          []string  `json:"boundaries"`
	NonGoals            []string  `json:"non_goals"`
	ProjectID           string    `json:"project_id"`
	BaseRevision        string    `json:"base_revision"`
	Authority           Authority `json:"authority"`
	VerificationProfile string    `json:"verification_profile"`
	Priority            string    `json:"priority"`
}

func (g GoalContract) Validate() error {
	if strings.TrimSpace(g.Goal) == "" {
		return errors.New("goal is required")
	}
	if len(g.Acceptance) == 0 {
		return errors.New("at least one acceptance criterion is required")
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
