//go:build windows

package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"mar/internal/agent"
	"mar/internal/domain"
	"mar/internal/processctl"
)

type ControlBackend interface {
	AttemptAuthoritative(context.Context, string, string, int64) (bool, error)
	HeartbeatAttempt(context.Context, string, string, int64, time.Duration) error
	LatestValidCheckpoint(context.Context, string) (domain.SemanticCheckpoint, bool, error)
	PublishCheckpoint(context.Context, string, string, int64, string, domain.SemanticCheckpointPayload) (domain.SemanticCheckpoint, error)
}

type ProcessConfig struct {
	Executable    string
	Arguments     []string
	Environment   []string
	LeaseDuration time.Duration
	StopTimeout   time.Duration
}

type ProcessRunner struct {
	backend    ControlBackend
	supervisor *processctl.Supervisor
	cfg        ProcessConfig
}

type frameRead struct {
	frame frame
	err   error
}

func NewProcessRunner(backend ControlBackend, supervisor *processctl.Supervisor, cfg ProcessConfig) (*ProcessRunner, error) {
	if backend == nil || supervisor == nil {
		return nil, errors.New("worker process runner requires backend and supervisor")
	}
	if strings.TrimSpace(cfg.Executable) == "" {
		return nil, errors.New("worker process executable is required")
	}
	if cfg.LeaseDuration <= 0 {
		cfg.LeaseDuration = time.Minute
	}
	if cfg.StopTimeout <= 0 {
		cfg.StopTimeout = 10 * time.Second
	}
	if len(cfg.Arguments) == 0 {
		cfg.Arguments = []string{"worker-run"}
	} else {
		cfg.Arguments = append([]string{}, cfg.Arguments...)
	}
	return &ProcessRunner{backend: backend, supervisor: supervisor, cfg: cfg}, nil
}

func (r *ProcessRunner) Run(ctx context.Context, start StartRequest) (agent.Result, processctl.TerminationProof, error) {
	if err := start.Validate(); err != nil {
		return agent.Result{}, processctl.TerminationProof{}, err
	}
	childStdin, parentToChild, err := os.Pipe()
	if err != nil {
		return agent.Result{}, processctl.TerminationProof{}, err
	}
	defer parentToChild.Close()
	parentFromChild, childStdout, err := os.Pipe()
	if err != nil {
		childStdin.Close()
		return agent.Result{}, processctl.TerminationProof{}, err
	}
	defer parentFromChild.Close()

	stderr := &boundedBuffer{limit: 64 << 10}
	env := r.cfg.Environment
	if env == nil {
		env = os.Environ()
	}
	tree, err := r.supervisor.Start(processctl.Spec{
		Attempt: processctl.AttemptRef{TaskID: start.Task.ID, AttemptID: start.Attempt.ID, RunEpoch: start.Attempt.RunEpoch},
		Path:    r.cfg.Executable,
		Args:    append([]string{}, r.cfg.Arguments...),
		Env:     env,
		Stdin:   childStdin,
		Stdout:  childStdout,
		Stderr:  stderr,
	})
	childStdin.Close()
	childStdout.Close()
	if err != nil {
		return agent.Result{}, processctl.TerminationProof{}, err
	}
	defer tree.CloseUnverified()

	encoder := json.NewEncoder(parentToChild)
	startFrame, err := marshalFrame(frameStart, 0, "", start, "")
	if err != nil {
		proof, stopErr := r.terminate(tree)
		return agent.Result{}, proof, errors.Join(err, stopErr)
	}
	if err := encoder.Encode(startFrame); err != nil {
		proof, stopErr := r.terminate(tree)
		return agent.Result{}, proof, errors.Join(fmt.Errorf("send worker start: %w", err), stopErr)
	}

	reads := make(chan frameRead, 1)
	go func() {
		decoder := json.NewDecoder(parentFromChild)
		for {
			var f frame
			err := decoder.Decode(&f)
			reads <- frameRead{frame: f, err: err}
			if err != nil {
				return
			}
		}
	}()

	heartbeatEvery := r.cfg.LeaseDuration / 3
	if heartbeatEvery < time.Second {
		heartbeatEvery = time.Second
	}
	ticker := time.NewTicker(heartbeatEvery)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			proof, stopErr := r.terminate(tree)
			return agent.Result{}, proof, errors.Join(ctx.Err(), stopErr)
		case <-ticker.C:
			if err := r.backend.HeartbeatAttempt(ctx, start.Task.ID, start.Attempt.ID, start.Attempt.RunEpoch, r.cfg.LeaseDuration); err != nil {
				proof, stopErr := r.terminate(tree)
				return agent.Result{}, proof, errors.Join(fmt.Errorf("worker heartbeat failed: %w", err), stopErr)
			}
		case read := <-reads:
			if read.err != nil {
				proof, waitErr := r.waitNatural(tree)
				if errors.Is(read.err, io.EOF) {
					read.err = errors.New("worker exited without terminal result")
				}
				return agent.Result{}, proof, errors.Join(read.err, waitErr, stderrError(stderr.String()))
			}
			if err := validateFrame(read.frame); err != nil {
				proof, stopErr := r.terminate(tree)
				return agent.Result{}, proof, errors.Join(err, stopErr)
			}
			switch read.frame.Type {
			case frameRequest:
				response := r.handleRequest(ctx, start, read.frame)
				if err := encoder.Encode(response); err != nil {
					proof, stopErr := r.terminate(tree)
					return agent.Result{}, proof, errors.Join(fmt.Errorf("send worker RPC response: %w", err), stopErr)
				}
			case frameResult:
				var result agent.Result
				if err := json.Unmarshal(read.frame.Payload, &result); err != nil {
					proof, stopErr := r.terminate(tree)
					return agent.Result{}, proof, errors.Join(fmt.Errorf("decode worker result: %w", err), stopErr)
				}
				proof, waitErr := r.waitNatural(tree)
				if waitErr != nil {
					return agent.Result{}, proof, waitErr
				}
				return result, proof, nil
			case frameError:
				proof, waitErr := r.waitNatural(tree)
				workerErr := errors.New(strings.TrimSpace(read.frame.Error))
				if workerErr.Error() == "" {
					workerErr = errors.New("worker reported an unspecified error")
				}
				return agent.Result{}, proof, errors.Join(workerErr, waitErr, stderrError(stderr.String()))
			default:
				proof, stopErr := r.terminate(tree)
				return agent.Result{}, proof, errors.Join(fmt.Errorf("unexpected worker frame type %s", read.frame.Type), stopErr)
			}
		}
	}
}

