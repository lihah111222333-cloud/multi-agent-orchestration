// client.go — Codex HTTP API 客户端 (纯 REST, 无 WebSocket/SSE)。
//
// NOTE: 此客户端仅用于 bus/router 跨 Agent HTTP 通信。
// 主要通信路径已迁移至 client_appserver.go (JSON-RPC 2.0)。
//
// 生命周期: Spawn → Health → CreateThread → Submit → Shutdown
package codex

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	apperrors "github.com/multi-agent/go-agent-v2/pkg/errors"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
)

// TransportMode 传输模式。
type TransportMode string

const (
	// TransportWS WebSocket 全双工 (已弃用, 仅保留常量兼容)。
	TransportWS TransportMode = "ws"
	// TransportSSE POST+SSE 降级 (已弃用, 仅保留常量兼容)。
	TransportSSE TransportMode = "sse"
)

// EventHandler 事件回调。
type EventHandler func(event Event)

// Client Codex HTTP API 客户端 (纯 REST, 无 socket)。
//
// NOTE: WebSocket/SSE 传输已注释掉。
// 新代码应使用 AppServerClient (JSON-RPC 2.0)。
type Client struct {
	Port      int
	Cmd       *exec.Cmd
	ThreadID  string
	AgentID   string        // 所属 Agent 标识, 用于日志关联
	Transport TransportMode // 保留字段兼容, 不再使用

	baseURL         string
	handler         EventHandler
	handlerMu       sync.RWMutex
	stopped         atomic.Bool
	ctx             context.Context
	cancel          context.CancelFunc
	httpCli         *http.Client
	stderrCollector *logger.StderrCollector
}

// NewClient 创建客户端 (不启动进程)。
func NewClient(port int, agentID string) *Client {
	ctx, cancel := context.WithCancel(context.Background())
	return &Client{
		Port:    port,
		AgentID: agentID,
		baseURL: fmt.Sprintf("http://127.0.0.1:%d", port),
		ctx:     ctx,
		cancel:  cancel,
		httpCli: &http.Client{Timeout: 10 * time.Second},
	}
}

// SetEventHandler 注册事件回调 (线程安全)。
func (c *Client) SetEventHandler(h EventHandler) {
	c.handlerMu.Lock()
	c.handler = h
	c.handlerMu.Unlock()
}

// GetPort 返回端口号。
func (c *Client) GetPort() int { return c.Port }

// GetThreadID 返回当前 thread ID。
func (c *Client) GetThreadID() string { return c.ThreadID }

// ========================================
// 进程管理
// ========================================

// Spawn 启动 codex http-api 子进程并等待健康检查通过。
//
// 支持两种模式:
//   - port > 0: 先检查端口空闲 → 启动 → health check → 查 agent_threads 获取信息
//   - port = 0: 自动分配端口 → 从 stdout/扫描发现实际端口
func (c *Client) Spawn(ctx context.Context) error {
	// 指定端口时先检查是否空闲
	if c.Port > 0 {
		if err := checkPortFree(c.Port); err != nil {
			return apperrors.Wrapf(err, "Client.Spawn", "port %d occupied", c.Port)
		}
	}

	portArg := strconv.Itoa(c.Port)
	c.Cmd = exec.CommandContext(ctx, "codex", "http-api", "--p1", portArg)
	c.Cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	c.Cmd.Env = os.Environ()

	// port 0: 捕获 stdout 以发现实际端口
	var stdoutBuf bytes.Buffer
	if c.Port == 0 {
		c.Cmd.Stdout = &stdoutBuf
	} else {
		c.Cmd.Stdout = io.Discard
	}
	c.stderrCollector = logger.NewStderrCollector(fmt.Sprintf("codex-http-%d", c.Port))
	c.Cmd.Stderr = c.stderrCollector

	if err := c.Cmd.Start(); err != nil {
		return apperrors.Wrap(err, "Client.Spawn", "spawn codex http-api")
	}

	logger.Infow("codex: process spawned",
		logger.FieldAgentID, c.AgentID,
		logger.FieldPort, c.Port,
		logger.FieldPID, c.Cmd.Process.Pid,
	)

	// 等待 health check (最多 15 秒, 指数退避)
	deadline := time.Now().Add(15 * time.Second)

	if c.Port > 0 {
		return c.waitForKnownPort(ctx, deadline)
	}

	// port 0: 先等 stdout, 再尝试解析端口, 再扫描
	return c.discoverPort(ctx, deadline, &stdoutBuf)
}

