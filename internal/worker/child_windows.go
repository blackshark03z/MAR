//go:build windows

package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"mar/internal/aci"
	"mar/internal/agent"
	"mar/internal/contextengine"
	"mar/internal/domain"
	"mar/internal/model"
	"mar/internal/model/openaichat"
	"mar/internal/resourcegov"
)

type pressureAwareContextBuilder struct {
	inner      agent.ContextBuilder
	evict      func() int
	memoryLoad func() (float64, error)
	threshold  float64
}

func (b *pressureAwareContextBuilder) Build(ctx context.Context, req contextengine.Request) (contextengine.Pack, error) {
	if b.threshold > 0 && b.memoryLoad != nil {
		if load, err := b.memoryLoad(); err == nil && load >= b.threshold && b.evict != nil {
			b.evict()
		}
	}
	return b.inner.Build(ctx, req)
}

func monitorMemoryPressure(ctx context.Context, interval time.Duration, threshold float64, memoryLoad func() (float64, error), evict func() int) {
	if interval <= 0 || threshold <= 0 || memoryLoad == nil || evict == nil {
		return
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if load, err := memoryLoad(); err == nil && load >= threshold {
				evict()
			}
		}
	}
}

type rpcClient struct {
	decoder *json.Decoder
	encoder *json.Encoder
	nextID  uint64
}

type authorityRequest struct {
	TaskID    string `json:"task_id"`
	AttemptID string `json:"attempt_id"`
	RunEpoch  int64  `json:"run_epoch"`
}

type authorityResponse struct {
	Authoritative bool `json:"authoritative"`
}

type latestCheckpointRequest struct {
	TaskID string `json:"task_id"`
}

type latestCheckpointResponse struct {
	Checkpoint domain.SemanticCheckpoint `json:"checkpoint"`
	Available  bool                      `json:"available"`
}

type publishCheckpointRequest struct {
	TaskID          string                           `json:"task_id"`
	AttemptID       string                           `json:"attempt_id"`
	RunEpoch        int64                            `json:"run_epoch"`
	CurrentRevision string                           `json:"current_revision"`
	Payload         domain.SemanticCheckpointPayload `json:"payload"`
}

type publishCheckpointResponse struct {
	Checkpoint domain.SemanticCheckpoint `json:"checkpoint"`
}

type controlsSinceRequest struct {
	TaskID       string `json:"task_id"`
	AfterVersion int64  `json:"after_version"`
	Limit        int    `json:"limit"`
}

type controlsSinceResponse struct {
	Controls []domain.TaskControl `json:"controls"`
}

type inputRequiredRequest struct {
	TaskID    string `json:"task_id"`
	AttemptID string `json:"attempt_id"`
	RunEpoch  int64  `json:"run_epoch"`
}

type persistObservationRequest struct {
	TaskID         string `json:"task_id"`
	AttemptID      string `json:"attempt_id"`
	RunEpoch       int64  `json:"run_epoch"`
	ToolCallID     string `json:"tool_call_id"`
	Kind           string `json:"kind"`
	Raw            string `json:"raw"`
	SourceBytes    int64  `json:"source_bytes"`
	SourceComplete bool   `json:"source_complete"`
}

type persistObservationResponse struct {
	Artifact domain.ObservationArtifact `json:"artifact"`
}

type decisionProjectionResponse struct {
	State contextengine.DecisionProjectionState `json:"state"`
}

type webTurnRequest struct {
	TaskID    string            `json:"task_id"`
	AttemptID string            `json:"attempt_id"`
	RunEpoch  int64             `json:"run_epoch"`
	Request   model.TurnRequest `json:"request"`
}

type webTurnResponse struct {
	Response model.TurnResponse `json:"response"`
}

type webBrainProvider struct {
	rpc       *rpcClient
	taskID    string
	attemptID string
	runEpoch  int64
}

func (p *webBrainProvider) Turn(ctx context.Context, req model.TurnRequest) (model.TurnResponse, error) {
	if p == nil || p.rpc == nil {
		return model.TurnResponse{}, errors.New("web brain provider is unavailable")
	}
	return p.rpc.WebTurn(ctx, p.taskID, p.attemptID, p.runEpoch, req)
}

