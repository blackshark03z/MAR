package worker

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"time"

	"mar/internal/agent"
	"mar/internal/domain"
)

const protocolVersion = 2

// WaitCapacity allows the parent runtime to park scarce execution capacity
// while a worker is durably waiting for external Web cognition. The worker
// process/attempt remains authoritative; mutation may resume only after Resume
// reacquires the parent-owned capacity grant.
type WaitCapacity interface {
	Park(context.Context) (bool, error)
	Resume(context.Context) error
}

type BrainMode string

const (
	BrainProvider BrainMode = "provider"
	BrainWeb      BrainMode = "web"
)

type ProviderConfig struct {
	BrainMode      BrainMode     `json:"brain_mode,omitempty"`
	BaseURL        string        `json:"base_url"`
	APIKeyEnv      string        `json:"api_key_env"`
	RequestTimeout time.Duration `json:"request_timeout"`
}

func (c ProviderConfig) Mode() BrainMode {
	if c.BrainMode == "" {
		return BrainProvider
	}
	return c.BrainMode
}

type StartRequest struct {
	Task                  domain.Task             `json:"task"`
	Attempt               domain.ExecutionAttempt `json:"attempt"`
	WorkspacePath         string                  `json:"workspace_path"`
	Provider              ProviderConfig          `json:"provider"`
	AgentProfile          agent.Profile           `json:"agent_profile"`
	AgentConfig           agent.Config            `json:"agent_config"`
	SandboxReadPaths      []string                `json:"sandbox_read_paths,omitempty"`
	GoModuleCache         string                  `json:"go_module_cache,omitempty"`
	GoBuildCache          string                  `json:"go_build_cache,omitempty"`
	CommandTimeout        time.Duration           `json:"command_timeout,omitempty"`
	MemoryPressurePercent float64                 `json:"memory_pressure_percent,omitempty"`
	Capacity              WaitCapacity            `json:"-"`
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
	switch r.Provider.Mode() {
	case BrainProvider:
		if strings.TrimSpace(r.Provider.BaseURL) == "" || strings.TrimSpace(r.Provider.APIKeyEnv) == "" {
			return errors.New("provider brain mode requires model provider base URL and API key environment name")
		}
	case BrainWeb:
	default:
		return errors.New("worker brain mode must be provider or web")
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
	if r.MemoryPressurePercent < 0 || r.MemoryPressurePercent > 100 {
		return errors.New("worker memory pressure percent must be in [0,100]")
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
	if strings.TrimSpace(r.GoBuildCache) != "" && !filepath.IsAbs(r.GoBuildCache) {
		return errors.New("worker shared Go build cache must be an absolute path")
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
	Version int    `json:"version"`
	Type    string `json:"type"`
	ID      uint64 `json:"id,omitempty"`
	Method  string `json:"method,omitempty"`
	Payload []byte `json:"payload,omitempty"`
	Error   string `json:"error,omitempty"`
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
	methodControlsSince        = "controls_since"
	methodEnterInputRequired   = "enter_input_required"
	methodPersistObservation   = "persist_observation"
	methodDecisionProjection   = "decision_projection_state"
	methodWebTurn              = "web_turn"
)

func marshalFrame(kind string, id uint64, method string, payload any, errText string) (frame, error) {
	var raw []byte
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return frame{}, err
		}
		// Keep the inner payload as owned JSON bytes, but carry those bytes as a
		// normal []byte in the outer frame. Go 1.27 routes encoding/json through
		// JSON v2, where RawMessage/jsontext.Value validation can surface intermittent
		// NUL-prefixed failures under framed worker traffic. The outer []byte is
		// base64 encoded and decoded back to the same JSON bytes before inner unmarshal.
		raw = append([]byte(nil), encoded...)
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