func (c *Client) waitForKnownPort(ctx context.Context, deadline time.Time) error {
	backoff := 100 * time.Millisecond
	for time.Now().Before(deadline) {
		if err := c.Health(); err == nil {
			logger.Infow("codex: health check passed", logger.FieldAgentID, c.AgentID, logger.FieldPort, c.Port)
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, 2*time.Second)
	}
	logger.Warn("codex: health check timeout", logger.FieldAgentID, c.AgentID, logger.FieldPort, c.Port)
	return apperrors.Newf("Client.Spawn", "health check timeout on port %d", c.Port)
}

func (c *Client) discoverPort(ctx context.Context, deadline time.Time, stdoutBuf *bytes.Buffer) error {
	backoff := 200 * time.Millisecond
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, 2*time.Second)

		// 策略 1: 从 stdout 解析端口 (codex 可能输出 "Listening on port XXXX")
		if port := parsePortFromOutput(stdoutBuf.String()); port > 0 {
			c.setPort(port)
			if err := c.Health(); err == nil {
				return nil
			}
		}

		// 策略 2: 扫描 4001-4099 端口找到匹配的 health 响应
		if port := c.scanForPort(ctx); port > 0 {
			c.setPort(port)
			return nil
		}
	}
	return apperrors.New("Client.Spawn", "port discovery timeout (port 0 mode)")
}

// setPort 更新实际端口。
func (c *Client) setPort(port int) {
	c.Port = port
	c.baseURL = fmt.Sprintf("http://127.0.0.1:%d", port)
}

// scanForPort 扫描端口范围找到 codex http-api 实例。
func (c *Client) scanForPort(ctx context.Context) int {
	pid := 0
	if c.Cmd != nil && c.Cmd.Process != nil {
		pid = c.Cmd.Process.Pid
	}

	for port := 4001; port <= 4099; port++ {
		url := fmt.Sprintf("http://127.0.0.1:%d/health", port)
		resp, err := c.httpCli.Get(url)
		if err != nil {
			continue
		}

		var hr HealthResponse
		err = json.NewDecoder(resp.Body).Decode(&hr)
		resp.Body.Close()
		if err != nil {
			continue
		}

		// 匹配 PID 确认是我们启动的进程
		if pid > 0 && hr.PID == pid {
			return port
		}
		// 无 PID 匹配时，接受第一个 healthy 响应
		if pid == 0 && hr.Status == "ok" {
			return port
		}
	}
	return 0
}

// parsePortFromOutput 从 stdout 输出解析端口号。
func parsePortFromOutput(output string) int {
	// 查找 "port XXXX" 或 ":XXXX" 模式
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// "Listening on port 4001" / "port: 4001" / ":4001"
		for _, prefix := range []string{"port ", "port: ", ":"} {
			idx := strings.LastIndex(strings.ToLower(line), prefix)
			if idx < 0 {
				continue
			}
			numStr := strings.TrimSpace(line[idx+len(prefix):])
			// 取第一个数字序列
			end := 0
			for end < len(numStr) && numStr[end] >= '0' && numStr[end] <= '9' {
				end++
			}
			if end > 0 {
				port, err := strconv.Atoi(numStr[:end])
				if err == nil && port > 0 && port < 65536 {
					return port
				}
			}
		}
	}
	return 0
}

// checkPortFree 检查端口是否空闲。
func checkPortFree(port int) error {
	l, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return err
	}
	_ = l.Close()
	return nil
}

// ========================================
// HTTP API
// ========================================

