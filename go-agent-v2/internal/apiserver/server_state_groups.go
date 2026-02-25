package apiserver

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/multi-agent/go-agent-v2/internal/lsp"
	"github.com/multi-agent/go-agent-v2/internal/store"
)

// connManagerState 聚合 WebSocket 连接与 Server->Client 请求跟踪状态。
type connManagerState struct {
	// 连接管理 (支持多 IDE 同时连接)
	mu     sync.RWMutex
	conns  map[string]*connEntry // connID -> entry
	nextID atomic.Int64

	// Server -> Client 请求: 服务端发起请求, 等待客户端响应。
	pendingMu sync.Mutex
	pending   map[int64]chan *Response // requestID -> response channel
	nextReqID atomic.Int64
}

// diagnosticsCacheState 聚合 LSP 诊断缓存。
type diagnosticsCacheState struct {
	diagMu    sync.RWMutex
	diagCache map[string][]lsp.Diagnostic // uri -> diagnostics
}

// codeRunState 聚合 code_run 执行状态与 agent 工作目录缓存。
type codeRunState struct {
	codeRunMu      sync.Mutex
	activeCodeRuns map[string]map[string]context.CancelFunc // agentID -> runKey -> cancel
	codeRunSeq     atomic.Int64

	agentWorkDirMu sync.RWMutex
	agentWorkDirs  map[string]string // agentID -> abs cwd
}

// turnTrackingState 聚合 turn 序列与回报/文件变更追踪。
type turnTrackingState struct {
	threadSeq atomic.Int64 // thread/start 唯一序号

	fileChangeMu       sync.Mutex
	fileChangeByThread map[string][]string // threadID -> changed files

	orchestrationReportMu       sync.Mutex
	orchestrationPendingReports map[string]map[string]time.Time // workerID -> requesterID -> createdAt
	orchestrationReportTTL      time.Duration
}

// uiThrottleState 聚合 ui/state/changed 节流状态。
type uiThrottleState struct {
	uiThrottleMu      sync.Mutex
	uiThrottleEntries map[string]*uiStateThrottleEntry // key: threadID or agentID
}

// storeBundle 聚合 apiserver 资源/仪表盘相关存储依赖。
type storeBundle struct {
	// 资源 Store (编排工具依赖)
	dagStore          *store.TaskDAGStore
	cmdStore          *store.CommandCardStore
	promptStore       *store.PromptTemplateStore
	fileStore         *store.SharedFileStore
	workspaceRunStore *store.WorkspaceRunStore
	sysLogStore       *store.SystemLogStore

	// Dashboard Store (JSON-RPC dashboard/* 方法)
	agentStatusStore *store.AgentStatusStore
	auditLogStore    *store.AuditLogStore
	aiLogStore       *store.AILogStore
	busLogStore      *store.BusLogStore
	taskAckStore     *store.TaskAckStore
	taskTraceStore   *store.TaskTraceStore

	// Agent <-> Codex Thread 1:1 共生绑定 (根基约束, 不允许绕过)。
	bindingStore *store.AgentCodexBindingStore
}
