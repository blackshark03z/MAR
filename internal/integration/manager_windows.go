//go:build windows

package integration

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"mar/internal/domain"
	"mar/internal/processctl"
	"mar/internal/store"
)

var (
	ErrNotReady             = errors.New("task is not ready for authoritative integration")
	ErrHeadDrift            = errors.New("authoritative project head drifted from verified base")
	ErrIntegrationBlocked   = errors.New("authoritative integration is blocked")
	ErrWorktreeSyncRequired = errors.New("authoritative ref advanced but project worktree synchronization is incomplete")
)

type FreshResultGate interface {
	LatestFreshResult(context.Context, string) (domain.TaskResult, bool, error)
}

type gitRunner interface {
	Run(context.Context, string, string, ...string) (string, error)
}

type containedGit struct {
	path string
}

func (g containedGit) Run(ctx context.Context, taskID, root string, args ...string) (string, error) {
	return processctl.RunContainedCommand(ctx, processctl.CommandSpec{
		TaskID:         taskID,
		OperationID:    "integration-git:" + integrationOperation(args),
		Path:           g.path,
		Args:           append([]string{"-C", root}, args...),
		Dir:            root,
		MaxOutputBytes: 256 << 10,
	})
}

type Manager struct {
	store *store.SQLite
	gate  FreshResultGate
	git   gitRunner
	now   func() time.Time

	locksMu sync.Mutex
	locks   map[string]*sync.Mutex
}

func NewManager(s *store.SQLite, gate FreshResultGate) (*Manager, error) {
	if s == nil || gate == nil {
		return nil, errors.New("integration manager requires store and fresh-result gate")
	}
	gitPath, err := exec.LookPath("git")
	if err != nil {
		return nil, fmt.Errorf("git executable is required for integration: %w", err)
	}
	return &Manager{store: s, gate: gate, git: containedGit{path: gitPath}, now: time.Now, locks: make(map[string]*sync.Mutex)}, nil
}

func newManagerWithGit(s *store.SQLite, gate FreshResultGate, git gitRunner) (*Manager, error) {
	if s == nil || gate == nil || git == nil {
		return nil, errors.New("integration manager requires store, fresh-result gate and git runner")
	}
	return &Manager{store: s, gate: gate, git: git, now: time.Now, locks: make(map[string]*sync.Mutex)}, nil
}

