package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"mar/internal/domain"
)

const researchArtifactVerificationProfile = "research-artifacts"

type ProjectCapability struct {
	State                          string   `json:"state"`
	Ecosystems                     []string `json:"ecosystems,omitempty"`
	Languages                      []string `json:"languages,omitempty"`
	EvidenceMarkers                []string `json:"evidence_markers,omitempty"`
	SupportedVerificationProfiles  []string `json:"supported_verification_profiles,omitempty"`
	RecommendedVerificationProfile string   `json:"recommended_verification_profile,omitempty"`
}

type ProjectContextItem struct {
	ProjectID  string               `json:"project_id"`
	Head       string               `json:"head"`
	Policy     domain.ProjectPolicy `json:"policy"`
	Capability ProjectCapability    `json:"capability"`
}

var pythonProjectMarkers = []string{
	"pyproject.toml",
	"setup.py",
	"setup.cfg",
	"requirements.txt",
	"requirements-dev.txt",
}

func detectProjectCapability(root string) (ProjectCapability, error) {
	hasGo, err := rootMarkerExists(root, "go.mod")
	if err != nil {
		return ProjectCapability{}, err
	}
	pythonMarkers := make([]string, 0, len(pythonProjectMarkers))
	for _, marker := range pythonProjectMarkers {
		present, markerErr := rootMarkerExists(root, marker)
		if markerErr != nil {
			return ProjectCapability{}, markerErr
		}
		if present {
			pythonMarkers = append(pythonMarkers, marker)
		}
	}
	hasPython := len(pythonMarkers) != 0

	capability := ProjectCapability{
		State:                          "supported",
		Ecosystems:                     []string{"artifact"},
		SupportedVerificationProfiles:  []string{researchArtifactVerificationProfile},
		RecommendedVerificationProfile: researchArtifactVerificationProfile,
	}
	switch {
	case hasGo && hasPython:
		capability.State = "mixed"
		capability.Ecosystems = []string{"go", "python"}
		capability.Languages = []string{"go", "python"}
		capability.EvidenceMarkers = append([]string{"go.mod"}, pythonMarkers...)
		capability.SupportedVerificationProfiles = []string{"go-standard", "go-docs", "go-release", "python-standard"}
		capability.RecommendedVerificationProfile = ""
	case hasGo:
		capability.Ecosystems = []string{"go"}
		capability.Languages = []string{"go"}
		capability.EvidenceMarkers = []string{"go.mod"}
		capability.SupportedVerificationProfiles = []string{"go-standard", "go-docs", "go-release"}
		capability.RecommendedVerificationProfile = "go-standard"
	case hasPython:
		capability.Ecosystems = []string{"python"}
		capability.Languages = []string{"python"}
		capability.EvidenceMarkers = pythonMarkers
		capability.SupportedVerificationProfiles = []string{"python-standard"}
		capability.RecommendedVerificationProfile = "python-standard"
	}
	return capability, nil
}

func rootMarkerExists(root, marker string) (bool, error) {
	info, err := os.Stat(filepath.Join(root, marker))
	if err == nil {
		return !info.IsDir(), nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return false, fmt.Errorf("inspect project capability marker %q: %w", marker, err)
}

func (s *TaskService) ProjectContext(ctx context.Context, projectID string) ([]ProjectContextItem, error) {
	projects, err := s.store.ListProjects(ctx)
	if err != nil {
		return nil, err
	}
	if len(projects) == 0 {
		return nil, errors.New("no MAR projects are registered")
	}
	projectID = strings.TrimSpace(projectID)
	items := make([]ProjectContextItem, 0, len(projects))
	for _, project := range projects {
		if projectID != "" && project.ID != projectID {
			continue
		}
		cmd := exec.CommandContext(ctx, "git", "-C", project.Root, "rev-parse", "HEAD")
		out, err := cmd.Output()
		if err != nil {
			return nil, fmt.Errorf("read current HEAD for project %q: %w", project.ID, err)
		}
		policy, err := s.store.GetProjectPolicy(ctx, project.ID)
		if err != nil {
			return nil, fmt.Errorf("read project policy for %q: %w", project.ID, err)
		}
		capability, err := detectProjectCapability(project.Root)
		if err != nil {
			return nil, fmt.Errorf("detect project capability for %q: %w", project.ID, err)
		}
		items = append(items, ProjectContextItem{
			ProjectID:  project.ID,
			Head:       strings.TrimSpace(string(out)),
			Policy:     policy,
			Capability: capability,
		})
	}
	if projectID != "" && len(items) == 0 {
		return nil, fmt.Errorf("unknown project %q", projectID)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ProjectID < items[j].ProjectID })
	return items, nil
}
