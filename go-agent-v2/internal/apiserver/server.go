// server.go — JSON-RPC over WebSocket 服务器（核心结构体与启动）。
//
// 架构:
//
//	WebSocket 连接 → JSON-RPC 2.0 消息解析 → 方法分发 → 响应
//	Agent 事件 → Notification 广播给所有连接
//
// 拆分说明:
//   - server_conn.go / server_transport.go: 连接管理与 WS/HTTP/SSE 传输
//   - server_event_handler.go / server_payload*.go: 事件转发与 payload 增强
//   - server_bootstrap_*.go: stores/skills/runtime 初始化装配
//   - server_lifecycle.go: 服务启动与运行时清理
package apiserver

import (
	"context"
	"encoding/json"
	"time"

	"github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/multi-agent/go-agent-v2/internal/apiserver/codexadapter"
	"github.com/multi-agent/go-agent-v2/internal/config"
	"github.com/multi-agent/go-agent-v2/internal/executor"
	"github.com/multi-agent/go-agent-v2/pkg/toolsdk/lsp"
	"github.com/multi-agent/go-agent-v2/internal/runner"
	"github.com/multi-agent/go-agent-v2/internal/service"
	skillsruntime "github.com/multi-agent/go-agent-v2/internal/skills"
	"github.com/multi-agent/go-agent-v2/pkg/toolsdk/tooladapter"
	"github.com/multi-agent/go-agent-v2/pkg/toolsdk/tools"
	"github.com/multi-agent/go-agent-v2/internal/uistate"
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
	// 状态分组说明
	// ========================================
	// Server 保留核心依赖与入口, 可变共享状态按职责分组嵌入:
	// connManagerState:      WebSocket 连接 + Server->Client pending 请求
	// diagnosticsCacheState: LSP diagnostics 缓存
	// codeRunState:          code_run 取消句柄 + agent workdir
	// turnTrackingState:     thread 序号 + 文件变更 + 自动回报跟踪
	// uiThrottleState:       ui/state/changed 节流
	// storeBundle:           资源/仪表盘/绑定存储依赖
	// toolCallState:         动态工具调用计数
	// sseState:              SSE 客户端集合
	// notifyHookState:       通知钩子
	// runtimeGuardState:     审批去重 + 一次性清理
	// threadAliasState:      thread alias 串行化锁
	// ========================================
	mgr        *runner.AgentManager
	lsp        *lsp.Manager
	lspTools   tools.LSPProvider
	cfg        *config.Config
	codeRunner *executor.CodeRunner // 代码块执行引擎
	// adapter 层: codex 专属能力聚合入口。
	codexAdapter *codexadapter.Adapter
	methods      map[string]Handler
	dynTools     map[string]tooladapter.RuntimeToolHandler // 动态工具注册表
	// submitAgentMessage 统一消息下发入口，便于测试替换。
	submitAgentMessage       func(agentID, prompt string, images, files []string) error
	lspDiagnosticsQueryTyped func(ctx context.Context, p lspDiagnosticsQueryParams) (any, error)

	storeBundle

	skillSvc     *service.SkillService
	skillsMgr    *skillsruntime.Manager
	skillsDir    string
	workspaceMgr *service.WorkspaceManager
	prefManager  *uistate.PreferenceManager
	uiRuntime    *uistate.RuntimeManager
	threadAliasState

	connManagerState
	diagnosticsCacheState

	toolCallState

	codeRunState
	turnTrackingState

	sseState

	notifyHookState

	uiThrottleState

	runtimeGuardState

	upgrader websocket.Upgrader
}

// Deps 服务器依赖注入。
type Deps struct {
	Manager   *runner.AgentManager
	LSP       *lsp.Manager
	Config    *config.Config
	DB        *pgxpool.Pool // 必需: 资源工具
	SkillsDir string        // skills 目录路径 (可选, 默认 app 缓存目录)
}

// New 创建服务器。
func New(deps Deps) *Server {
	s := &Server{
		mgr:      deps.Manager,
		lsp:      deps.LSP,
		cfg:      deps.Config,
		methods:  make(map[string]Handler),
		dynTools: make(map[string]tooladapter.RuntimeToolHandler),
		connManagerState: connManagerState{
			conns:   make(map[string]*connEntry),
			pending: make(map[int64]chan *Response),
		},
		diagnosticsCacheState: diagnosticsCacheState{
			diagCache: make(map[string][]lsp.Diagnostic),
		},
		codeRunState: codeRunState{
			activeCodeRuns: make(map[string]map[string]context.CancelFunc),
			agentWorkDirs:  make(map[string]string),
		},
		turnTrackingState: turnTrackingState{
			fileChangeByThread:          make(map[string][]string),
			orchestrationPendingReports: make(map[string]map[string]time.Time),
			orchestrationReportTTL:      defaultOrchestrationReportTTL,
		},
		toolCallState: toolCallState{
			toolCallCount: make(map[string]int64),
		},
		sseState: sseState{
			clients: safeSet[chan []byte]{
				items: make(map[chan []byte]struct{}),
			},
		},
		prefManager: uistate.NewPreferenceManager(nil),
		uiRuntime:   uistate.NewRuntimeManager(),
		uiThrottleState: uiThrottleState{
			uiThrottleEntries: make(map[string]*uiStateThrottleEntry),
		},
		upgrader: websocket.Upgrader{
			CheckOrigin: checkLocalOrigin,
		},
	}
	initStores(s, deps.DB)
	initRuntimeWiring(s)
	initSkills(s, deps.SkillsDir)
	s.registerMethods()

	applyStallConfig(s, deps.Config)
	initCodeRunner(s)

	applyInjectedPromptVisibilityPreference(s, context.Background())
	registerDynamicTools(s)
	return s
}