func (m *Manager) Integrate(ctx context.Context, taskID string) (domain.IntegrationAttempt, domain.TaskResult, error) {
	task, err := m.store.GetTask(ctx, taskID)
	if err != nil {
		return domain.IntegrationAttempt{}, domain.TaskResult{}, err
	}
	lock := m.projectLock(task.Contract.ProjectID)
	lock.Lock()
	defer lock.Unlock()

	if existing, ok, err := m.store.LatestIntegrationAttempt(ctx, taskID); err != nil {
		return domain.IntegrationAttempt{}, domain.TaskResult{}, err
	} else if ok {
		switch existing.Status {
		case domain.IntegrationPrepared, domain.IntegrationDispatched:
			return m.driveAttempt(ctx, existing)
		case domain.IntegrationComplete:
			result, available, err := m.store.LatestTaskResult(ctx, taskID)
			if err != nil || !available {
				if err == nil {
					err = store.ErrNotFound
				}
				return domain.IntegrationAttempt{}, domain.TaskResult{}, err
			}
			return existing, result, nil
		case domain.IntegrationBlocked:
			result, _, _ := m.store.LatestTaskResult(ctx, taskID)
			return existing, result, ErrIntegrationBlocked
		}
	}

	result, fresh, err := m.gate.LatestFreshResult(ctx, taskID)
	if err != nil {
		return domain.IntegrationAttempt{}, domain.TaskResult{}, err
	}
	if !fresh || result.Verdict != domain.ResultVerified {
		return domain.IntegrationAttempt{}, domain.TaskResult{}, ErrNotReady
	}
	workspace, err := m.store.GetWorkspaceByTask(ctx, taskID)
	if err != nil {
		return domain.IntegrationAttempt{}, domain.TaskResult{}, err
	}
	project, err := m.store.GetProject(ctx, task.Contract.ProjectID)
	if err != nil {
		return domain.IntegrationAttempt{}, domain.TaskResult{}, err
	}
	expectedRef, err := m.symbolicHead(ctx, taskID, project.Root)
	if err != nil {
		return domain.IntegrationAttempt{}, domain.TaskResult{}, err
	}
	actualHead, err := m.refHead(ctx, taskID, project.Root, expectedRef)
	if err != nil {
		return domain.IntegrationAttempt{}, domain.TaskResult{}, err
	}
	if actualHead != workspace.BaseRevision {
		reason := fmt.Sprintf("authoritative head drift: expected verified base %s, observed %s", workspace.BaseRevision, actualHead)
		blocked, blockErr := m.store.BlockVerifiedIntegration(ctx, taskID, reason, newID("result"), m.now().UTC())
		if blockErr != nil {
			return domain.IntegrationAttempt{}, domain.TaskResult{}, blockErr
		}
		return domain.IntegrationAttempt{}, blocked, fmt.Errorf("%w: %s", ErrHeadDrift, reason)
	}
	clean, err := m.projectClean(ctx, taskID, project.Root)
	if err != nil {
		return domain.IntegrationAttempt{}, domain.TaskResult{}, err
	}
	if !clean {
		reason := "authoritative project worktree is not clean before integration"
		blocked, blockErr := m.store.BlockVerifiedIntegration(ctx, taskID, reason, newID("result"), m.now().UTC())
		if blockErr != nil {
			return domain.IntegrationAttempt{}, domain.TaskResult{}, blockErr
		}
		return domain.IntegrationAttempt{}, blocked, fmt.Errorf("%w: %s", ErrIntegrationBlocked, reason)
	}
	if _, err := m.git.Run(ctx, taskID, project.Root, "merge-base", "--is-ancestor", workspace.BaseRevision, result.FinalRevision); err != nil {
		reason := "verified candidate is not a descendant of the resolved task base"
		blocked, blockErr := m.store.BlockVerifiedIntegration(ctx, taskID, reason, newID("result"), m.now().UTC())
		if blockErr != nil {
			return domain.IntegrationAttempt{}, domain.TaskResult{}, blockErr
		}
		return domain.IntegrationAttempt{}, blocked, fmt.Errorf("%w: %s", ErrIntegrationBlocked, reason)
	}
	seed := domain.IntegrationAttempt{
		ID:                 newID("integration"),
		TaskID:             taskID,
		ProjectID:          task.Contract.ProjectID,
		ExpectedRef:        expectedRef,
		ExpectedHead:       workspace.BaseRevision,
		TaskResultID:       result.ID,
		TaskResultVersion:  result.Version,
		TaskResultRevision: result.FinalRevision,
		CandidateRevision:  result.FinalRevision,
		EvidenceID:         result.EvidenceID,
	}
	attempt, err := m.store.PrepareIntegrationAttempt(ctx, seed, m.now().UTC())
	if err != nil {
		return domain.IntegrationAttempt{}, domain.TaskResult{}, err
	}
	return m.driveAttempt(ctx, attempt)
}

func (m *Manager) RecoverPending(ctx context.Context) error {
	pending, err := m.store.ListPendingIntegrationAttempts(ctx)
	if err != nil {
		return err
	}
	for _, attempt := range pending {
		lock := m.projectLock(attempt.ProjectID)
		lock.Lock()
		_, _, driveErr := m.driveAttempt(ctx, attempt)
		lock.Unlock()
		if driveErr != nil && !errors.Is(driveErr, ErrIntegrationBlocked) && !errors.Is(driveErr, ErrWorktreeSyncRequired) {
			return driveErr
		}
	}
	return nil
}

func (m *Manager) RecoverAttempt(ctx context.Context, attemptID string) (domain.IntegrationAttempt, domain.TaskResult, error) {
	attempt, err := m.store.GetIntegrationAttempt(ctx, attemptID)
	if err != nil {
		return domain.IntegrationAttempt{}, domain.TaskResult{}, err
	}
	lock := m.projectLock(attempt.ProjectID)
	lock.Lock()
	defer lock.Unlock()
	return m.driveAttempt(ctx, attempt)
}

