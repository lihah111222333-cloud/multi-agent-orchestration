// server.go — JSON-RPC over WebSocket 服务器（核心结构体与启动）。
//
// 架构:
//
//	WebSocket 连接 → JSON-RPC 2.0 消息解析 → 方法分发 → 响应
//	Agent 事件 → Notification 广播给所有连接
//
// 拆分说明:
//   - server_conn.go:          连接管理、类型定义 (connEntry)、广播、SendRequest
//   - server_payload.go:       事件提取、通知、节流、UI 状态同步、HTTP-RPC 兼容层
//   - server_approval.go:      审批事件处理
//   - server_dynamic_tools.go: LSP/编排/资源 动态工具注册与调用
package apiserver

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/multi-agent/go-agent-v2/internal/config"
	"github.com/multi-agent/go-agent-v2/internal/executor"
	"github.com/multi-agent/go-agent-v2/internal/lsp"
	"github.com/multi-agent/go-agent-v2/internal/runner"
	"github.com/multi-agent/go-agent-v2/internal/service"
	"github.com/multi-agent/go-agent-v2/internal/store"
	"github.com/multi-agent/go-agent-v2/internal/uistate"
	pkgerr "github.com/multi-agent/go-agent-v2/pkg/errors"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
	"github.com/multi-agent/go-agent-v2/pkg/util"
)

// Handler JSON-RPC 方法处理器。
type Handler func(ctx context.Context, params json.RawMessage) (any, error)

const (
	maxConnections    = 100      // 最大并发连接数
	maxMessageSize    = 4 << 20  // 4MB 消息大小限制
	connOutboxSize    = 256      // 单连接发送缓冲
	connBacklogCut    = 256 - 16 // 单连接过载水位
	uiStateThrottleMs = 500      // ui/state/changed 全局节流间隔 (ms)
)

// Server JSON-RPC WebSocket 服务器。
type Server struct {
	// ========================================
	// 锁职责说明
	// ========================================
	// 以下锁保护各自独立的数据, 不存在嵌套获取关系。
	// mu:           conns map (WebSocket 连接管理)
	// pendingMu:    pending map (Server→Client 请求跟踪)
	// diagMu:       diagCache (LSP 诊断缓存)
	// toolCallMu:   toolCallCount (工具调用计数)
	// fileChangeMu: fileChangeByThread (文件变更跟踪)
	// skillsMu:     agentSkills (技能配置)
	// sseMu:        sseClients (SSE 推送)
	// notifyHookMu: notifyHook (桌面端通知钩子)
	// turnMu:       activeTurns (turn 生命周期跟踪)
	// ========================================
	mgr        *runner.AgentManager
	lsp        *lsp.Manager
	cfg        *config.Config
	codeRunner *executor.CodeRunner // 代码块执行引擎
	methods    map[string]Handler
	dynTools   map[string]func(json.RawMessage) string // 动态工具注册表
	// submitAgentMessage 统一消息下发入口，便于测试替换。
	submitAgentMessage func(agentID, prompt string, images, files []string) error

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
	skillSvc         *service.SkillService
	skillsDir        string
	workspaceMgr     *service.WorkspaceManager
	prefManager      *uistate.PreferenceManager
	uiRuntime        *uistate.RuntimeManager
	threadAliasMu    sync.Mutex

	// Agent ↔ Codex Thread 1:1 共生绑定 (根基约束, 不允许绕过)。
	bindingStore *store.AgentCodexBindingStore

	// 连接管理 (支持多 IDE 同时连接)
	mu     sync.RWMutex
	conns  map[string]*connEntry // connID → entry
	nextID atomic.Int64

	// § 二 Server → Client 请求: 服务端发起请求, 等待客户端响应
	pendingMu sync.Mutex
	pending   map[int64]chan *Response // requestID → response channel
	nextReqID atomic.Int64

	threadSeq atomic.Int64 // thread/start 唯一序号

	// LSP 诊断缓存 (uri → diagnostics)
	diagMu    sync.RWMutex
	diagCache map[string][]lsp.Diagnostic

	// 动态工具调用计数 (可观测性)
	toolCallMu    sync.Mutex
	toolCallCount map[string]int64 // toolName → count

	// 文件变更跟踪 (threadId → 当前变更文件列表)
	fileChangeMu       sync.Mutex
	fileChangeByThread map[string][]string

	// turn 生命周期跟踪 (threadId → active turn)
	turnMu              sync.Mutex
	activeTurns         map[string]*trackedTurn
	turnWatchdogTimeout time.Duration
	turnSummaryCache    map[string]trackedTurnSummaryCacheEntry
	turnSummaryTTL      time.Duration
	stallThreshold      time.Duration // 无事件多久(秒)触发 stall 自动中断
	stallHeartbeat      time.Duration // dynamic tool call / 审批等待时的保活心跳间隔

	// 委托消息自动回报跟踪 (workerAgentID -> requesterAgentID -> createdAt)
	orchestrationReportMu       sync.Mutex
	orchestrationPendingReports map[string]map[string]time.Time
	orchestrationReportTTL      time.Duration

	// Per-session 技能配置 (agentID → skills 列表)
	skillsMu    sync.RWMutex
	agentSkills map[string][]string // agentID → ["skill1", "skill2"]

	// SSE 客户端 (debug 模式浏览器事件推送)
	sseMu      sync.RWMutex
	sseClients map[chan []byte]struct{}

	// 通知钩子 (给桌面端桥接使用)
	notifyHookMu sync.RWMutex
	notifyHook   func(method string, params any)

	// ui/state/changed 节流 (key = threadId or agent_id)
	uiThrottleMu      sync.Mutex
	uiThrottleEntries map[string]*uiStateThrottleEntry

	// 审批去重: 防止同一 agentID+method 并发双重处理
	approvalInFlight sync.Map // key: "agentID:method"

	upgrader websocket.Upgrader
}