func RunChild(ctx context.Context, input io.Reader, output io.Writer) error {
	if input == nil || output == nil {
		return errors.New("worker child requires protocol input/output")
	}
	decoder := json.NewDecoder(input)
	encoder := json.NewEncoder(output)
	var first frame
	if err := decoder.Decode(&first); err != nil {
		return fmt.Errorf("read worker start frame: %w", err)
	}
	if err := validateFrame(first); err != nil || first.Type != frameStart {
		if err == nil {
			err = errors.New("worker protocol must begin with start frame")
		}
		_ = sendChildError(encoder, err)
		return err
	}
	var start StartRequest
	if err := json.Unmarshal(first.Payload, &start); err != nil {
		_ = sendChildError(encoder, err)
		return fmt.Errorf("decode worker start request: %w", err)
	}
	if err := start.Validate(); err != nil {
		_ = sendChildError(encoder, err)
		return err
	}
	harness := start.HarnessConfig()
	if harness.Provider.Mode() == BrainHarness {
		return runExternalHarnessChild(ctx, start, harness, encoder)
	}

	rpc := &rpcClient{decoder: decoder, encoder: encoder}
	repository, err := contextengine.NewGitRepository(8 << 20)
	if err != nil {
		_ = sendChildError(encoder, err)
		return err
	}
	contextBuilder, err := contextengine.New(repository, contextengine.Config{})
	if err != nil {
		_ = sendChildError(encoder, err)
		return err
	}
	var agentContext agent.ContextBuilder = contextBuilder
	if start.MemoryPressurePercent > 0 {
		agentContext = &pressureAwareContextBuilder{
			inner:      contextBuilder,
			evict:      contextBuilder.EvictOptionalCaches,
			memoryLoad: resourcegov.WindowsMemoryLoadPercent,
			threshold:  start.MemoryPressurePercent,
		}
		pressureCtx, stopPressure := context.WithCancel(ctx)
		pressureDone := make(chan struct{})
		go func() {
			defer close(pressureDone)
			monitorMemoryPressure(pressureCtx, time.Second, start.MemoryPressurePercent, resourcegov.WindowsMemoryLoadPercent, contextBuilder.EvictOptionalCaches)
		}()
		defer func() {
			stopPressure()
			<-pressureDone
		}()
	}
	goTempDir := aci.TaskGoTempDir(start.GoBuildCache, start.Task.ID)
	if err := os.MkdirAll(goTempDir, 0o755); err != nil {
		_ = sendChildError(encoder, fmt.Errorf("create task-scoped Go temp directory: %w", err))
		return err
	}
	var executor *aci.WindowsSandboxExecutor
	if !start.Task.Contract.Authority.LocalFileWrite && !start.Task.Contract.Authority.LocalGitWrite {
		executor, err = aci.NewWindowsReadOnlySandboxExecutorWithWritePaths(start.WorkspacePath, []string{start.GoBuildCache, goTempDir}, start.SandboxReadPaths...)
	} else {
		executor, err = aci.NewWindowsSandboxExecutorWithWritePaths(start.WorkspacePath, []string{start.GoBuildCache, goTempDir}, start.SandboxReadPaths...)
	}
	if err != nil {
		_ = sendChildError(encoder, err)
		return err
	}
	gitBroker, err := aci.NewContainedGitBroker()
	if err != nil {
		_ = sendChildError(encoder, err)
		return err
	}
	codingRuntime, err := aci.New(aci.Config{
		Root:           start.WorkspacePath,
		TaskID:         start.Task.ID,
		GitBroker:      gitBroker,
		GitExecutable:  gitBroker.ExecutablePath(),
		GoModuleCache:  start.GoModuleCache,
		GoBuildCache:   start.GoBuildCache,
		CommandTimeout: start.CommandTimeout,
	}, executor)
	if err != nil {
		_ = sendChildError(encoder, err)
		return err
	}
	var provider model.Provider
	switch harness.Provider.Mode() {
	case BrainWeb:
		provider = &webBrainProvider{rpc: rpc, taskID: start.Task.ID, attemptID: start.Attempt.ID, runEpoch: start.Attempt.RunEpoch}
	case BrainProvider:
		provider, err = openaichat.New(openaichat.Config{
			BaseURL:        harness.Provider.BaseURL,
			APIKeyEnv:      harness.Provider.APIKeyEnv,
			RequestTimeout: harness.Provider.RequestTimeout,
		})
		if err != nil {
			_ = sendChildError(encoder, err)
			return err
		}
	default:
		return errors.New("unsupported worker brain mode")
	}
	gateway, err := model.NewGateway(provider)
	if err != nil {
		_ = sendChildError(encoder, err)
		return err
	}
	loop, err := agent.New(gateway, codingRuntime, agentContext, rpc, rpc, harness.AgentProfile, harness.AgentConfig)
	if err != nil {
		_ = sendChildError(encoder, err)
		return err
	}
	loop.WithControlStream(rpc).WithObservationStore(rpc).WithDecisionProjectionSource(rpc)
	result, err := loop.Run(ctx, agent.RunRequest{
		TaskID:           start.Task.ID,
		AttemptID:        start.Attempt.ID,
		RunEpoch:         start.Attempt.RunEpoch,
		Root:             start.WorkspacePath,
		Contract:         start.Task.Contract,
		ExpectedRevision: start.Task.Contract.BaseRevision,
	})
	if err != nil {
		_ = sendChildError(encoder, err)
		return err
	}
	terminal, err := marshalFrame(frameResult, 0, "", result, "")
	if err != nil {
		return err
	}
	if err := encoder.Encode(terminal); err != nil {
		return fmt.Errorf("send worker result: %w", err)
	}
	return nil
}

