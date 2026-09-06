//go:build windows

package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
	executor, err := aci.NewWindowsSandboxExecutor(start.WorkspacePath, start.SandboxReadPaths...)
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
		GoModuleCache:  start.GoModuleCache,
		CommandTimeout: start.CommandTimeout,
	}, executor)
	if err != nil {
		_ = sendChildError(encoder, err)
		return err
	}
	provider, err := openaichat.New(openaichat.Config{
		BaseURL:        start.Provider.BaseURL,
		APIKeyEnv:      start.Provider.APIKeyEnv,
		RequestTimeout: start.Provider.RequestTimeout,
	})
	if err != nil {
		_ = sendChildError(encoder, err)
		return err
	}
	gateway, err := model.NewGateway(provider)
	if err != nil {
		_ = sendChildError(encoder, err)
		return err
	}
	loop, err := agent.New(gateway, codingRuntime, agentContext, rpc, rpc, start.AgentProfile, start.AgentConfig)
	if err != nil {
		_ = sendChildError(encoder, err)
		return err
	}
	loop.WithControlStream(rpc)
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