func (m *Manager) driveAttempt(ctx context.Context, attempt domain.IntegrationAttempt) (domain.IntegrationAttempt, domain.TaskResult, error) {
	project, err := m.store.GetProject(ctx, attempt.ProjectID)
	if err != nil {
		return domain.IntegrationAttempt{}, domain.TaskResult{}, err
	}
	attempt, err = m.store.GetIntegrationAttempt(ctx, attempt.ID)
	if err != nil {
		return domain.IntegrationAttempt{}, domain.TaskResult{}, err
	}
	if attempt.Status == domain.IntegrationComplete {
		result, ok, err := m.store.LatestTaskResult(ctx, attempt.TaskID)
		if err != nil || !ok {
			if err == nil {
				err = store.ErrNotFound
			}
			return domain.IntegrationAttempt{}, domain.TaskResult{}, err
		}
		return attempt, result, nil
	}
	if attempt.Status == domain.IntegrationBlocked {
		result, _, _ := m.store.LatestTaskResult(ctx, attempt.TaskID)
		return attempt, result, ErrIntegrationBlocked
	}

	if attempt.Status == domain.IntegrationPrepared {
		freshResult, fresh, gateErr := m.gate.LatestFreshResult(ctx, attempt.TaskID)
		if gateErr != nil || !fresh || freshResult.ID != attempt.TaskResultID || freshResult.EvidenceID != attempt.EvidenceID {
			reason := "verification evidence became stale before integration dispatch"
			if gateErr != nil {
				reason += ": " + gateErr.Error()
			}
			return m.blockAttempt(ctx, attempt, project.Root, reason)
		}
		currentRef, refErr := m.symbolicHead(ctx, attempt.TaskID, project.Root)
		if refErr != nil || currentRef != attempt.ExpectedRef {
			reason := "authoritative checked-out ref changed before integration dispatch"
			if refErr != nil {
				reason += ": " + refErr.Error()
			}
			return m.blockAttempt(ctx, attempt, project.Root, reason)
		}
		head, headErr := m.refHead(ctx, attempt.TaskID, project.Root, attempt.ExpectedRef)
		if headErr != nil || head != attempt.ExpectedHead {
			reason := fmt.Sprintf("authoritative head changed before integration dispatch: expected=%s observed=%s", attempt.ExpectedHead, head)
			if headErr != nil {
				reason += ": " + headErr.Error()
			}
			return m.blockAttempt(ctx, attempt, project.Root, reason)
		}
		clean, cleanErr := m.projectClean(ctx, attempt.TaskID, project.Root)
		if cleanErr != nil || !clean {
			reason := "authoritative project worktree became dirty before integration dispatch"
			if cleanErr != nil {
				reason += ": " + cleanErr.Error()
			}
			return m.blockAttempt(ctx, attempt, project.Root, reason)
		}
		attempt, err = m.store.MarkIntegrationDispatched(ctx, attempt.ID, m.now().UTC())
		if err != nil {
			return domain.IntegrationAttempt{}, domain.TaskResult{}, err
		}
	}

	if attempt.Status != domain.IntegrationDispatched {
		return domain.IntegrationAttempt{}, domain.TaskResult{}, store.ErrStateConflict
	}
	head, err := m.refHead(ctx, attempt.TaskID, project.Root, attempt.ExpectedRef)
	if err != nil {
		return domain.IntegrationAttempt{}, domain.TaskResult{}, err
	}
	if head == attempt.ExpectedHead {
		freshResult, fresh, gateErr := m.gate.LatestFreshResult(ctx, attempt.TaskID)
		if gateErr != nil || !fresh || freshResult.ID != attempt.TaskResultID || freshResult.EvidenceID != attempt.EvidenceID {
			reason := "verification evidence became stale while dispatched but before authoritative ref advancement"
			if gateErr != nil {
				reason += ": " + gateErr.Error()
			}
			return m.blockAttempt(ctx, attempt, project.Root, reason)
		}
		_, updateErr := m.git.Run(ctx, attempt.TaskID, project.Root, "update-ref", attempt.ExpectedRef, attempt.CandidateRevision, attempt.ExpectedHead)
		if updateErr != nil {
			observed, observeErr := m.refHead(ctx, attempt.TaskID, project.Root, attempt.ExpectedRef)
			if observeErr != nil {
				return domain.IntegrationAttempt{}, domain.TaskResult{}, fmt.Errorf("integration CAS failed and ref observation failed: %w / %v", updateErr, observeErr)
			}
			switch observed {
			case attempt.CandidateRevision:
				head = observed
			case attempt.ExpectedHead:
				return attempt, domain.TaskResult{}, fmt.Errorf("integration CAS not applied: %w", updateErr)
			default:
				return m.blockAttempt(ctx, attempt, project.Root, fmt.Sprintf("integration CAS reconciled unexpected authoritative head %s", observed))
			}
		} else {
			head = attempt.CandidateRevision
		}
	}
	if head != attempt.CandidateRevision {
		return m.blockAttempt(ctx, attempt, project.Root, fmt.Sprintf("dispatched integration observed unexpected authoritative head %s", head))
	}

	// The ref is authoritative. Synchronize the registered root only if it is
	// still checked out on that same ref. reset --merge refuses to overwrite
	// conflicting owner changes, so recovery can retry without data loss.
	if checkedRef, refErr := m.symbolicHead(ctx, attempt.TaskID, project.Root); refErr == nil && checkedRef == attempt.ExpectedRef {
		if _, resetErr := m.git.Run(ctx, attempt.TaskID, project.Root, "reset", "--merge", attempt.CandidateRevision); resetErr != nil {
			return attempt, domain.TaskResult{}, fmt.Errorf("%w: %v", ErrWorktreeSyncRequired, resetErr)
		}
		clean, cleanErr := m.projectClean(ctx, attempt.TaskID, project.Root)
		if cleanErr != nil {
			return attempt, domain.TaskResult{}, cleanErr
		}
		if !clean {
			return attempt, domain.TaskResult{}, ErrWorktreeSyncRequired
		}
	}
	result, err := m.store.FinalizeIntegrationApplied(ctx, attempt.ID, attempt.CandidateRevision, newID("result"), m.now().UTC())
	if err != nil {
		return domain.IntegrationAttempt{}, domain.TaskResult{}, err
	}
	completed, err := m.store.GetIntegrationAttempt(ctx, attempt.ID)
	if err != nil {
		return domain.IntegrationAttempt{}, domain.TaskResult{}, err
	}
	return completed, result, nil
}

