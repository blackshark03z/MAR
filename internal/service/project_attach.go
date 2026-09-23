package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"unicode"

	"mar/internal/domain"
	"mar/internal/pathidentity"
)

const researchOnlyProjectMode = "research_only"

type ProjectAttachResult struct {
	Schema           string               `json:"schema"`
	ProjectID        string               `json:"project_id"`
	Root             string               `json:"root"`
	RelativeTarget   string               `json:"relative_target,omitempty"`
	Kind             string               `json:"kind"`
	Mode             string               `json:"mode"`
	Created          bool                 `json:"created"`
	Reused           bool                 `json:"reused"`
	WorkspaceCreated bool                 `json:"workspace_created"`
	Policy           domain.ProjectPolicy `json:"policy"`
}

type ProjectDetachResult struct {
	Schema    string `json:"schema"`
	ProjectID string `json:"project_id"`
	Root      string `json:"root"`
	Detached  bool   `json:"detached"`
}

func (s *TaskService) DetachLocalProject(ctx context.Context, projectID string) (ProjectDetachResult, error) {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return ProjectDetachResult{}, errors.New("project_id is required for project detach")
	}
	if !strings.HasPrefix(projectID, "local-") {
		return ProjectDetachResult{}, errors.New("project detach only supports attach-generated local-* projects")
	}
	project, err := s.store.GetProject(ctx, projectID)
	if err != nil {
		return ProjectDetachResult{}, err
	}
	if err := s.store.DeleteProject(ctx, projectID); err != nil {
		return ProjectDetachResult{}, err
	}
	return ProjectDetachResult{Schema: "mar-project-detach-v1", ProjectID: projectID, Root: project.Root, Detached: true}, nil
}

func (s *TaskService) AttachLocalPath(ctx context.Context, requestedPath string) (ProjectAttachResult, error) {
	return s.AttachLocalPathWithNetwork(ctx, requestedPath, false)
}

func (s *TaskService) AttachLocalPathWithNetwork(ctx context.Context, requestedPath string, networkAllowed bool) (ProjectAttachResult, error) {
	requestedPath = strings.TrimSpace(requestedPath)
	if requestedPath == "" {
		return ProjectAttachResult{}, errors.New("local path is required")
	}
	resolved, err := pathidentity.ResolveExisting(requestedPath)
	if err != nil {
		return ProjectAttachResult{}, fmt.Errorf("resolve local path: %w", err)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return ProjectAttachResult{}, fmt.Errorf("inspect local path: %w", err)
	}
	kind, baseDir := "directory", resolved
	if !info.IsDir() {
		if !info.Mode().IsRegular() {
			return ProjectAttachResult{}, errors.New("local path attach supports regular files and directories only")
		}
		kind, baseDir = "file", filepath.Dir(resolved)
	}
	projects, err := s.store.ListProjects(ctx)
	if err != nil {
		return ProjectAttachResult{}, err
	}
	if project, target, ok := mostSpecificContainingProject(projects, resolved); ok {
		policy, err := s.store.GetProjectPolicy(ctx, project.ID)
		if err != nil {
			return ProjectAttachResult{}, fmt.Errorf("read existing project policy: %w", err)
		}
		rel, err := projectRelativeTarget(project.Root, target)
		if err != nil {
			return ProjectAttachResult{}, err
		}
		mode := researchOnlyProjectMode
		if _, ok := currentProjectHead(ctx, project.Root); ok {
			mode = "git"
		}
		if networkAllowed && mode == "git" && !policy.NetworkAllowed {
			policy.NetworkAllowed = true
			policy.UpdatedAt = s.now().UTC()
			if err := s.store.PutProjectPolicy(ctx, policy); err != nil {
				return ProjectAttachResult{}, fmt.Errorf("enable attached project network policy: %w", err)
			}
		}
		return ProjectAttachResult{Schema: "mar-project-attach-v1", ProjectID: project.ID, Root: project.Root, RelativeTarget: rel, Kind: kind, Mode: mode, Reused: true, Policy: policy}, nil
	}
	attachRoot, mode := baseDir, researchOnlyProjectMode
	if gitRoot, ok := discoverGitTopLevel(ctx, baseDir); ok {
		if _, err := safeProjectReadTarget(domain.Project{Root: gitRoot}, resolved); err == nil {
			attachRoot, mode = gitRoot, "git"
		}
	}
	attachRoot, err = pathidentity.ResolveExisting(attachRoot)
	if err != nil {
		return ProjectAttachResult{}, fmt.Errorf("resolve attach root: %w", err)
	}
	now := s.now().UTC()
	project := domain.Project{ID: stableAttachmentProjectID(attachRoot), Root: filepath.Clean(attachRoot), CreatedAt: now}
	var created bool
	if mode == researchOnlyProjectMode {
		project, created, err = s.store.RegisterProject(ctx, project)
		if err != nil {
			return ProjectAttachResult{}, err
		}
		if created {
			if err := s.store.PutProjectPolicy(ctx, domain.ProjectPolicy{ProjectID: project.ID, UpdatedAt: now}); err != nil {
				return ProjectAttachResult{}, fmt.Errorf("persist research-only project policy: %w", err)
			}
		}
	} else {
		project, created, err = s.RegisterProject(ctx, project.ID, project.Root)
		if err != nil {
			return ProjectAttachResult{}, err
		}
	}
	policy, err := s.store.GetProjectPolicy(ctx, project.ID)
	if err != nil {
		return ProjectAttachResult{}, fmt.Errorf("read attached project policy: %w", err)
	}
	if networkAllowed && mode == "git" && !policy.NetworkAllowed {
		policy.NetworkAllowed = true
		policy.UpdatedAt = s.now().UTC()
		if err := s.store.PutProjectPolicy(ctx, policy); err != nil {
			return ProjectAttachResult{}, fmt.Errorf("enable attached project network policy: %w", err)
		}
	}
	rel, err := projectRelativeTarget(project.Root, resolved)
	if err != nil {
		return ProjectAttachResult{}, err
	}
	return ProjectAttachResult{Schema: "mar-project-attach-v1", ProjectID: project.ID, Root: project.Root, RelativeTarget: rel, Kind: kind, Mode: mode, Created: created, Reused: !created, WorkspaceCreated: false, Policy: policy}, nil
}

