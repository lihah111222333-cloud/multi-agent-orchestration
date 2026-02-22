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

	apperrors "github.com/multi-agent/go-agent-v2/pkg/errors"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
	"github.com/multi-agent/go-agent-v2/pkg/util"
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

	// mu 保护 pending map 和 stdin 写入 (JSON-RPC 帧序列化)。
	// 也用于保护 onDiag callback 注册。
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
		return apperrors.Wrap(err, "LSP.Start", "stdin pipe")
	}
	stdoutPipe, err := c.cmd.StdoutPipe()
	if err != nil {
		return apperrors.Wrap(err, "LSP.Start", "stdout pipe")
	}
	c.stdout = bufio.NewReaderSize(stdoutPipe, 256*1024)
	c.stderr, err = c.cmd.StderrPipe()
	if err != nil {
		logger.Warn("lsp: stderr pipe failed", logger.FieldLanguage, c.language, logger.FieldError, err)
	}

	if err := c.cmd.Start(); err != nil {
		return apperrors.Wrapf(err, "LSP.Start", "start %s", command)
	}

	logger.Infow("lsp: process started",
		logger.FieldLanguage, c.language,
		logger.FieldCommand, command,
		logger.FieldPID, c.cmd.Process.Pid,
	)

	// 后台读取 server→client 消息
	util.SafeGo(func() { c.readLoop() })

	// 收集 stderr (LSP 服务器错误输出 → 统一日志)
	if c.stderr != nil {
		c.stderrCollector = logger.NewStderrCollector("lsp-" + c.language)
		util.SafeGo(func() { _, _ = io.Copy(c.stderrCollector, c.stderr) })
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
		return apperrors.Wrap(err, "LSP.Start", "initialize")
	}

	// initialized 通知 (无响应)
	if err := c.notify("initialized", struct{}{}); err != nil {
		_ = c.Stop()
		return apperrors.Wrap(err, "LSP.Start", "initialized notify")
	}

	logger.Infow("lsp: initialize handshake complete", logger.FieldLanguage, c.language)
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

// DidChange 通知服务器文档内容发生变化 (全量替换)。
func (c *Client) DidChange(uri string, version int, newText string) error {
	return c.notify("textDocument/didChange", DidChangeTextDocumentParams{
		TextDocument: VersionedTextDocumentIdentifier{
			URI:     uri,
			Version: version,
		},
		ContentChanges: []TextDocumentContentChangeEvent{
			{Text: newText},
		},
	})
}

// Hover 请求 hover 信息。
func (c *Client) Hover(ctx context.Context, uri string, line, character int) (*HoverResult, error) {
	if !c.Running() {
		return nil, fmt.Errorf("lsp client not running")
	}
	var result HoverResult
	err := c.call("textDocument/hover", TextDocumentPositionParams{
		TextDocument: TextDocumentIdentifier{URI: uri},
		Position:     Position{Line: line, Character: character},
	}, &result)
	if err != nil {
		return nil, err
	}
	if result.Contents.Value == "" {
		return nil, nil
	}
	return &result, nil
}

// Definition 跳转定义 — 返回符号的定义位置。
//
// LSP 规范: 返回值可能是 Location | Location[] | null。
// 此方法统一返回 []Location。
func (c *Client) Definition(ctx context.Context, uri string, line, character int) ([]Location, error) {
	if !c.Running() {
		return nil, fmt.Errorf("lsp client not running")
	}
	var raw json.RawMessage
	err := c.call("textDocument/definition", DefinitionParams{
		TextDocument: TextDocumentIdentifier{URI: uri},
		Position:     Position{Line: line, Character: character},
	}, &raw)
	if err != nil {
		return nil, err
	}
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	// 尝试解析为 []Location
	var locs []Location
	if err := json.Unmarshal(raw, &locs); err == nil {
		return locs, nil
	}
	// 回退: 解析为单个 Location
	var loc Location
	if err := json.Unmarshal(raw, &loc); err == nil {
		return []Location{loc}, nil
	}
	return nil, nil
}

// References 查找引用 — 返回符号的所有引用位置。
func (c *Client) References(ctx context.Context, uri string, line, character int, includeDecl bool) ([]Location, error) {
	if !c.Running() {
		return nil, fmt.Errorf("lsp client not running")
	}
	var result []Location
	err := c.call("textDocument/references", ReferenceParams{
		TextDocument: TextDocumentIdentifier{URI: uri},
		Position:     Position{Line: line, Character: character},
		Context:      ReferenceContext{IncludeDeclaration: includeDecl},
	}, &result)
	if err != nil {
		return nil, err
	}
	return result, nil
}

// DocumentSymbol 文件大纲 — 返回文件中所有符号的层次结构。
func (c *Client) DocumentSymbol(ctx context.Context, uri string) ([]DocumentSymbol, error) {
	if !c.Running() {
		return nil, fmt.Errorf("lsp client not running")
	}
	var result []DocumentSymbol
	err := c.call("textDocument/documentSymbol", DocumentSymbolParams{
		TextDocument: TextDocumentIdentifier{URI: uri},
	}, &result)
	if err != nil {
		return nil, err
	}
	return result, nil
}

