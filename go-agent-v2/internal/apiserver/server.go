// server.go — JSON-RPC over WebSocket 服务器。
//
// 架构:
//
//	WebSocket 连接 → JSON-RPC 2.0 消息解析 → 方法分发 → 响应
//	Agent 事件 → Notification 广播给所有连接
package apiserver

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/multi-agent/go-agent-v2/internal/config"
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

type wsOutbound struct {
	msgType int
	data    []byte
}

// connEntry WebSocket 连接 + 写锁 (gorilla/websocket 不安全并发写)。
type connEntry struct {
	ws        *websocket.Conn
	wrMu      sync.Mutex // 序列化所有写操作
	outbox    chan wsOutbound
	closeCh   chan struct{}
	closeOnce sync.Once
}

func newConnEntry(ws *websocket.Conn) *connEntry {
	return &connEntry{
		ws:      ws,
		outbox:  make(chan wsOutbound, connOutboxSize),
		closeCh: make(chan struct{}),
	}
}

// writeMsg 线程安全地写入 WebSocket 消息。
func (c *connEntry) writeMsg(msgType int, data []byte) error {
	c.wrMu.Lock()
	defer c.wrMu.Unlock()
	_ = c.ws.SetWriteDeadline(time.Now().Add(10 * time.Second))
	return c.ws.WriteMessage(msgType, data)
}

func (c *connEntry) enqueue(msgType int, data []byte) bool {
	select {
	case <-c.closeCh:
		return false
	default:
	}
	select {
	case c.outbox <- wsOutbound{msgType: msgType, data: data}:
		return true
	default:
		return false
	}
}

func (c *connEntry) outboxDepth() int {
	return len(c.outbox)
}

func (c *connEntry) closeNow() {
	c.closeOnce.Do(func() {
		close(c.closeCh)
		if c.ws != nil {
			_ = c.ws.Close()
		}
	})
}

func (c *connEntry) writeLoop() error {
	for {
		select {
		case <-c.closeCh:
			return nil
		case msg := <-c.outbox:
			if err := c.writeMsg(msg.msgType, msg.data); err != nil {
				return err
			}
		}
	}
}

const (
	maxConnections    = 100      // 最大并发连接数
	maxMessageSize    = 4 << 20  // 4MB 消息大小限制
	connOutboxSize    = 256      // 单连接发送缓冲
	connBacklogCut    = 256 - 16 // 单连接过载水位
	uiStateThrottleMs = 500      // ui/state/changed 全局节流间隔 (ms)
)

// uiStateThrottleEntry 全局节流状态。
type uiStateThrottleEntry struct {
	lastEmit time.Time      // 上次实际发送时间
	timer    *time.Timer    // trailing timer (保证最终一致)
	pending  map[string]any // 最新 payload (合并)
}

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
	mgr      *runner.AgentManager
	lsp      *lsp.Manager
	cfg      *config.Config
	methods  map[string]Handler
	dynTools map[string]func(json.RawMessage) string // 动态工具注册表
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

	s.registerDynamicTools()
	return s
}

func (s *Server) skillsDirectory() string {
	dir := strings.TrimSpace(s.skillsDir)
	if dir == "" {
		return ".agent/skills"
	}
	return dir
}

// registerDynamicTools 注册所有动态工具处理函数。
//
// 新增工具只需一行: s.dynTools["tool_name"] = s.toolHandler
func (s *Server) registerDynamicTools() {
	// LSP 工具
	s.dynTools["lsp_hover"] = s.lspHover
	s.dynTools["lsp_open_file"] = s.lspOpenFile
	s.dynTools["lsp_diagnostics"] = s.lspDiagnostics

	// 编排工具
	s.dynTools["orchestration_list_agents"] = func(_ json.RawMessage) string { return s.orchestrationListAgents() }
	s.dynTools["orchestration_send_message"] = s.orchestrationSendMessage
	s.dynTools["orchestration_launch_agent"] = s.orchestrationLaunchAgent
	s.dynTools["orchestration_stop_agent"] = s.orchestrationStopAgent

	// 资源工具
	s.dynTools["task_create_dag"] = s.resourceTaskCreateDAG
	s.dynTools["task_get_dag"] = s.resourceTaskGetDAG
	s.dynTools["task_update_node"] = s.resourceTaskUpdateNode
	s.dynTools["command_list"] = s.resourceCommandList
	s.dynTools["command_get"] = s.resourceCommandGet
	s.dynTools["prompt_list"] = s.resourcePromptList
	s.dynTools["prompt_get"] = s.resourcePromptGet
	s.dynTools["shared_file_read"] = s.resourceSharedFileRead
	s.dynTools["shared_file_write"] = s.resourceSharedFileWrite
	s.dynTools["workspace_create_run"] = s.resourceWorkspaceCreateRun
	s.dynTools["workspace_get_run"] = s.resourceWorkspaceGetRun
	s.dynTools["workspace_list_runs"] = s.resourceWorkspaceListRuns
	s.dynTools["workspace_merge_run"] = s.resourceWorkspaceMergeRun
	s.dynTools["workspace_abort_run"] = s.resourceWorkspaceAbortRun
}

