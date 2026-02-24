// methods_thread.go — thread/* JSON-RPC 方法实现。
package apiserver

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/multi-agent/go-agent-v2/internal/apiserver/codexadapter"
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
	dynamicTools := s.allDynamicToolSchemas()
	result, err := s.codexAdapter.ThreadStart(ctx, codexadapter.ThreadStartOptions{
		ThreadID:          fmt.Sprintf("thread-%d-%d", time.Now().UnixMilli(), s.threadSeq.Add(1)),
		Cwd:               p.Cwd,
		Model:             p.Model,
		ModelProvider:     p.ModelProvider,
		ApprovalPolicy:    p.ApprovalPolicy,
		DynamicTools:      dynamicTools,
		StartInstructions: s.resolveStartInstructionsForLaunch(ctx, dynamicTools),
		RegisterBinding:   s.registerBinding,
	})
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
	result, err := s.codexAdapter.ThreadFork(codexadapter.ThreadForkOptions{
		ThreadID:     p.ThreadID,
		WithThread:   s.withThread,
		NowUnixMilli: time.Now().UnixMilli,
	})
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

// threadListItem thread/list 响应项。
type threadListItem = codexadapter.ThreadListItem

// threadListResponse thread/list 响应。
type threadListResponse struct {
	Threads []threadListItem `json:"threads"`
}

func (s *Server) threadList(ctx context.Context, _ json.RawMessage) (any, error) {
	threads, err := s.codexAdapter.ThreadList(ctx, codexadapter.ThreadListOptions{
		MethodName:           "thread/list",
		LoadThreadArchiveMap: s.loadThreadArchiveMap,
		LoadThreadAliases:    s.loadThreadAliases,
		ApplyThreadAliases:   applyThreadAliases,
	})
	if err != nil {
		return nil, err
	}
	return threadListResponse{Threads: threads}, nil
}

// threadLoadedListResponse thread/loaded/list 响应。
type threadLoadedListResponse struct {
	Threads []threadListItem `json:"threads"`
}

func (s *Server) threadLoadedList(ctx context.Context, _ json.RawMessage) (any, error) {
	threads, err := s.codexAdapter.ThreadLoadedList(ctx, codexadapter.ThreadListOptions{
		MethodName:           "thread/loaded/list",
		LoadThreadArchiveMap: s.loadThreadArchiveMap,
		LoadThreadAliases:    s.loadThreadAliases,
		ApplyThreadAliases:   applyThreadAliases,
	})
	if err != nil {
		return nil, err
	}
	return threadLoadedListResponse{Threads: threads}, nil
}

func (s *Server) threadReadTyped(ctx context.Context, p threadIDParams) (any, error) {
	return s.codexAdapter.ThreadRead(ctx, codexadapter.ThreadReadOptions{
		ThreadID:   p.ThreadID,
		WithThread: s.withThread,
	})
}

func (s *Server) threadResolveTyped(ctx context.Context, p threadIDParams) (any, error) {
	return s.codexAdapter.ThreadResolve(ctx, codexadapter.ThreadResolveOptions{
		ThreadID:                    p.ThreadID,
		ResolvePrimaryCodexThreadID: s.resolvePrimaryCodexThreadID,
		IsLikelyCodexThreadID:       isLikelyCodexThreadID,
		ThreadExistsInHistory:       s.threadExistsInHistory,
	})
}

// threadMessagesParams thread/messages 请求参数。

const (
	threadMessageHydrationMaxRecords = 20000
	threadMessageHydrationPageSize   = 500
)

// streamRemainingHistory 后台分页加载剩余历史, 加载完后通过 AppendHistory 追加到 timeline。
//
// firstPage 已通过 HydrateHistory 加载, 此处只加载后续页并追加。

// msgsToRecords 将消息列表转为 hydration 记录。

func calculateHydrationLoadLimit(initialCount int, total int64) int {
	if initialCount < 0 {
		initialCount = 0
	}
	limit := initialCount
	if total > int64(limit) {
		limit = int(total)
	}
	if limit > threadMessageHydrationMaxRecords {
		limit = threadMessageHydrationMaxRecords
	}
	return limit
}

func (s *Server) loadThreadArchiveMap(ctx context.Context) (map[string]int64, error) {
	if s.prefManager == nil {
		return map[string]int64{}, nil
	}
	value, err := s.prefManager.Get(ctx, prefThreadArchivesChat)
	if err != nil {
		return nil, err
	}
	return normalizeThreadArchiveMap(value), nil
}

func normalizeThreadArchiveMap(value any) map[string]int64 {
	return codexadapter.NormalizeThreadArchiveMap(value)
}

func inferThreadArtifactKind(filename string) string {
	return codexadapter.InferThreadArtifactKind(filename)
}

func sanitizeArchiveName(raw string) string {
	return codexadapter.SanitizeArchiveName(raw)
}

func sanitizeArchiveNameStrict(raw string) (string, error) {
	return codexadapter.SanitizeArchiveNameStrict(raw)
}

func pathWithinRoot(root string, path string) (bool, error) {
	return codexadapter.PathWithinRoot(root, path)
}