// Health GET /health。
func (c *Client) Health() error {
	resp, err := c.httpCli.Get(c.baseURL + "/health")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return apperrors.Newf("Client.Health", "health status %d", resp.StatusCode)
	}
	return nil
}

// CreateThread POST /threads — 创建线程。
func (c *Client) CreateThread(req CreateThreadRequest) (*CreateThreadResponse, error) {
	var result CreateThreadResponse
	if err := c.postJSON("/threads", req, &result, http.StatusCreated); err != nil {
		return nil, err
	}
	c.ThreadID = result.ThreadID
	return &result, nil
}

// ListThreads GET /threads。
func (c *Client) ListThreads() ([]ThreadInfo, error) {
	var threads []ThreadInfo
	return threads, c.getJSON("/threads", &threads)
}

// DeleteThread DELETE /threads/:id。
func (c *Client) DeleteThread(threadID string) error {
	return c.doRequest(http.MethodDelete, "/threads/"+threadID, http.StatusNoContent, http.StatusOK)
}

// ResumeThread 恢复已有会话 (对应 CLI: codex resume <id> [path])。
func (c *Client) ResumeThread(req ResumeThreadRequest) error {
	if req.ThreadID == "" {
		return apperrors.New("Client.ResumeThread", "resume requires thread_id")
	}
	if err := c.postJSON("/threads/"+req.ThreadID+"/resume", req, nil, http.StatusOK, http.StatusCreated); err != nil {
		return err
	}
	c.ThreadID = req.ThreadID
	return nil
}

// ForkThread 分叉会话 (对应 CLI: codex fork <id> [path])。
func (c *Client) ForkThread(req ForkThreadRequest) (*ForkThreadResponse, error) {
	if req.SourceThreadID == "" {
		return nil, apperrors.New("Client.ForkThread", "fork requires source_thread_id")
	}
	var result ForkThreadResponse
	if err := c.postJSON("/threads/"+req.SourceThreadID+"/fork", req, &result, http.StatusOK, http.StatusCreated); err != nil {
		return nil, err
	}
	c.ThreadID = result.ThreadID
	return &result, nil
}

// ========================================
// 通用 HTTP helpers (消除重复 marshal→post→check→decode)
// ========================================

