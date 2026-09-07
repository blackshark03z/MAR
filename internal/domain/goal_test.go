package domain

import (
	"strings"
	"testing"
)

func validGoalForOracleTest() GoalContract {
	return GoalContract{
		Goal:                "make a bounded change",
		Acceptance:          []string{"criterion one"},
		Boundaries:          []string{"local project only"},
		NonGoals:            []string{},
		ProjectID:           "project-1",
		BaseRevision:        "0123456789abcdef",
		Authority:           Authority{LocalFileWrite: true, LocalGitWrite: true},
		VerificationProfile: "go-standard",
		Priority:            "P2",
	}
}

func TestGoalContractRejectsUnsupportedProseAcceptanceOracle(t *testing.T) {
	goal := validGoalForOracleTest()
	goal.AcceptanceChecks = []AcceptanceCheck{{
		CriterionIndex: 1,
		Scenario:       "run a test",
		Oracle:         "the test proves the feature works",
		CommandIndexes: []int{1},
	}}
	if err := goal.Validate(); err == nil || !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("unsupported prose oracle was admitted: %v", err)
	}
}

func TestGoalContractOutputContainsRequiresCommandIndex(t *testing.T) {
	goal := validGoalForOracleTest()
	goal.AcceptanceChecks = []AcceptanceCheck{{
		CriterionIndex: 1,
		Scenario:       "run the criterion-specific test",
		Oracle:         "output_contains:MAR_ACCEPTANCE_1",
	}}
	if err := goal.Validate(); err == nil || !strings.Contains(err.Error(), "command index") {
		t.Fatalf("output_contains without command index was admitted: %v", err)
	}
}

func TestGoalContractFileContainsMustUseCandidateFileObservationOnly(t *testing.T) {
	goal := validGoalForOracleTest()
	goal.AcceptanceChecks = []AcceptanceCheck{{
		CriterionIndex: 1,
		Scenario:       "inspect the sealed candidate README",
		Oracle:         "file_contains:README.md:owner journey ready",
		CommandIndexes: []int{1},
	}}
	if err := goal.Validate(); err == nil || !strings.Contains(err.Error(), "must not declare command indexes") {
		t.Fatalf("file_contains with command indexes was admitted: %v", err)
	}

	goal.AcceptanceChecks[0].CommandIndexes = nil
	if err := goal.Validate(); err != nil {
		t.Fatalf("valid file_contains oracle was rejected: %v", err)
	}
}