// Completion 代码补全 — 返回补全列表。
//
// LSP 规范: 返回值可能是 CompletionItem[] | CompletionList | null。
func (c *Client) Completion(ctx context.Context, uri string, line, character int) ([]CompletionItem, error) {
	if !c.Running() {
		return nil, fmt.Errorf("lsp client not running")
	}
	var raw json.RawMessage
	err := c.call("textDocument/completion", CompletionParams{
		TextDocument: TextDocumentIdentifier{URI: uri},
		Position:     Position{Line: line, Character: character},
	}, &raw)
	if err != nil {
		return nil, err
	}
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}

	var list CompletionList
	if err := json.Unmarshal(raw, &list); err == nil {
		return list.Items, nil
	}

	var items []CompletionItem
	if err := json.Unmarshal(raw, &items); err == nil {
		return items, nil
	}
	return nil, nil
}

// Rename 重命名 — 返回重命名所需的全部编辑。
func (c *Client) Rename(ctx context.Context, uri string, line, character int, newName string) (*WorkspaceEdit, error) {
	if !c.Running() {
		return nil, fmt.Errorf("lsp client not running")
	}
	var result WorkspaceEdit
	err := c.call("textDocument/rename", RenameParams{
		TextDocument: TextDocumentIdentifier{URI: uri},
		Position:     Position{Line: line, Character: character},
		NewName:      newName,
	}, &result)
	if err != nil {
		return nil, err
	}
	return &result, nil
}

// Stop 优雅关闭: shutdown → exit → wait。
func (c *Client) Stop() error {
	if c.stopped.Load() {
		return nil // 已停止
	}

	logger.Infow("lsp: stopping", logger.FieldLanguage, c.language)

	if c.stderrCollector != nil {
		_ = c.stderrCollector.Close()
	}

	// 在标记 stopped 之前尝试优雅关闭，
	// 让 readLoop 还能读取 shutdown 响应，避免固定 30s 超时。
	if c.Running() {
		_ = c.callWithTimeout("shutdown", nil, nil, 2*time.Second)
		_ = c.notify("exit", nil)
	}

	if c.stopped.Swap(true) {
		return nil
	}

	if c.stdin != nil {
		_ = c.stdin.Close()
	}

	if c.cmd != nil && c.cmd.Process != nil {
		waitDone := make(chan struct{})
		go func() {
			_ = c.cmd.Wait()
			close(waitDone)
		}()

		select {
		case <-waitDone:
		case <-time.After(2 * time.Second):
			_ = c.cmd.Process.Kill()
			<-waitDone
		}
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
	return c.callWithTimeout(method, params, result, 30*time.Second)
}

func (c *Client) callWithTimeout(method string, params any, result any, timeout time.Duration) error {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}

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

	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case resp, ok := <-ch:
		if !ok {
			return apperrors.Newf("LSP.call", "connection closed while waiting for %s", method)
		}
		if resp.Error != nil {
			return apperrors.Newf("LSP.call", "%s error %d: %s", method, resp.Error.Code, resp.Error.Message)
		}
		if result != nil && resp.Result != nil {
			return json.Unmarshal(resp.Result, result)
		}
		return nil
	case <-timer.C:
		logger.Warn("lsp: call timeout", logger.FieldMethod, method, logger.FieldLanguage, c.language)
		return apperrors.Newf("LSP.call", "%s timeout (%s)", method, timeout)
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
	defer func() {
		// readLoop 退出后不再有任何响应，关闭 pending 以立即唤醒等待中的请求。
		c.mu.Lock()
		for id, ch := range c.pending {
			close(ch)
			delete(c.pending, id)
		}
		c.mu.Unlock()
	}()

	for !c.stopped.Load() {
		data, err := c.readFrame()
		if err != nil {
			if !c.stopped.Load() {
				logger.Warn("lsp: readLoop error",
					logger.FieldLanguage, c.language,
					logger.FieldError, err,
				)
			}
			return
		}

		// 判断是响应还是通知
		var peek struct {
			ID     *int   `json:"id"`
			Method string `json:"method"`
		}
		if err := json.Unmarshal(data, &peek); err != nil {
			logger.Debug("lsp: unmarshal peek failed", logger.FieldLanguage, c.language, logger.FieldError, err)
			continue
		}

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
			var atoiErr error
			contentLen, atoiErr = strconv.Atoi(numStr)
			if atoiErr != nil {
				logger.Debug("lsp: invalid Content-Length", logger.FieldRaw, numStr, logger.FieldError, atoiErr)
			}
		}
	}
	if contentLen <= 0 {
		return nil, apperrors.Newf("LSP.readFrame", "invalid Content-Length: %d", contentLen)
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
			logger.Warn("lsp: unmarshal diagnostics failed",
				logger.FieldLanguage, c.language,
				logger.FieldError, err,
			)
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