// InvokeMethod 内部调用 JSON-RPC 方法 (不经过 WebSocket)。
//
// 用于 Wails UI 等 Go 进程内客户端直接调用后端功能。
// 复用 dispatchRequest 统一分发逻辑 (DRY)。
func (s *Server) InvokeMethod(ctx context.Context, method string, params json.RawMessage) (any, error) {
	resp := s.dispatchRequest(ctx, 1, method, params)
	if resp == nil {
		return nil, nil
	}
	if resp.Error != nil {
		return nil, pkgerr.Newf("Server.InvokeMethod", "%s (code %d)", resp.Error.Message, resp.Error.Code)
	}
	return resp.Result, nil
}

// checkLocalOrigin 仅允许 localhost 来源的 WebSocket 连接。
//
// 接受: 无 Origin header (本地工具), localhost, 127.0.0.1, [::1]。
func checkLocalOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true // 无 Origin = 非浏览器客户端 (CLI/IDE)
	}
	origin = strings.ToLower(origin)
	for _, allowed := range []string{
		"http://localhost", "https://localhost",
		"http://127.0.0.1", "https://127.0.0.1",
		"http://[::1]", "https://[::1]",
		"wails://", // Wails 桌面应用 WebKit
	} {
		if strings.HasPrefix(origin, allowed) {
			return true
		}
	}
	logger.Warn("app-server: rejected non-local origin", logger.FieldOrigin, origin)
	return false
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
		Addr:        host,
		Handler:     recoveryMiddleware(corsMiddleware(mux)),
		BaseContext: func(_ net.Listener) context.Context { return ctx },
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

// SetNotifyHook 设置 Notify 事件钩子。
//
// 用于桌面端桥接: apiserver 事件 -> Wails runtime event。
func (s *Server) SetNotifyHook(h func(method string, params any)) {
	s.notifyHookMu.Lock()
	s.notifyHook = h
	s.notifyHookMu.Unlock()
}

// Notify 向所有连接广播 JSON-RPC 通知 (WebSocket + SSE)。
func (s *Server) Notify(method string, params any) {
	s.syncUIRuntimeFromNotify(method, params)
	payload := util.ToMapAny(params)
	s.broadcastNotification(method, payload)

	if shouldEmitUIStateChanged(method, payload) {
		statePayload := map[string]any{"source": method}
		if tid, _ := payload["threadId"].(string); tid != "" {
			statePayload["threadId"] = tid
		}
		if aid, _ := payload["agent_id"].(string); aid != "" {
			statePayload["agent_id"] = aid
		}
		s.throttledUIStateChanged(statePayload)
	}
}

func shouldEmitUIStateChanged(method string, payload map[string]any) bool {
	if method == "" || method == "ui/state/changed" {
		return false
	}
	if strings.HasPrefix(method, "workspace/run/") {
		return true
	}
	threadID, _ := payload["threadId"].(string)
	if strings.TrimSpace(threadID) != "" {
		return true
	}
	agentID, _ := payload["agent_id"].(string)
	if strings.TrimSpace(agentID) != "" {
		return true
	}
	return false
}

