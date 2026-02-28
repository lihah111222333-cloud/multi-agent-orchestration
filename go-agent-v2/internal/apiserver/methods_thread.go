package apiserver

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
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

type threadInfo struct {
	ID         string `json:"id"`
	Status     string `json:"status,omitempty"`
	ForkedFrom string `json:"forkedFrom,omitempty"`
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
	return map[string]any{
		"thread":         threadInfo{ID: result.ThreadID, Status: result.Status},
		"model":          result.Model,
		"modelProvider":  result.ModelProvider,
		"cwd":            result.Cwd,
		"approvalPolicy": result.ApprovalPolicy,
	}, nil
}

type threadIDParams struct {
	ThreadID string `json:"threadId"`
}

type threadForkParams struct {
	ThreadID  string `json:"threadId"`
	TurnIndex *int   `json:"turnIndex,omitempty"`
}

func (s *Server) threadForkTyped(_ context.Context, p threadForkParams) (any, error) {
	result, err := s.codexAdapter.ThreadFork(p.ThreadID)
	if err != nil {
		return nil, err
	}
	return map[string]any{"thread": threadInfo{ID: result.ThreadID, ForkedFrom: result.ForkedFrom}}, nil
}

type threadResumeParams struct {
	ThreadID string `json:"threadId"`
	Path     string `json:"path,omitempty"`
	Cwd      string `json:"cwd,omitempty"`
	Model    string `json:"model,omitempty"`
}

func (s *Server) threadResumeTyped(ctx context.Context, p threadResumeParams) (any, error) {
	result, err := s.codexAdapter.ThreadResume(ctx, strings.TrimSpace(strings.SplitN(p.ThreadID, ",", 2)[0]), p.Path, p.Cwd, p.Model)
	if err != nil {
		return nil, err
	}
	return map[string]any{"thread": threadInfo{ID: result.ThreadID, Status: result.Status}, "model": result.Model}, nil
}

func (s *Server) threadRecoverTyped(ctx context.Context, p threadIDParams) (any, error) {
	result, err := s.codexAdapter.ThreadRecover(ctx, strings.TrimSpace(p.ThreadID))
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"thread":    threadInfo{ID: result.ThreadID, Status: result.Status},
		"recovered": result.Recovered,
		"mode":      result.Mode,
	}, nil
}

type threadNameSetParams struct {
	ThreadID string `json:"threadId"`
	Name     string `json:"name"`
}

type threadRollbackParams struct {
	ThreadID  string `json:"threadId"`
	NumTurns  *int   `json:"numTurns,omitempty"`
	TurnIndex *int   `json:"turnIndex,omitempty"`
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
	return s.codexAdapter.ThreadRollback(strings.TrimSpace(p.ThreadID), numTurns)
}

type threadMessagesParams struct {
	ThreadID string `json:"threadId"`
	Limit    int    `json:"limit,omitempty"`
	Before   int64  `json:"before,omitempty"`
}

func (s *Server) threadList(ctx context.Context, params json.RawMessage) (any, error) {
	threads, err := s.codexAdapter.ThreadList(ctx)
	if err != nil {
		return nil, err
	}
	return map[string]any{"data": filterThreadsByArchived(threads, params), "nextCursor": nil}, nil
}

func filterThreadsByArchived(threads []contracts.ThreadListItem, params json.RawMessage) []contracts.ThreadListItem {
	var p struct {
		Archived *bool `json:"archived,omitempty"`
	}
	_ = json.Unmarshal(params, &p)
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

type threadLoadedListParams struct {
	Cursor *string `json:"cursor,omitempty"`
	Limit  *uint32 `json:"limit,omitempty"`
}

func (s *Server) threadLoadedList(ctx context.Context, params json.RawMessage) (any, error) {
	var p threadLoadedListParams
	if err := json.Unmarshal(params, &p); err != nil && params != nil {
		return nil, pkgerr.Wrap(err, "Server.threadLoadedList", "invalid params")
	}
	data, nextCursor, err := s.codexAdapter.ThreadLoadedList(ctx, p.Cursor, p.Limit)
	if err != nil {
		return nil, err
	}
	return map[string]any{"data": data, "nextCursor": nextCursor}, nil
}
