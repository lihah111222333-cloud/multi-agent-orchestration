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
	"time"

	"github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/multi-agent/go-agent-v2/internal/apiserver/codexadapter"
	"github.com/multi-agent/go-agent-v2/internal/config"
	"github.com/multi-agent/go-agent-v2/internal/executor"
	"github.com/multi-agent/go-agent-v2/internal/lsp"
	"github.com/multi-agent/go-agent-v2/internal/runner"
	"github.com/multi-agent/go-agent-v2/internal/service"
	skillsruntime "github.com/multi-agent/go-agent-v2/internal/skills"
	"github.com/multi-agent/go-agent-v2/internal/tooladapter"
	"github.com/multi-agent/go-agent-v2/internal/tools"
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
			sseClients: make(map[chan []byte]struct{}),
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
	if s.mgr != nil {
		s.submitAgentMessage = s.mgr.Submit
	}
	s.codexAdapter = newCodexAdapter(s)
	s.lspTools = lsp.NewToolHandlers(s.lsp, diagnosticsAccessor(s))
	s.lspDiagnosticsQueryTyped = func(_ context.Context, p lspDiagnosticsQueryParams) (any, error) {
		return s.lspTools.DiagnosticsQuery(p.FilePath), nil
	}

	initStores(s, deps.DB)
	initSkills(s, deps.SkillsDir)
	s.registerMethods()

	// 从 Config 加载 stall 参数
	if deps.Config != nil {
		if deps.Config.StallThresholdSec > 0 {
			s.codexAdapter.SetStallThreshold(time.Duration(deps.Config.StallThresholdSec) * time.Second)
		}
		if deps.Config.StallHeartbeatSec > 0 {
			s.codexAdapter.SetStallHeartbeat(time.Duration(deps.Config.StallHeartbeatSec) * time.Second)
		}
	}

	// 代码执行引擎 (无外部依赖, 仅需 workDir)
	workDir, wdErr := os.Getwd()
	if wdErr != nil {
		logger.Warn("app-server: resolve working directory failed; fallback to current path", logger.FieldError, wdErr)
		workDir = "."
	}
	if cr, crErr := executor.NewCodeRunner(workDir); crErr != nil {
		logger.Warn("app-server: code runner unavailable", logger.FieldError, crErr)
	} else {
		s.codeRunner = cr
	}

	applyInjectedPromptVisibilityPreference(s, context.Background())
	registerDynamicTools(s)
	return s
}

// ListenAndServe 启动 WebSocket 服务器。
//
// addr 格式: "ws://127.0.0.1:4500" 或 "127.0.0.1:4500"。
func (s *Server) ListenAndServe(ctx context.Context, addr string) error {
	defer s.cleanupRuntimeResources()

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	// 解析地址: 去掉 ws:// 前缀
	host := strings.TrimPrefix(addr, "ws://")
	host = strings.TrimPrefix(host, "wss://")

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { handleUpgrade(s, w, r) })    // WebSocket
	mux.HandleFunc("/rpc", func(w http.ResponseWriter, r *http.Request) { handleHTTPRPC(s, w, r) }) // HTTP JSON-RPC (调试模式)
	mux.HandleFunc("/events", func(w http.ResponseWriter, r *http.Request) { handleSSE(s, w, r) })  // SSE 事件流 (调试模式)

	srv := &http.Server{
		Addr:              host,
		Handler:           recoveryMiddleware(corsMiddleware(mux)),
		BaseContext:       func(_ net.Listener) context.Context { return runCtx },
		ReadHeaderTimeout: 10 * time.Second,
	}

	ln, err := net.Listen("tcp", host)
	if err != nil {
		return pkgerr.Wrap(err, "Server.ListenAndServe", "listen")
	}
	defer func() { _ = ln.Close() }()

	// 优雅关闭: 给活跃连接 5 秒完成处理
	util.SafeGo(func() {
		<-runCtx.Done()
		if ctx.Err() == nil {
			return
		}
		logger.Info("app-server: shutdown trigger", "ctx_err", ctx.Err())
		logger.Info("app-server: shutting down")
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		if shutdownErr := srv.Shutdown(shutdownCtx); shutdownErr != nil && shutdownErr != http.ErrServerClosed {
			logger.Warn("app-server: shutdown error", logger.FieldError, shutdownErr)
			return
		}
		logger.Info("app-server: shutdown completed")
	})

	logger.Info("app-server: listening", logger.FieldAddr, host)
	if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
		return pkgerr.Wrap(err, "Server.ListenAndServe", "listen")
	}
	return nil
}

func (s *Server) cleanupRuntimeResources() {
	if s == nil {
		return
	}
	doRuntimeCleanupState(s, func() {
		cancelAllCodeRuns(s)
		if s.codeRunner != nil {
			s.codeRunner.Cleanup()
		}
		clearAllAgentWorkDirsState(s)
	})
}