func (m *Manager) blockAttempt(ctx context.Context, attempt domain.IntegrationAttempt, root, reason string) (domain.IntegrationAttempt, domain.TaskResult, error) {
	observed, _ := m.refHead(ctx, attempt.TaskID, root, attempt.ExpectedRef)
	result, err := m.store.BlockIntegrationAttempt(ctx, attempt.ID, observed, reason, newID("result"), m.now().UTC())
	if err != nil {
		return domain.IntegrationAttempt{}, domain.TaskResult{}, err
	}
	blocked, err := m.store.GetIntegrationAttempt(ctx, attempt.ID)
	if err != nil {
		return domain.IntegrationAttempt{}, domain.TaskResult{}, err
	}
	return blocked, result, fmt.Errorf("%w: %s", ErrIntegrationBlocked, reason)
}

func (m *Manager) symbolicHead(ctx context.Context, taskID, root string) (string, error) {
	out, err := m.git.Run(ctx, taskID, root, "symbolic-ref", "-q", "HEAD")
	if err != nil {
		return "", fmt.Errorf("authoritative project HEAD must be a branch ref: %w", err)
	}
	ref := strings.TrimSpace(out)
	if !strings.HasPrefix(ref, "refs/heads/") {
		return "", fmt.Errorf("authoritative project HEAD ref is unsupported: %q", ref)
	}
	return ref, nil
}

func (m *Manager) refHead(ctx context.Context, taskID, root, ref string) (string, error) {
	out, err := m.git.Run(ctx, taskID, root, "rev-parse", "--verify", "--end-of-options", ref+"^{commit}")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

func (m *Manager) projectClean(ctx context.Context, taskID, root string) (bool, error) {
	out, err := m.git.Run(ctx, taskID, root, "status", "--porcelain=v1", "-z", "--untracked-files=normal")
	if err != nil {
		return false, err
	}
	return len(out) == 0, nil
}

func (m *Manager) projectLock(projectID string) *sync.Mutex {
	m.locksMu.Lock()
	defer m.locksMu.Unlock()
	lock := m.locks[projectID]
	if lock == nil {
		lock = &sync.Mutex{}
		m.locks[projectID] = lock
	}
	return lock
}

func newID(prefix string) string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		panic(fmt.Sprintf("crypto/rand failed: %v", err))
	}
	buf[6] = (buf[6] & 0x0f) | 0x40
	buf[8] = (buf[8] & 0x3f) | 0x80
	return prefix + "-" + hex.EncodeToString(buf)
}

func integrationOperation(args []string) string {
	if len(args) == 0 {
		return "unknown"
	}
	name := strings.TrimSpace(args[0])
	name = strings.ReplaceAll(name, string(filepath.Separator), "-")
	if name == "" {
		return "unknown"
	}
	return name
}
