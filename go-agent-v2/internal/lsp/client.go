// client.go — 零依赖 LSP JSON-RPC 2.0 over stdio 客户端。
//
// 协议: Content-Length: N\r\n\r\n{json} — 标准 LSP Base Protocol。
// 生命周期: Start → initialize → initialized → (didOpen/hover/...) → shutdown → exit。
package lsp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/multi-agent/go-agent-v2/pkg/logger"
)

// DiagnosticHandler 诊断回调。
type DiagnosticHandler func(uri string, diagnostics []Diagnostic)

// Client 是一个 LSP 语言服务器的 JSON-RPC 2.0 客户端。
// 通过 stdio (stdin/stdout) 与子进程通信。
type Client struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader
	stderr io.ReadCloser

	stderrCollector *logger.StderrCollector

	mu       sync.Mutex
	nextID   atomic.Int64
	pending  map[int]chan *Response
	onDiag   DiagnosticHandler
	stopped  atomic.Bool
	language string
}

// NewClient 创建客户端 (不启动进程)。
func NewClient(language string) *Client {
	return &Client{
		language: language,
		pending:  make(map[int]chan *Response),
	}
}

// Language 返回此客户端服务的语言标识。
func (c *Client) Language() string { return c.language }

// SetDiagnosticHandler 注册诊断回调。
func (c *Client) SetDiagnosticHandler(h DiagnosticHandler) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.onDiag = h
}

// Start 启动语言服务器进程并完成 initialize 握手。
func (c *Client) Start(ctx context.Context, command string, args []string, rootURI string) error {
	c.cmd = exec.CommandContext(ctx, command, args...)
	c.cmd.Env = os.Environ()

	var err error
	c.stdin, err = c.cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("lsp: stdin pipe: %w", err)
	}
	stdoutPipe, err := c.cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("lsp: stdout pipe: %w", err)
	}
	c.stdout = bufio.NewReaderSize(stdoutPipe, 256*1024)
	c.stderr, _ = c.cmd.StderrPipe()

	if err := c.cmd.Start(); err != nil {
		return fmt.Errorf("lsp: start %s: %w", command, err)
	}

	// 后台读取 server→client 消息
	go c.readLoop()

	// 收集 stderr (LSP 服务器错误输出 → 统一日志)
	if c.stderr != nil {
		c.stderrCollector = logger.NewStderrCollector("lsp-" + c.language)
		go func() { _, _ = io.Copy(c.stderrCollector, c.stderr) }()
	}

	// initialize 握手
	initParams := InitializeParams{
		ProcessID: os.Getpid(),
		RootURI:   rootURI,
		Capabilities: ClientCapabilities{
			TextDocument: &TextDocumentClientCapabilities{
				PublishDiagnostics: &PublishDiagnosticsCapability{
					RelatedInformation: true,
				},
				Hover: &HoverCapability{
					ContentFormat: []string{"markdown", "plaintext"},
				},
			},
		},
	}

	var initResult InitializeResult
	if err := c.call("initialize", initParams, &initResult); err != nil {
		_ = c.Stop()
		return fmt.Errorf("lsp: initialize: %w", err)
	}

	// initialized 通知 (无响应)
	if err := c.notify("initialized", struct{}{}); err != nil {
		_ = c.Stop()
		return fmt.Errorf("lsp: initialized notify: %w", err)
	}

	return nil
}

// Running 返回客户端是否正在运行。
func (c *Client) Running() bool {
	return !c.stopped.Load() && c.cmd != nil && c.cmd.Process != nil
}

// DidOpen 通知服务器打开了文件。
func (c *Client) DidOpen(uri, languageID, text string) error {
	return c.notify("textDocument/didOpen", DidOpenTextDocumentParams{
		TextDocument: TextDocumentItem{
			URI:        uri,
			LanguageID: languageID,
			Version:    1,
			Text:       text,
		},
	})
}

// DidClose 通知服务器关闭了文件。
func (c *Client) DidClose(uri string) error {
	return c.notify("textDocument/didClose", DidCloseTextDocumentParams{
		TextDocument: TextDocumentIdentifier{URI: uri},
	})
}

// Hover 请求 hover 信息。
func (c *Client) Hover(ctx context.Context, uri string, line, character int) (*HoverResult, error) {
	params := HoverParams{
		TextDocument: TextDocumentIdentifier{URI: uri},
		Position:     Position{Line: line, Character: character},
	}
	var result HoverResult
	if err := c.call("textDocument/hover", params, &result); err != nil {
		return nil, err
	}
	if result.Contents.Value == "" {
		return nil, nil
	}
	return &result, nil
}

// Stop 优雅关闭: shutdown → exit → wait。
func (c *Client) Stop() error {
	if c.stopped.Swap(true) {
		return nil // 已停止
	}

	if c.stderrCollector != nil {
		_ = c.stderrCollector.Close()
	}

	// shutdown 请求
	_ = c.call("shutdown", nil, nil)

	// exit 通知
	_ = c.notify("exit", nil)

	if c.stdin != nil {
		_ = c.stdin.Close()
	}

	if c.cmd != nil && c.cmd.Process != nil {
		_ = c.cmd.Wait()
	}

	// 清理 pending
	c.mu.Lock()
	for id, ch := range c.pending {
		close(ch)
		delete(c.pending, id)
	}
	c.mu.Unlock()

	return nil
}