func mostSpecificContainingProject(projects []domain.Project, target string) (domain.Project, string, bool) {
	var best domain.Project
	bestTarget := ""
	bestDepth := -1
	for _, project := range projects {
		resolved, err := safeProjectReadTarget(project, target)
		if err != nil {
			continue
		}
		if depth := len(filepath.Clean(project.Root)); depth > bestDepth {
			best, bestTarget, bestDepth = project, resolved, depth
		}
	}
	return best, bestTarget, bestDepth >= 0
}
func discoverGitTopLevel(ctx context.Context, dir string) (string, bool) {
	out, err := exec.CommandContext(ctx, "git", "-C", dir, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return "", false
	}
	root := strings.TrimSpace(string(out))
	if root == "" {
		return "", false
	}
	resolved, err := pathidentity.ResolveExisting(root)
	if err != nil {
		return "", false
	}
	return filepath.Clean(resolved), true
}
func currentProjectHead(ctx context.Context, root string) (string, bool) {
	out, err := exec.CommandContext(ctx, "git", "-C", root, "rev-parse", "HEAD").Output()
	if err != nil {
		return "", false
	}
	head := strings.TrimSpace(string(out))
	return head, head != ""
}
func stableAttachmentProjectID(root string) string {
	sum := sha256.Sum256([]byte(strings.ToLower(filepath.Clean(root))))
	return "local-" + attachmentSlug(filepath.Base(filepath.Clean(root))) + "-" + hex.EncodeToString(sum[:8])
}
func attachmentSlug(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	dash := false
	for _, r := range value {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			dash = false
			continue
		}
		if b.Len() > 0 && !dash {
			b.WriteByte('-')
			dash = true
		}
	}
	slug := strings.Trim(b.String(), "-")
	if slug == "" {
		return "path"
	}
	return slug
}
func projectRelativeTarget(root, target string) (string, error) {
	resolvedRoot, err := pathidentity.ResolveExisting(root)
	if err != nil {
		return "", fmt.Errorf("resolve project root: %w", err)
	}
	rel, err := filepath.Rel(resolvedRoot, target)
	if err != nil {
		return "", fmt.Errorf("resolve project-relative target: %w", err)
	}
	if rel == "." {
		return "", nil
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", errors.New("attached path escapes project root")
	}
	return filepath.ToSlash(rel), nil
}