func (r *ProcessRunner) handleRequest(ctx context.Context, start StartRequest, request frame) frame {
	respond := func(payload any, err error) frame {
		errText := ""
		if err != nil {
			errText = err.Error()
		}
		response, marshalErr := marshalFrame(frameResponse, request.ID, request.Method, payload, errText)
		if marshalErr != nil {
			response, _ = marshalFrame(frameResponse, request.ID, request.Method, nil, marshalErr.Error())
		}
		return response
	}
	if request.ID == 0 || strings.TrimSpace(request.Method) == "" {
		return respond(nil, errors.New("worker RPC request identity is invalid"))
	}
	switch request.Method {
	case methodAttemptAuthoritative:
		var payload authorityRequest
		if err := json.Unmarshal(request.Payload, &payload); err != nil {
			return respond(nil, err)
		}
		if payload.TaskID != start.Task.ID || payload.AttemptID != start.Attempt.ID || payload.RunEpoch != start.Attempt.RunEpoch {
			return respond(nil, errors.New("worker authority request escaped assigned attempt"))
		}
		authoritative, err := r.backend.AttemptAuthoritative(ctx, payload.TaskID, payload.AttemptID, payload.RunEpoch)
		return respond(authorityResponse{Authoritative: authoritative}, err)
	case methodLatestCheckpoint:
		var payload latestCheckpointRequest
		if err := json.Unmarshal(request.Payload, &payload); err != nil {
			return respond(nil, err)
		}
		if payload.TaskID != start.Task.ID {
			return respond(nil, errors.New("worker checkpoint request escaped assigned task"))
		}
		checkpoint, available, err := r.backend.LatestValidCheckpoint(ctx, payload.TaskID)
		return respond(latestCheckpointResponse{Checkpoint: checkpoint, Available: available}, err)
	case methodPublishCheckpoint:
		var payload publishCheckpointRequest
		if err := json.Unmarshal(request.Payload, &payload); err != nil {
			return respond(nil, err)
		}
		if payload.TaskID != start.Task.ID || payload.AttemptID != start.Attempt.ID || payload.RunEpoch != start.Attempt.RunEpoch {
			return respond(nil, errors.New("worker checkpoint publish escaped assigned attempt"))
		}
		checkpoint, err := r.backend.PublishCheckpoint(ctx, payload.TaskID, payload.AttemptID, payload.RunEpoch, payload.CurrentRevision, payload.Payload)
		return respond(publishCheckpointResponse{Checkpoint: checkpoint}, err)
	default:
		return respond(nil, fmt.Errorf("unsupported worker RPC method %q", request.Method))
	}
}

func (r *ProcessRunner) terminate(tree *processctl.Tree) (processctl.TerminationProof, error) {
	ctx, cancel := context.WithTimeout(context.Background(), r.cfg.StopTimeout)
	defer cancel()
	return tree.TerminateAndConfirm(ctx)
}

func (r *ProcessRunner) waitNatural(tree *processctl.Tree) (processctl.TerminationProof, error) {
	ctx, cancel := context.WithTimeout(context.Background(), r.cfg.StopTimeout)
	defer cancel()
	return tree.WaitAndConfirm(ctx)
}

func stderrError(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return fmt.Errorf("worker stderr: %s", value)
}

type boundedBuffer struct {
	mu        sync.Mutex
	limit     int
	truncated bool
	buf       []byte
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	original := len(p)
	remaining := b.limit - len(b.buf)
	if remaining > 0 {
		if len(p) > remaining {
			p = p[:remaining]
			b.truncated = true
		}
		b.buf = append(b.buf, p...)
	} else if len(p) > 0 {
		b.truncated = true
	}
	return original, nil
}

func (b *boundedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	value := string(b.buf)
	if b.truncated {
		value += "\n...[MAR worker stderr truncated]..."
	}
	return value
}