// ========================================
// JSON-RPC 传输层
// ========================================

// call 发送请求并等待响应 (30 秒超时防止 goroutine 泄漏)。
func (c *Client) call(method string, params any, result any) error {
	id := int(c.nextID.Add(1))
	ch := make(chan *Response, 1)

	c.mu.Lock()
	c.pending[id] = ch
	c.mu.Unlock()

	defer func() {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
	}()

	if err := c.writeRequest(id, method, params); err != nil {
		return err
	}

	select {
	case resp, ok := <-ch:
		if !ok {
			return fmt.Errorf("lsp: connection closed while waiting for %s", method)
		}
		if resp.Error != nil {
			return fmt.Errorf("lsp: %s error %d: %s", method, resp.Error.Code, resp.Error.Message)
		}
		if result != nil && resp.Result != nil {
			return json.Unmarshal(resp.Result, result)
		}
		return nil
	case <-time.After(30 * time.Second):
		return fmt.Errorf("lsp: %s timeout (30s)", method)
	}
}

// notify 发送通知 (无响应)。
func (c *Client) notify(method string, params any) error {
	msg := map[string]any{
		"jsonrpc": "2.0",
		"method":  method,
	}
	if params != nil {
		msg["params"] = params
	}
	return c.writeMessage(msg)
}

// writeRequest 编码并写入请求。
func (c *Client) writeRequest(id int, method string, params any) error {
	msg := map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  method,
	}
	if params != nil {
		msg["params"] = params
	}
	return c.writeMessage(msg)
}

// writeMessage 编码 JSON 并加 Content-Length 头写入 stdin。
func (c *Client) writeMessage(v any) error {
	body, err := json.Marshal(v)
	if err != nil {
		return err
	}
	header := fmt.Sprintf("Content-Length: %d\r\n\r\n", len(body))

	c.mu.Lock()
	defer c.mu.Unlock()

	if _, err := io.WriteString(c.stdin, header); err != nil {
		return err
	}
	_, err = c.stdin.Write(body)
	return err
}

// readLoop 持续读取 server→client 消息 (响应 + 通知)。
func (c *Client) readLoop() {
	for !c.stopped.Load() {
		data, err := c.readFrame()
		if err != nil {
			if !c.stopped.Load() {
				// 非主动关闭的读取错误
			}
			return
		}

		// 判断是响应还是通知
		var peek struct {
			ID     *int   `json:"id"`
			Method string `json:"method"`
		}
		_ = json.Unmarshal(data, &peek)

		if peek.ID != nil && peek.Method == "" {
			// 响应
			var resp Response
			if err := json.Unmarshal(data, &resp); err != nil {
				continue
			}
			c.mu.Lock()
			ch, ok := c.pending[*resp.ID]
			if ok {
				delete(c.pending, *resp.ID)
			}
			c.mu.Unlock()
			if ok {
				ch <- &resp
			}
		} else if peek.Method != "" {
			// 通知
			c.handleNotification(peek.Method, data)
		}
	}
}

// readFrame 读取一个 Content-Length 帧。
func (c *Client) readFrame() ([]byte, error) {
	contentLen := 0
	for {
		line, err := c.stdout.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimSpace(line)
		if line == "" {
			break // 空行标志头部结束
		}
		if strings.HasPrefix(line, "Content-Length:") {
			numStr := strings.TrimSpace(strings.TrimPrefix(line, "Content-Length:"))
			contentLen, _ = strconv.Atoi(numStr)
		}
	}
	if contentLen <= 0 {
		return nil, fmt.Errorf("lsp: invalid Content-Length: %d", contentLen)
	}

	body := make([]byte, contentLen)
	if _, err := io.ReadFull(c.stdout, body); err != nil {
		return nil, err
	}
	return body, nil
}

// handleNotification 处理 server→client 通知。
func (c *Client) handleNotification(method string, raw []byte) {
	switch method {
	case "textDocument/publishDiagnostics":
		var notif struct {
			Params PublishDiagnosticsParams `json:"params"`
		}
		if err := json.Unmarshal(raw, &notif); err != nil {
			return
		}
		c.mu.Lock()
		handler := c.onDiag
		c.mu.Unlock()
		if handler != nil {
			handler(notif.Params.URI, notif.Params.Diagnostics)
		}
	}
	// 忽略其他通知
}

// ========================================
// 导出的帧工具 (用于测试)
// ========================================

// EncodeFrame 编码一个 Content-Length 帧 (用于测试)。
func EncodeFrame(body []byte) []byte {
	header := fmt.Sprintf("Content-Length: %d\r\n\r\n", len(body))
	return append([]byte(header), body...)
}
