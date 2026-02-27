// methods_thread.go — thread/* JSON-RPC 方法实现。
package apiserver

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/multi-agent/go-agent-v2/internal/apiserver/contracts"
	pkgerr "github.com/multi-agent/go-agent-v2/pkg/errors"
)

type threadStartParams struct {
	Model                 string `json:"model,omitempty"`
	ModelProvider         string `json:"modelProvider,omitempty"`
	Cwd                   string `json:"cwd,omitempty"`
	ApprovalPolicy        string `json:"approvalPolicy,omitempty"`
	BaseInstructions      string `json:"baseInstructions,omitempty"`
	DeveloperInstructions string `json:"developerInstructions,omitempty"`
}

// threadInfo 通用线程信息。
type threadInfo struct {
	ID         string `json:"id"`
	Status     string `json:"status,omitempty"`
	ForkedFrom string `json:"forkedFrom,omitempty"`
}

// threadStartResponse thread/start 响应。
type threadStartResponse struct {
	Thread         threadInfo `json:"thread"`
	Model          string     `json:"model"`
	ModelProvider  string     `json:"modelProvider"`
	Cwd            string     `json:"cwd"`
	ApprovalPolicy string     `json:"approvalPolicy"`
}

func (s *Server) threadStartTyped(ctx context.Context, p threadStartParams) (any, error) {
	result, err := s.codexAdapter.ThreadStart(
		ctx,
		fmt.Sprintf("thread-%d-%d", time.Now().UnixMilli(), nextThreadSeqState(s)),
		p.Cwd,
		p.Model,
		p.ModelProvider,
		p.ApprovalPolicy,
	)
	if err != nil {
		return nil, err
	}
	return threadStartResponse{
		Thread: threadInfo{
			ID:     result.ThreadID,
			Status: result.Status,
		},
		Model:          result.Model,
		ModelProvider:  result.ModelProvider,
		Cwd:            result.Cwd,
		ApprovalPolicy: result.ApprovalPolicy,
	}, nil
}

// threadResumeParams thread/resume 请求参数。

// threadResumeResponse thread/resume 响应。

type threadIDParams struct {
	ThreadID string `json:"threadId"`
}

// threadForkParams thread/fork 请求参数。
type threadForkParams struct {
	ThreadID  string `json:"threadId"`
	TurnIndex *int   `json:"turnIndex,omitempty"`
}

// threadForkResponse thread/fork 响应。
type threadForkResponse struct {
	Thread threadInfo `json:"thread"`
}

func (s *Server) threadForkTyped(_ context.Context, p threadForkParams) (any, error) {
	result, err := s.codexAdapter.ThreadFork(p.ThreadID)
	if err != nil {
		return nil, err
	}
	return threadForkResponse{
		Thread: threadInfo{
			ID:         result.ThreadID,
			ForkedFrom: result.ForkedFrom,
		},
	}, nil
}

// threadNameSetParams thread/name/set 请求参数。

// threadRollbackParams thread/rollback 请求参数。

type threadResumeParams struct {
	ThreadID string `json:"threadId"`
	Path     string `json:"path,omitempty"`
	Cwd      string `json:"cwd,omitempty"`
	Model    string `json:"model,omitempty"`
}

type threadResumeResponse struct {
	Thread threadInfo `json:"thread"`
	Model  string     `json:"model"`
}

func (s *Server) threadResumeTyped(ctx context.Context, p threadResumeParams) (any, error) {
	result, err := s.codexAdapter.ThreadResume(ctx, p.ThreadID, p.Path, p.Cwd, p.Model)
	if err != nil {
		return nil, err
	}
	return threadResumeResponse{
		Thread: threadInfo{ID: result.ThreadID, Status: result.Status},
		Model:  result.Model,
	}, nil
}

type threadRecoverResponse struct {
	Thread    threadInfo `json:"thread"`
	Recovered bool       `json:"recovered"`
	Mode      string     `json:"mode"`
}

func (s *Server) threadRecoverTyped(ctx context.Context, p threadIDParams) (any, error) {
	result, err := s.codexAdapter.ThreadRecover(ctx, p.ThreadID)
	if err != nil {
		return nil, err
	}
	return threadRecoverResponse{
		Thread: threadInfo{
			ID:     result.ThreadID,
			Status: result.Status,
		},
		Recovered: result.Recovered,
		Mode:      result.Mode,
	}, nil
}

type threadNameSetParams struct {
	ThreadID string `json:"threadId"`
	Name     string `json:"name"`
}

type threadRollbackParams struct {
	ThreadID  string `json:"threadId"`
	NumTurns  *int   `json:"numTurns,omitempty"`
	TurnIndex *int   `json:"turnIndex,omitempty"` // legacy alias
}

func (s *Server) threadRollbackTyped(_ context.Context, p threadRollbackParams) (any, error) {
	numTurns := 0
	if p.NumTurns != nil {
		numTurns = *p.NumTurns
	} else if p.TurnIndex != nil {
		numTurns = *p.TurnIndex
	}
	if numTurns <= 0 {
		return nil, pkgerr.New("Server.threadRollback", "numTurns must be >= 1")
	}
	return s.codexAdapter.ThreadRollback(p.ThreadID, numTurns)
}

type threadMessagesParams struct {
	ThreadID string `json:"threadId"`
	Limit    int    `json:"limit,omitempty"`
	Before   int64  `json:"before,omitempty"` // cursor: id < before
}

// threadListResponse thread/list 响应。
type threadListResponse struct {
	Data       []contracts.ThreadListItem `json:"data"`
	NextCursor *string                    `json:"nextCursor"`
}

func (s *Server) threadList(ctx context.Context, params json.RawMessage) (any, error) {
	threads, err := s.codexAdapter.ThreadList(ctx)
	if err != nil {
		return nil, err
	}
	threads = filterThreadsByArchived(threads, params)
	return threadListResponse{Data: threads, NextCursor: nil}, nil
}

func filterThreadsByArchived(threads []contracts.ThreadListItem, params json.RawMessage) []contracts.ThreadListItem {
	var p struct {
		Archived *bool `json:"archived,omitempty"`
	}
	if len(params) > 0 && string(params) != "null" {
		_ = json.Unmarshal(params, &p)
	}
	if p.Archived == nil {
		return threads
	}
	wantArchived := *p.Archived
	filtered := make([]contracts.ThreadListItem, 0, len(threads))
	for _, t := range threads {
		if t.Archived == wantArchived {
			filtered = append(filtered, t)
		}
	}
	return filtered
}

// threadLoadedListResponse thread/loaded/list 响应。
type threadLoadedListResponse struct {
	Data       []string `json:"data"`
	NextCursor *string  `json:"nextCursor"`
}

type threadLoadedListParams struct {
	Cursor *string `json:"cursor,omitempty"`
	Limit  *uint32 `json:"limit,omitempty"`
}

func decodeThreadLoadedListParams(raw json.RawMessage) (threadLoadedListParams, error) {
	var p threadLoadedListParams
	if raw == nil {
		return p, nil
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return p, pkgerr.Wrap(err, "Server.threadLoadedList", "invalid params")
	}
	return p, nil
}

func (s *Server) threadLoadedList(ctx context.Context, params json.RawMessage) (any, error) {
	p, err := decodeThreadLoadedListParams(params)
	if err != nil {
		return nil, err
	}
	data, nextCursor, err := s.codexAdapter.ThreadLoadedList(ctx, p.Cursor, p.Limit)
	if err != nil {
		return nil, err
	}
	return threadLoadedListResponse{Data: data, NextCursor: nextCursor}, nil
}