// throttledUIStateChanged 全局节流发送 ui/state/changed。
//
// 使用全局统一节流 (不再 per-thread): 多 agent 并行时也只发一条,
// 前端只需要一个信号触发 syncRuntimeState 拉取最新快照。
func (s *Server) throttledUIStateChanged(payload map[string]any) {
	key := "_global"

	now := time.Now()
	interval := time.Duration(uiStateThrottleMs) * time.Millisecond

	s.uiThrottleMu.Lock()
	if s.uiThrottleEntries == nil {
		s.uiThrottleEntries = make(map[string]*uiStateThrottleEntry)
	}
	entry, ok := s.uiThrottleEntries[key]
	if !ok {
		entry = &uiStateThrottleEntry{}
		s.uiThrottleEntries[key] = entry
	}

	// 保存最新 payload (合并/覆盖)
	entry.pending = payload

	// 节流窗口内: 只安排 trailing timer
	if now.Sub(entry.lastEmit) < interval {
		if entry.timer == nil {
			entry.timer = time.AfterFunc(interval, func() {
				s.flushUIStateChanged(key)
			})
		}
		s.uiThrottleMu.Unlock()
		return
	}

	// 节流窗口外: 立即发送
	entry.lastEmit = now
	pending := entry.pending
	entry.pending = nil
	// 取消 trailing timer (刚发了, 不需要了)
	if entry.timer != nil {
		entry.timer.Stop()
		entry.timer = nil
	}
	s.uiThrottleMu.Unlock()

	s.broadcastNotification("ui/state/changed", pending)
}

// flushUIStateChanged trailing timer 回调: 发送最后一个 pending payload。
func (s *Server) flushUIStateChanged(key string) {
	s.uiThrottleMu.Lock()
	entry, ok := s.uiThrottleEntries[key]
	if !ok || entry.pending == nil {
		if ok {
			entry.timer = nil
		}
		s.uiThrottleMu.Unlock()
		return
	}
	entry.lastEmit = time.Now()
	pending := entry.pending
	entry.pending = nil
	entry.timer = nil
	s.uiThrottleMu.Unlock()

	s.broadcastNotification("ui/state/changed", pending)
}

func (s *Server) broadcastNotification(method string, params any) {
	s.notifyHookMu.RLock()
	hook := s.notifyHook
	s.notifyHookMu.RUnlock()
	if hook != nil {
		hook(method, params)
	}

	notif := newNotification(method, params)
	data, err := json.Marshal(notif)
	if err != nil {
		logger.Error("app-server: marshal notification failed", logger.FieldMethod, method, logger.FieldError, err)
		return
	}

	// SSE 广播 — 将事件推给浏览器调试客户端
	s.sseMu.RLock()
	sseCount := len(s.sseClients)
	if sseCount > 0 {
		logger.Debug("sse: broadcasting", logger.FieldMethod, method, "clients", sseCount, logger.FieldDataLen, len(data))
		for ch := range s.sseClients {
			select {
			case ch <- data:
			default:
				// 客户端跟不上, 丢弃 (非关键)
				logger.Warn("sse: client channel full, dropping event")
			}
		}
	}
	s.sseMu.RUnlock()

	s.mu.RLock()
	snapshot := make(map[string]*connEntry, len(s.conns))
	for id, entry := range s.conns {
		snapshot[id] = entry
	}
	s.mu.RUnlock()
	for id, entry := range snapshot {
		s.enqueueConnMessage(id, entry, websocket.TextMessage, data, "notify_backpressure")
	}
}

func (s *Server) enqueueConnMessage(connID string, entry *connEntry, msgType int, data []byte, reason string) bool {
	if entry == nil {
		return false
	}
	if entry.enqueue(msgType, data) {
		return true
	}
	logger.Warn("app-server: client send queue overloaded, disconnecting",
		logger.FieldConn, connID,
		"reason", strings.TrimSpace(reason),
		"outbox_depth", entry.outboxDepth(),
		"outbox_cap", connOutboxSize,
	)
	s.disconnectConn(connID)
	return false
}

func (s *Server) disconnectConn(connID string) {
	id := strings.TrimSpace(connID)
	if id == "" {
		return
	}
	s.mu.Lock()
	entry, ok := s.conns[id]
	if ok {
		delete(s.conns, id)
	}
	s.mu.Unlock()
	if ok && entry != nil {
		entry.closeNow()
	}
}

