package worker

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"time"

	"mar/internal/agent"
	"mar/internal/domain"
)

const protocolVersion = 1

type ProviderConfig struct {
	BaseURL        string        `json:"base_url"`
	APIKeyEnv      string        `json:"api_key_env"`
	RequestTimeout time.Duration `json:"request_timeout"`
}

type StartRequest struct {
	Task             domain.Task             `json:"task"`
	Attempt          domain.ExecutionAttempt `json:"attempt"`
	WorkspacePath    string                  `json:"workspace_path"`
	Provider         ProviderConfig          `json:"provider"`
	AgentProfile     agent.Profile           `json:"agent_profile"`
	AgentConfig      agent.Config            `json:"agent_config"`
	SandboxReadPaths []string                `json:"sandbox_read_paths,omitempty"`
	GoModuleCache    string                  `json:"go_module_cache,omitempty"`
}

func (r StartRequest) Validate() error {
	if strings.TrimSpace(r.Task.ID) == "" || strings.TrimSpace(r.Attempt.ID) == "" || r.Attempt.RunEpoch <= 0 {
		return errors.New("worker start requires task and attempt identity")
	}
	if r.Attempt.TaskID != r.Task.ID || r.Attempt.RunEpoch != r.Task.RunEpoch {
		return errors.New("worker start task/attempt identity mismatch")
	}
	if r.Task.State != domain.TaskRunning {
		return errors.New("worker start requires RUNNING task state")
	}
	if strings.TrimSpace(r.WorkspacePath) == "" {
		return errors.New("worker start requires workspace path")
	}
	if strings.TrimSpace(r.Provider.BaseURL) == "" || strings.TrimSpace(r.Provider.APIKeyEnv) == "" {
		return errors.New("worker start requires model provider base URL and API key environment name")
	}
	if strings.TrimSpace(r.AgentProfile.Model) == "" || strings.TrimSpace(r.AgentProfile.BaseInstructions) == "" {
		return errors.New("worker start requires agent model profile")
	}
	for _, readPath := range r.SandboxReadPaths {
		readPath = strings.TrimSpace(readPath)
		if readPath == "" || !filepath.IsAbs(readPath) {
			return errors.New("worker sandbox read paths must be explicit absolute paths")
		}
	}
	if strings.TrimSpace(r.GoModuleCache) != "" {
		if !filepath.IsAbs(r.GoModuleCache) {
			return errors.New("worker shared Go module cache must be an absolute path")
		}
		granted := false
		for _, readPath := range r.SandboxReadPaths {
			if protocolPathWithin(r.GoModuleCache, readPath) {
				granted = true
				break
			}
		}
		if !granted {
			return errors.New("worker shared Go module cache must be inside an explicit sandbox read grant")
		}
	}
	return r.Task.Contract.Validate()
}

func protocolPathWithin(path, root string) bool {
	path = filepath.Clean(path)
	root = filepath.Clean(root)
	rel, err := filepath.Rel(root, path)
	if err != nil || filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false
	}
	return true
}

type frame struct {
	Version int             `json:"version"`
	Type    string          `json:"type"`
	ID      uint64          `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Payload json.RawMessage `json:"payload,omitempty"`
	Error   string          `json:"error,omitempty"`
}

const (
	frameStart    = "start"
	frameRequest  = "request"
	frameResponse = "response"
	frameResult   = "result"
	frameError    = "error"
)

const (
	methodAttemptAuthoritative = "attempt_authoritative"
	methodLatestCheckpoint     = "latest_checkpoint"
	methodPublishCheckpoint    = "publish_checkpoint"
)

func marshalFrame(kind string, id uint64, method string, payload any, errText string) (frame, error) {
	var raw json.RawMessage
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return frame{}, err
		}
		raw = encoded
	}
	return frame{Version: protocolVersion, Type: kind, ID: id, Method: method, Payload: raw, Error: errText}, nil
}

func validateFrame(f frame) error {
	if f.Version != protocolVersion {
		return errors.New("worker protocol version mismatch")
	}
	switch f.Type {
	case frameStart, frameRequest, frameResponse, frameResult, frameError:
		return nil
	default:
		return errors.New("unknown worker protocol frame type")
	}
}