func runExternalHarnessChild(ctx context.Context, start StartRequest, harness HarnessConfig, encoder *json.Encoder) error {
	input, err := start.ExternalHarnessInput()
	if err != nil {
		_ = sendChildError(encoder, err)
		return err
	}
	inputDir, err := os.MkdirTemp("", "mar-harness-input-")
	if err != nil {
		err = fmt.Errorf("create external harness input directory: %w", err)
		_ = sendChildError(encoder, err)
		return err
	}
	defer os.RemoveAll(inputDir)
	inputPath := filepath.Join(inputDir, "input.json")
	inputJSON, err := json.Marshal(input)
	if err != nil {
		err = fmt.Errorf("encode external harness input: %w", err)
		_ = sendChildError(encoder, err)
		return err
	}
	if err := os.WriteFile(inputPath, inputJSON, 0o600); err != nil {
		err = fmt.Errorf("write external harness input: %w", err)
		_ = sendChildError(encoder, err)
		return err
	}
	readPaths := append(append([]string(nil), start.SandboxReadPaths...), inputDir)

	var executor *aci.WindowsSandboxExecutor
	if !start.Task.Contract.Authority.LocalFileWrite && !start.Task.Contract.Authority.LocalGitWrite {
		executor, err = aci.NewWindowsReadOnlySandboxExecutorWithWritePaths(start.WorkspacePath, nil, readPaths...)
	} else {
		executor, err = aci.NewWindowsSandboxExecutor(start.WorkspacePath, readPaths...)
	}
	if err != nil {
		_ = sendChildError(encoder, err)
		return err
	}
	if executor.IsolationLevel() != aci.IsolationEnforcedSandbox {
		err := errors.New("external harness requires enforced sandbox isolation")
		_ = sendChildError(encoder, err)
		return err
	}
	runCtx := ctx
	cancel := func() {}
	if start.CommandTimeout > 0 {
		runCtx, cancel = context.WithTimeout(ctx, start.CommandTimeout)
	}
	defer cancel()
	result, err := executor.Run(runCtx, start.Task.ID, aci.ExecSpec{
		OperationID:    "external-harness",
		Path:           harness.Executable,
		Args:           append([]string(nil), harness.Arguments...),
		Dir:            start.WorkspacePath,
		Env:            externalHarnessEnvironment(inputPath),
		MaxOutputBytes: 64 << 10,
	})
	if err != nil {
		err = fmt.Errorf("external harness failed: %w", err)
		_ = sendChildError(encoder, err)
		return err
	}
	summary := strings.TrimSpace(result.Output)
	if summary == "" {
		summary = "external harness completed"
	}
	const maxSummaryBytes = 4 << 10
	if len(summary) > maxSummaryBytes {
		summary = summary[:maxSummaryBytes]
	}
	terminal, err := marshalFrame(frameResult, 0, "", agent.Result{
		Status:  agent.StatusCompletedCandidate,
		Summary: summary,
	}, "")
	if err != nil {
		return err
	}
	if err := encoder.Encode(terminal); err != nil {
		return fmt.Errorf("send external harness result: %w", err)
	}
	return nil
}

func externalHarnessEnvironment(inputPath string) []string {
	keys := []string{"SystemRoot", "WINDIR", "ComSpec", "PATH", "PATHEXT", "USERPROFILE", "LOCALAPPDATA", "APPDATA", "TEMP", "TMP", "ProgramFiles", "ProgramData"}
	env := make([]string, 0, len(keys)+1)
	for _, key := range keys {
		if value := os.Getenv(key); value != "" {
			env = append(env, key+"="+value)
		}
	}
	env = append(env, ExternalHarnessInputPathEnv+"="+inputPath)
	return env
}

func (c *rpcClient) AttemptAuthoritative(ctx context.Context, taskID, attemptID string, epoch int64) (bool, error) {
	var response authorityResponse
	if err := c.call(ctx, methodAttemptAuthoritative, authorityRequest{TaskID: taskID, AttemptID: attemptID, RunEpoch: epoch}, &response); err != nil {
		return false, err
	}
	return response.Authoritative, nil
}