// Deps 服务器依赖注入。
type Deps struct {
	Manager   *runner.AgentManager
	LSP       *lsp.Manager
	Config    *config.Config
	DB        *pgxpool.Pool // 必需: 资源工具
	SkillsDir string        // .agent/skills 目录路径 (可选, 默认 ".agent/skills")
}

// New 创建服务器。
func New(deps Deps) *Server {
	s := &Server{
		mgr:                         deps.Manager,
		lsp:                         deps.LSP,
		cfg:                         deps.Config,
		methods:                     make(map[string]Handler),
		dynTools:                    make(map[string]func(json.RawMessage) string),
		conns:                       make(map[string]*connEntry),
		pending:                     make(map[int64]chan *Response),
		diagCache:                   make(map[string][]lsp.Diagnostic),
		toolCallCount:               make(map[string]int64),
		fileChangeByThread:          make(map[string][]string),
		activeTurns:                 make(map[string]*trackedTurn),
		turnWatchdogTimeout:         defaultTurnWatchdogTimeout,
		stallThreshold:              defaultStallThreshold,
		stallHeartbeat:              defaultStallHeartbeat,
		turnSummaryCache:            make(map[string]trackedTurnSummaryCacheEntry),
		turnSummaryTTL:              defaultTrackedTurnSummaryTTL,
		orchestrationPendingReports: make(map[string]map[string]time.Time),
		orchestrationReportTTL:      defaultOrchestrationReportTTL,
		agentSkills:                 make(map[string][]string),
		sseClients:                  make(map[chan []byte]struct{}),
		prefManager:                 uistate.NewPreferenceManager(nil),
		uiRuntime:                   uistate.NewRuntimeManager(),
		uiThrottleEntries:           make(map[string]*uiStateThrottleEntry),
		upgrader: websocket.Upgrader{
			CheckOrigin: checkLocalOrigin,
		},
	}
	if s.mgr != nil {
		s.submitAgentMessage = s.mgr.Submit
	}
	if deps.DB != nil {
		s.prefManager = uistate.NewPreferenceManager(store.NewUIPreferenceStore(deps.DB))
		s.dagStore = store.NewTaskDAGStore(deps.DB)
		s.cmdStore = store.NewCommandCardStore(deps.DB)
		s.promptStore = store.NewPromptTemplateStore(deps.DB)
		s.fileStore = store.NewSharedFileStore(deps.DB)
		s.workspaceRunStore = store.NewWorkspaceRunStore(deps.DB)
		s.sysLogStore = store.NewSystemLogStore(deps.DB)
		// Dashboard stores
		s.agentStatusStore = store.NewAgentStatusStore(deps.DB)
		s.auditLogStore = store.NewAuditLogStore(deps.DB)
		s.aiLogStore = store.NewAILogStore(deps.DB)
		s.busLogStore = store.NewBusLogStore(deps.DB)
		s.taskAckStore = store.NewTaskAckStore(deps.DB)
		s.taskTraceStore = store.NewTaskTraceStore(deps.DB)
		s.bindingStore = store.NewAgentCodexBindingStore(deps.DB)

		if s.cfg != nil {
			maxFileBytes := int64(s.cfg.OrchestrationWorkspaceMaxFileBytes)
			maxTotalBytes := int64(s.cfg.OrchestrationWorkspaceMaxTotalBytes)
			workspaceMgr, mgrErr := service.NewWorkspaceManager(
				s.workspaceRunStore,
				s.cfg.OrchestrationWorkspaceRoot,
				s.cfg.OrchestrationWorkspaceMaxFiles,
				maxFileBytes,
				maxTotalBytes,
			)
			if mgrErr != nil {
				logger.Warn("app-server: workspace manager unavailable", logger.FieldError, mgrErr)
			} else {
				s.workspaceMgr = workspaceMgr
				logger.Info("app-server: workspace manager enabled", logger.FieldRoot, workspaceMgr.RootDir())
			}
		}
		logger.Info("app-server: resource tools + dashboard enabled")
	}
	// Skills service (filesystem, no DB required)
	skillsDir := deps.SkillsDir
	if skillsDir == "" {
		skillsDir = ".agent/skills"
	}
	s.skillsDir = skillsDir
	s.skillSvc = service.NewSkillService(skillsDir)
	s.registerMethods()

	// 从 Config 加载 stall 参数
	if deps.Config != nil {
		if deps.Config.StallThresholdSec > 0 {
			s.stallThreshold = time.Duration(deps.Config.StallThresholdSec) * time.Second
		}
		if deps.Config.StallHeartbeatSec > 0 {
			s.stallHeartbeat = time.Duration(deps.Config.StallHeartbeatSec) * time.Second
		}
	}

	// 代码执行引擎 (无外部依赖, 仅需 workDir)
	workDir, _ := os.Getwd()
	if cr, crErr := executor.NewCodeRunner(workDir); crErr != nil {
		logger.Warn("app-server: code runner unavailable", logger.FieldError, crErr)
	} else {
		s.codeRunner = cr
	}

	s.registerDynamicTools()
	return s
}