func (s *Server) syncUIRuntimeFromNotify(method string, params any) {
	if s.uiRuntime == nil {
		return
	}
	payload := util.ToMapAny(params)
	switch method {
	case "workspace/run/created", "workspace/run/aborted":
		run := util.ToMapAny(payload["run"])
		if len(run) == 0 {
			return
		}
		s.uiRuntime.UpsertWorkspaceRun(run)
	case "workspace/run/merged":
		runKey, _ := payload["runKey"].(string)
		result := util.ToMapAny(payload["result"])
		if len(result) == 0 {
			return
		}
		s.uiRuntime.ApplyWorkspaceMergeResult(runKey, result)
	}
	if shouldReplayThreadNotifyToUIRuntime(method, payload) {
		threadID, _ := payload["threadId"].(string)
		normalized := uistate.NormalizeEventFromPayload(method, method, payload)
		s.uiRuntime.ApplyAgentEvent(strings.TrimSpace(threadID), normalized, payload)
	}
}

func shouldReplayThreadNotifyToUIRuntime(method string, payload map[string]any) bool {
	if payload == nil {
		return false
	}
	if _, hasUIType := payload["uiType"]; hasUIType {
		return false
	}
	threadID, _ := payload["threadId"].(string)
	if strings.TrimSpace(threadID) == "" {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(method)) {
	case "turn/completed", "turn/aborted", "error", "codex/event/stream_error":
		return true
	default:
		return false
	}
}

// SendRequest 向指定连接发送 Server→Client 请求并等待响应 (§ 二)。
//
// 用于 approval 流程: requestApproval → client 审批 → 返回结果。
// 超时 5 分钟 (用户审批需要时间)。
func (s *Server) SendRequest(connID, method string, params any) (*Response, error) {
	reqID := s.nextReqID.Add(1)

	req := Request{
		JSONRPC: jsonrpcVersion,
		ID:      reqID,
		Method:  method,
	}
	if params != nil {
		raw, err := json.Marshal(params)
		if err != nil {
			return nil, pkgerr.Wrap(err, "Server.SendRequest", "marshal params")
		}
		req.Params = raw
	}

	data, err := json.Marshal(req)
	if err != nil {
		return nil, pkgerr.Wrap(err, "Server.SendRequest", "marshal request")
	}

	// 创建 response channel
	ch := make(chan *Response, 1)
	s.pendingMu.Lock()
	s.pending[reqID] = ch
	s.pendingMu.Unlock()

	defer func() {
		s.pendingMu.Lock()
		delete(s.pending, reqID)
		s.pendingMu.Unlock()
	}()

	// 发送到指定连接
	s.mu.RLock()
	entry, ok := s.conns[connID]
	s.mu.RUnlock()
	if !ok {
		return nil, pkgerr.Newf("Server.SendRequest", "connection %s not found", connID)
	}

	if !s.enqueueConnMessage(connID, entry, websocket.TextMessage, data, "server_request_backpressure") {
		return nil, pkgerr.Newf("Server.SendRequest", "connection %s overloaded; retry later", connID)
	}

	logger.Info("app-server: sent request to client", logger.FieldConn, connID, logger.FieldMethod, method, logger.FieldID, reqID)

	// 等待客户端响应 (5 分钟超时)
	timer := time.NewTimer(5 * time.Minute)
	defer timer.Stop()
	select {
	case resp := <-ch:
		return resp, nil
	case <-timer.C:
		return nil, pkgerr.Newf("Server.SendRequest", "request %d timed out waiting for client response", reqID)
	}
}

// SendRequestToAll 向所有连接广播 Server→Client 请求, 返回第一个响应。
//
// 适用于只有一个 IDE 连接的场景。
func (s *Server) SendRequestToAll(method string, params any) (*Response, error) {
	s.mu.RLock()
	var firstConn string
	for id := range s.conns {
		firstConn = id
		break
	}
	s.mu.RUnlock()

	if firstConn == "" {
		return nil, pkgerr.New("Server.SendRequestToAll", "no connected clients")
	}
	return s.SendRequest(firstConn, method, params)
}

// handleUpgrade HTTP → WebSocket 升级。