func (c *rpcClient) LatestValidCheckpoint(ctx context.Context, taskID string) (domain.SemanticCheckpoint, bool, error) {
	var response latestCheckpointResponse
	if err := c.call(ctx, methodLatestCheckpoint, latestCheckpointRequest{TaskID: taskID}, &response); err != nil {
		return domain.SemanticCheckpoint{}, false, err
	}
	return response.Checkpoint, response.Available, nil
}

func (c *rpcClient) PublishCheckpoint(ctx context.Context, taskID, attemptID string, epoch int64, currentRevision string, payload domain.SemanticCheckpointPayload) (domain.SemanticCheckpoint, error) {
	var response publishCheckpointResponse
	request := publishCheckpointRequest{TaskID: taskID, AttemptID: attemptID, RunEpoch: epoch, CurrentRevision: currentRevision, Payload: payload}
	if err := c.call(ctx, methodPublishCheckpoint, request, &response); err != nil {
		return domain.SemanticCheckpoint{}, err
	}
	return response.Checkpoint, nil
}

func (c *rpcClient) ControlsSince(ctx context.Context, taskID string, afterVersion int64, limit int) ([]domain.TaskControl, error) {
	var response controlsSinceResponse
	request := controlsSinceRequest{TaskID: taskID, AfterVersion: afterVersion, Limit: limit}
	if err := c.call(ctx, methodControlsSince, request, &response); err != nil {
		return nil, err
	}
	return response.Controls, nil
}

func (c *rpcClient) EnterInputRequired(ctx context.Context, taskID, attemptID string, epoch int64) error {
	return c.call(ctx, methodEnterInputRequired, inputRequiredRequest{TaskID: taskID, AttemptID: attemptID, RunEpoch: epoch}, nil)
}

func (c *rpcClient) PersistObservation(ctx context.Context, taskID, attemptID string, epoch int64, toolCallID, kind, raw string, sourceBytes int64, sourceComplete bool) (domain.ObservationArtifact, error) {
	var response persistObservationResponse
	request := persistObservationRequest{TaskID: taskID, AttemptID: attemptID, RunEpoch: epoch, ToolCallID: toolCallID, Kind: kind, Raw: raw, SourceBytes: sourceBytes, SourceComplete: sourceComplete}
	if err := c.call(ctx, methodPersistObservation, request, &response); err != nil {
		return domain.ObservationArtifact{}, err
	}
	return response.Artifact, nil
}

func (c *rpcClient) DecisionProjectionState(ctx context.Context, taskID, attemptID string, epoch int64) (contextengine.DecisionProjectionState, error) {
	var response decisionProjectionResponse
	request := authorityRequest{TaskID: taskID, AttemptID: attemptID, RunEpoch: epoch}
	if err := c.call(ctx, methodDecisionProjection, request, &response); err != nil {
		return contextengine.DecisionProjectionState{}, err
	}
	return response.State, nil
}

func (c *rpcClient) WebTurn(ctx context.Context, taskID, attemptID string, epoch int64, req model.TurnRequest) (model.TurnResponse, error) {
	var response webTurnResponse
	request := webTurnRequest{TaskID: taskID, AttemptID: attemptID, RunEpoch: epoch, Request: req}
	if err := c.call(ctx, methodWebTurn, request, &response); err != nil {
		return model.TurnResponse{}, err
	}
	return response.Response, nil
}

func (c *rpcClient) call(ctx context.Context, method string, request any, response any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.nextID++
	requestFrame, err := marshalFrame(frameRequest, c.nextID, method, request, "")
	if err != nil {
		return err
	}
	if err := c.encoder.Encode(requestFrame); err != nil {
		return fmt.Errorf("send worker RPC %s: %w", method, err)
	}
	var reply frame
	if err := c.decoder.Decode(&reply); err != nil {
		return fmt.Errorf("read worker RPC %s response: %w", method, err)
	}
	if err := validateFrame(reply); err != nil {
		return err
	}
	if reply.Type != frameResponse || reply.ID != c.nextID || reply.Method != method {
		return errors.New("worker RPC response identity mismatch")
	}
	if strings.TrimSpace(reply.Error) != "" {
		return errors.New(reply.Error)
	}
	if response != nil && len(reply.Payload) != 0 {
		if err := json.Unmarshal(reply.Payload, response); err != nil {
			return fmt.Errorf("decode worker RPC %s response: %w", method, err)
		}
	}
	return nil
}

func sendChildError(encoder *json.Encoder, err error) error {
	if encoder == nil || err == nil {
		return nil
	}
	f, marshalErr := marshalFrame(frameError, 0, "", nil, err.Error())
	if marshalErr != nil {
		return marshalErr
	}
	return encoder.Encode(f)
}
