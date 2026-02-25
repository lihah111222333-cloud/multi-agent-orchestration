// methods_thread.go — thread/* JSON-RPC 方法实现。
package apiserver

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/multi-agent/go-agent-v2/internal/apiserver/contracts"
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
	result, err := s.codexAdapter.ThreadStartFromParams(
		ctx,
		fmt.Sprintf("thread-%d-%d", time.Now().UnixMilli(), s.threadSeq.Add(1)),
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
	result, err := s.codexAdapter.ThreadForkByID(p.ThreadID)
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

func (s *Server) threadCompact(ctx context.Context, params json.RawMessage) (any, error) {
	return s.sendSlashCommand(ctx, params, "/compact")
}

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

func (s *Server) threadArchiveTyped(ctx context.Context, p threadIDParams) (any, error) {
	return s.codexAdapter.ThreadArchive(ctx, p.ThreadID)
}

func (s *Server) threadUnarchiveTyped(ctx context.Context, p threadIDParams) (any, error) {
	return s.codexAdapter.ThreadUnarchive(ctx, p.ThreadID)
}

type threadNameSetParams struct {
	ThreadID string `json:"threadId"`
	Name     string `json:"name"`
}

func (s *Server) threadNameSetTyped(ctx context.Context, p threadNameSetParams) (any, error) {
	return s.codexAdapter.ThreadNameSet(ctx, p.ThreadID, p.Name)
}

type threadRollbackParams struct {
	ThreadID  string `json:"threadId"`
	TurnIndex int    `json:"turnIndex"`
}

func (s *Server) threadRollbackTyped(_ context.Context, p threadRollbackParams) (any, error) {
	return s.codexAdapter.ThreadRollback(p.ThreadID, p.TurnIndex)
}

type threadMessagesParams struct {
	ThreadID string `json:"threadId"`
	Limit    int    `json:"limit,omitempty"`
	Before   int64  `json:"before,omitempty"` // cursor: id < before
}

func (s *Server) threadMessagesTyped(ctx context.Context, p threadMessagesParams) (any, error) {
	return s.codexAdapter.ThreadMessagesByID(ctx, p.ThreadID, p.Limit, p.Before)
}

// threadListResponse thread/list 响应。
type threadListResponse struct {
	Threads []contracts.ThreadListItem `json:"threads"`
}

func (s *Server) threadList(ctx context.Context, _ json.RawMessage) (any, error) {
	threads, err := s.codexAdapter.ThreadListDefault(ctx)
	if err != nil {
		return nil, err
	}
	return threadListResponse{Threads: threads}, nil
}

// threadLoadedListResponse thread/loaded/list 响应。
type threadLoadedListResponse struct {
	Threads []contracts.ThreadListItem `json:"threads"`
}

func (s *Server) threadLoadedList(ctx context.Context, _ json.RawMessage) (any, error) {
	threads, err := s.codexAdapter.ThreadLoadedListDefault(ctx)
	if err != nil {
		return nil, err
	}
	return threadLoadedListResponse{Threads: threads}, nil
}

func (s *Server) threadReadTyped(ctx context.Context, p threadIDParams) (any, error) {
	return s.codexAdapter.ThreadReadByID(ctx, p.ThreadID)
}

func (s *Server) threadResolveTyped(ctx context.Context, p threadIDParams) (any, error) {
	return s.codexAdapter.ThreadResolveByID(ctx, p.ThreadID)
}
