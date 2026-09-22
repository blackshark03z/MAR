package worker

import (
	"bytes"
	"context"
	"encoding/base64"
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

// HarnessConfig contains replaceable coding-harness cognition mechanics.
// It is compatibility configuration, not durable MAR execution authority.
type HarnessConfig struct {
	Provider     ProviderConfig
	AgentProfile agent.Profile
	AgentConfig  agent.Config
}

// ExecutionConfig contains the governed worker execution boundary that remains
// meaningful independently of the selected coding harness.
type ExecutionConfig struct {
	Task                  domain.Task
	Attempt               domain.ExecutionAttempt
	WorkspacePath         string
	SandboxReadPaths      []string
	GoModuleCache         string
	GoBuildCache          string
	CommandTimeout        time.Duration
	MemoryPressurePercent float64
	Capacity              WaitCapacity
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

// HarnessConfig projects the legacy flat wire contract onto the replaceable
// cognition/harness boundary without changing JSON compatibility.
func (r StartRequest) HarnessConfig() HarnessConfig {
	return HarnessConfig{Provider: r.Provider, AgentProfile: r.AgentProfile, AgentConfig: r.AgentConfig}
}

// ExecutionConfig projects the legacy flat wire contract onto the governed
// execution boundary. Returned path slices are caller-owned.
func (r StartRequest) ExecutionConfig() ExecutionConfig {
	return ExecutionConfig{
		Task: r.Task, Attempt: r.Attempt, WorkspacePath: r.WorkspacePath,
		SandboxReadPaths: append([]string(nil), r.SandboxReadPaths...),
		GoModuleCache:    r.GoModuleCache, GoBuildCache: r.GoBuildCache,
		CommandTimeout: r.CommandTimeout, MemoryPressurePercent: r.MemoryPressurePercent,
		Capacity: r.Capacity,
	}
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
	harness := r.HarnessConfig()
	switch harness.Provider.Mode() {
	case BrainProvider:
		if strings.TrimSpace(harness.Provider.BaseURL) == "" || strings.TrimSpace(harness.Provider.APIKeyEnv) == "" {
			return errors.New("provider brain mode requires model provider base URL and API key environment name")
		}
	case BrainWeb:
	default:
		return errors.New("worker brain mode must be provider or web")
	}
	if strings.TrimSpace(harness.AgentProfile.Model) == "" || strings.TrimSpace(harness.AgentProfile.BaseInstructions) == "" {
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
	Version int          `json:"version"`
	Type    string       `json:"type"`
	ID      uint64       `json:"id,omitempty"`
	Method  string       `json:"method,omitempty"`
	Payload framePayload `json:"payload,omitempty"`
	Error   string       `json:"error,omitempty"`
}

// framePayload preserves the existing base64-on-the-wire representation while
// owning the decode path explicitly. Go 1.27 routes encoding/json through the
// JSON v2 engine; relying on its implicit []byte codec under framed worker RPC
// has produced NUL-prefixed inner payloads in live decision_projection_state
// traffic. Decoding through a JSON string and an explicit base64 copy avoids
// that aliasing/special-case path without changing wire compatibility.
type framePayload []byte

func (p framePayload) MarshalJSON() ([]byte, error) {
	if p == nil {
		return []byte("null"), nil
	}
	return json.Marshal(base64.StdEncoding.EncodeToString([]byte(p)))
}

func (p *framePayload) UnmarshalJSON(data []byte) error {
	if p == nil {
		return errors.New("worker frame payload target is nil")
	}
	if string(data) == "null" {
		*p = nil
		return nil
	}
	var encoded string
	if err := json.Unmarshal(data, &encoded); err != nil {
		return err
	}
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return err
	}
	*p = append(framePayload(nil), decoded...)
	return nil
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
	var raw framePayload
	if payload != nil {
		// Encode into caller-owned storage before validation. Go 1.27's JSON v2
		// implementation has exhibited pooled-buffer aliasing in live worker RPC;
		// validating a slice returned directly by json.Marshal can therefore race
		// with reuse before we have taken ownership of the bytes.
		var buf bytes.Buffer
		if err := json.NewEncoder(&buf).Encode(payload); err != nil {
			return frame{}, err
		}
		encoded := bytes.TrimSpace(buf.Bytes())
		raw = append(framePayload(nil), encoded...)
		if !json.Valid(raw) {
			return frame{}, errors.New("worker frame payload marshal produced invalid JSON")
		}
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