// ListenAndServe 启动 WebSocket 服务器。
//
// addr 格式: "ws://127.0.0.1:4500" 或 "127.0.0.1:4500"。
func (s *Server) ListenAndServe(ctx context.Context, addr string) error {
	// 解析地址: 去掉 ws:// 前缀
	host := strings.TrimPrefix(addr, "ws://")
	host = strings.TrimPrefix(host, "wss://")

	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleUpgrade)    // WebSocket
	mux.HandleFunc("/rpc", s.handleHTTPRPC) // HTTP JSON-RPC (调试模式)
	mux.HandleFunc("/events", s.handleSSE)  // SSE 事件流 (调试模式)

	srv := &http.Server{
		Addr:              host,
		Handler:           recoveryMiddleware(corsMiddleware(mux)),
		BaseContext:       func(_ net.Listener) context.Context { return ctx },
		ReadHeaderTimeout: 10 * time.Second,
	}

	// 优雅关闭: 给活跃连接 5 秒完成处理
	util.SafeGo(func() {
		<-ctx.Done()
		logger.Info("app-server: shutdown trigger", "ctx_err", ctx.Err())
		logger.Info("app-server: shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			logger.Warn("app-server: shutdown error", logger.FieldError, err)
			return
		}
		logger.Info("app-server: shutdown completed")
	})

	logger.Info("app-server: listening", logger.FieldAddr, host)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return pkgerr.Wrap(err, "Server.ListenAndServe", "listen")
	}
	return nil
}