// postJSON POST JSON 请求。out 为 nil 时不解析响应体。
func (c *Client) postJSON(path string, reqBody any, out any, okStatus ...int) error {
	data, err := json.Marshal(reqBody)
	if err != nil {
		return err
	}
	resp, err := c.httpCli.Post(c.baseURL+path, "application/json", bytes.NewReader(data))
	if err != nil {
		return apperrors.Wrapf(err, "Client.postJSON", "POST %s", path)
	}
	defer resp.Body.Close()
	if !statusOK(resp.StatusCode, okStatus) {
		body, _ := io.ReadAll(resp.Body)
		return apperrors.Newf("Client.postJSON", "POST %s status %d: %s", path, resp.StatusCode, body)
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}

// getJSON GET 请求并解析 JSON。
func (c *Client) getJSON(path string, out any) error {
	resp, err := c.httpCli.Get(c.baseURL + path)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return json.NewDecoder(resp.Body).Decode(out)
}

// doRequest 发送无 body 的 HTTP 请求 (DELETE 等)。
func (c *Client) doRequest(method, path string, okStatus ...int) error {
	req, _ := http.NewRequest(method, c.baseURL+path, nil)
	resp, err := c.httpCli.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if !statusOK(resp.StatusCode, okStatus) {
		return apperrors.Newf("Client.doRequest", "%s %s status %d", method, path, resp.StatusCode)
	}
	return nil
}

// statusOK 检查状态码是否在允许列表中。
func statusOK(code int, allowed []int) bool {
	for _, ok := range allowed {
		if code == ok {
			return true
		}
	}
	return false
}

// ========================================
// NOTE: WebSocket/SSE 传输已移除。
// 如需 WS/SSE 能力, 参见 git history 或使用 AppServerClient (JSON-RPC)。
// ========================================

// Submit 发送对话 (纯 HTTP POST)。
func (c *Client) Submit(prompt string, images, files []string, outputSchema json.RawMessage) error {
	if c.ThreadID == "" {
		return apperrors.New("Client.Submit", "no thread_id for submit")
	}

	reqBody := SubmitMessage{
		Type:   "submit",
		Prompt: prompt,
		Images: images,
		Files:  files,
	}
	data, err := json.Marshal(reqBody)
	if err != nil {
		return err
	}

	url := fmt.Sprintf("%s/threads/%s/submit", c.baseURL, c.ThreadID)
	resp, err := c.httpCli.Post(url, "application/json", bytes.NewReader(data))
	if err != nil {
		return apperrors.Wrap(err, "Client.Submit", "submit http")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return apperrors.Newf("Client.Submit", "submit status %d: %s", resp.StatusCode, body)
	}
	return nil
}

// SendCommand 发送斜杠命令 (纯 REST 客户端不支持, 需使用 AppServerClient)。
func (c *Client) SendCommand(cmd, args string) error {
	return apperrors.New("Client.SendCommand", "slash commands not supported in REST client, use AppServerClient")
}

// SendDynamicToolResult 回传动态工具结果 (纯 REST 客户端不支持, 需使用 AppServerClient)。
func (c *Client) SendDynamicToolResult(callID, output string, requestID *int64) error {
	return apperrors.New("Client.SendDynamicToolResult", "dynamic tool result not supported in REST client, use AppServerClient")
}

// RespondError 向 codex 发送 JSON-RPC 错误响应。
//
// 纯 REST 客户端无 JSON-RPC server request 通道，因此该方法仅用于接口兼容。
func (c *Client) RespondError(id int64, code int, message string) error {
	return apperrors.New("Client.RespondError", "server request response not supported in REST client, use AppServerClient")
}

// ========================================
// 完整启动流程
// ========================================

// SpawnAndConnect 一键启动: spawn 进程 → 创建线程 (纯 REST, 无 socket 连接)。
func (c *Client) SpawnAndConnect(ctx context.Context, prompt, cwd, model string, dynamicTools []DynamicTool) error {
	if err := c.Spawn(ctx); err != nil {
		return err
	}

	req := CreateThreadRequest{
		Prompt:       prompt,
		Cwd:          cwd,
		Model:        model,
		DynamicTools: dynamicTools,
	}
	if _, err := c.CreateThread(req); err != nil {
		logger.Error("codex: create thread failed", logger.FieldAgentID, c.AgentID, logger.FieldPort, c.Port, logger.FieldError, err)
		_ = c.Kill()
		return err
	}

	logger.Infow("codex: spawn and connect complete",
		logger.FieldAgentID, c.AgentID,
		logger.FieldPort, c.Port,
		logger.FieldThreadID, c.ThreadID,
	)
	return nil
}

// ========================================
// 关闭
// ========================================

// Shutdown 优雅关闭: kill 进程。
func (c *Client) Shutdown() error {
	if c.stopped.Swap(true) {
		return nil
	}
	logger.Infow("codex: shutting down", logger.FieldAgentID, c.AgentID, logger.FieldPort, c.Port)
	if c.stderrCollector != nil {
		_ = c.stderrCollector.Close()
	}
	c.cancel()
	return c.Kill()
}

// Kill 强制终止子进程。
func (c *Client) Kill() error {
	if c.Cmd != nil && c.Cmd.Process != nil {
		pid := c.Cmd.Process.Pid
		logger.Warn("codex: force killing process", logger.FieldAgentID, c.AgentID, logger.FieldPID, pid)
		// 尝试杀整个进程组, 回退到单进程 kill。
		if err := syscall.Kill(-pid, syscall.SIGKILL); err != nil {
			_ = c.Cmd.Process.Kill()
		}
		_ = c.Cmd.Wait()
	}
	return nil
}

// Running 返回客户端是否在运行。
func (c *Client) Running() bool {
	return !c.stopped.Load() && c.Cmd != nil && c.Cmd.Process != nil
}
