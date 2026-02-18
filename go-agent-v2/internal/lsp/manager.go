// manager.go — 多语言 LSP 服务管理器。
//
// 按文件后缀自动选择语言服务器:
//   - .go           → gopls
//   - .rs           → rust-analyzer
//   - .ts/.tsx/.js/.jsx → typescript-language-server --stdio
//   - .py           → pylsp
//   - .c/.h         → clangd
//
// 延迟启动: 首次打开某语言文件时才 spawn 对应进程。
package lsp

import (
	"context"
	"net/url"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	apperrors "github.com/multi-agent/go-agent-v2/pkg/errors"
)

// ServerConfig 语言服务器配置。
type ServerConfig struct {
	Language   string   // 语言标识 ("go", "rust", "typescript")
	Command    string   // 可执行文件名
	Args       []string // 命令参数
	Extensions []string // 关联的文件后缀 (不含点号)
}

// DefaultServers 默认支持的五个语言服务器。
var DefaultServers = []ServerConfig{
	{
		Language:   "go",
		Command:    "gopls",
		Args:       nil,
		Extensions: []string{"go"},
	},
	{
		Language:   "rust",
		Command:    "rust-analyzer",
		Args:       nil,
		Extensions: []string{"rs"},
	},
	{
		Language:   "typescript",
		Command:    "typescript-language-server",
		Args:       []string{"--stdio"},
		Extensions: []string{"ts", "tsx", "js", "jsx"},
	},
	{
		Language:   "python",
		Command:    "pylsp",
		Args:       nil,
		Extensions: []string{"py"},
	},
	{
		Language:   "c",
		Command:    "clangd",
		Args:       nil,
		Extensions: []string{"c", "h"},
	},
}

// ServerStatus 服务器运行状态。
type ServerStatus struct {
	Language  string // 语言标识
	Command   string // 命令
	Available bool   // PATH 上是否可用
	Running   bool   // 是否正在运行
}

// Manager 管理多个语言的 LSP 客户端。
type Manager struct {
	// ========================================
	// 锁层次 (Lock Hierarchy)
	// ========================================
	// Manager.mu < Client.mu
	// Manager.mu 保护 configs/clients map, Client.mu 保护 pending/stdin。
	// ensureClient 中: 持有 Manager.mu → 释放 → 调用 client.Start
	// (client.Start 内部使用 Client.mu), 不嵌套。
	// ========================================

	mu       sync.RWMutex
	configs  map[string]*ServerConfig // ext → config
	clients  map[string]*Client       // language → client
	rootURI  string
	ctx      context.Context
	cancel   context.CancelFunc
	onDiag   DiagnosticHandler
	onStatus func(statuses []ServerStatus) // 状态变更回调
}

// NewManager 创建管理器。configs 为 nil 时使用 DefaultServers。
func NewManager(configs []ServerConfig) *Manager {
	if len(configs) == 0 {
		configs = DefaultServers
	}
	ctx, cancel := context.WithCancel(context.Background())
	m := &Manager{
		configs: make(map[string]*ServerConfig, len(configs)*3),
		clients: make(map[string]*Client),
		ctx:     ctx,
		cancel:  cancel,
	}
	for i := range configs {
		cfg := &configs[i]
		for _, ext := range cfg.Extensions {
			m.configs[ext] = cfg
		}
	}
	return m
}

// SetRootURI 设置项目根目录 (file:// URI)。
func (m *Manager) SetRootURI(rootURI string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rootURI = rootURI
}

// SetDiagnosticHandler 注册诊断回调 (所有语言共享)。
func (m *Manager) SetDiagnosticHandler(h DiagnosticHandler) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onDiag = h
}

// SetStatusHandler 注册状态变更回调。
func (m *Manager) SetStatusHandler(h func([]ServerStatus)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onStatus = h
}

// OpenFile 打开文件 — 自动选择语言服务器并发送 didOpen。
func (m *Manager) OpenFile(filePath, content string) error {
	ext := strings.TrimPrefix(filepath.Ext(filePath), ".")
	if ext == "" {
		return nil
	}

	m.mu.RLock()
	cfg, ok := m.configs[ext]
	m.mu.RUnlock()
	if !ok {
		return nil // 不支持的语言，静默忽略
	}

	client, err := m.ensureClient(cfg)
	if err != nil {
		return err
	}

	uri := pathToURI(filePath)
	return client.DidOpen(uri, cfg.Language, content)
}

// CloseFile 关闭文件。
func (m *Manager) CloseFile(filePath string) error {
	ext := strings.TrimPrefix(filepath.Ext(filePath), ".")
	m.mu.RLock()
	cfg, ok := m.configs[ext]
	if !ok {
		m.mu.RUnlock()
		return nil
	}
	client, exists := m.clients[cfg.Language]
	m.mu.RUnlock()
	if !exists || !client.Running() {
		return nil
	}
	return client.DidClose(pathToURI(filePath))
}

// Hover 获取 hover 信息。
func (m *Manager) Hover(filePath string, line, character int) (*HoverResult, error) {
	ext := strings.TrimPrefix(filepath.Ext(filePath), ".")
	m.mu.RLock()
	cfg, ok := m.configs[ext]
	if !ok {
		m.mu.RUnlock()
		return nil, nil
	}
	client, exists := m.clients[cfg.Language]
	m.mu.RUnlock()
	if !exists || !client.Running() {
		return nil, nil
	}
	return client.Hover(m.ctx, pathToURI(filePath), line, character)
}

// Statuses 返回所有配置的语言服务器状态。
func (m *Manager) Statuses() []ServerStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()

	seen := map[string]bool{}
	var result []ServerStatus
	for _, cfg := range m.configs {
		if seen[cfg.Language] {
			continue
		}
		seen[cfg.Language] = true

		_, available := exec.LookPath(cfg.Command)
		client, running := m.clients[cfg.Language]
		isRunning := running && client.Running()

		result = append(result, ServerStatus{
			Language:  cfg.Language,
			Command:   cfg.Command,
			Available: available == nil,
			Running:   isRunning,
		})
	}
	return result
}

// StopAll 关闭所有运行中的语言服务器。
func (m *Manager) StopAll() {
	m.cancel()
	m.mu.Lock()
	defer m.mu.Unlock()

	for lang, client := range m.clients {
		_ = client.Stop()
		delete(m.clients, lang)
	}
}

// Reload 重载所有语言服务器 (先关闭, 下次使用时自动重启)。
func (m *Manager) Reload() {
	m.cancel()
	m.mu.Lock()
	for lang, client := range m.clients {
		_ = client.Stop()
		delete(m.clients, lang)
	}
	// 重新创建 context — 让 ensureClient 可以再次启动
	m.ctx, m.cancel = context.WithCancel(context.Background())
	handler := m.onStatus
	m.mu.Unlock()

	if handler != nil {
		handler(m.Statuses())
	}
}

// ensureClient 确保指定语言的客户端已启动 (延迟启动)。
func (m *Manager) ensureClient(cfg *ServerConfig) (*Client, error) {
	m.mu.RLock()
	client, ok := m.clients[cfg.Language]
	m.mu.RUnlock()
	if ok && client.Running() {
		return client, nil
	}

	// 检查命令是否可用
	cmdPath, err := exec.LookPath(cfg.Command)
	if err != nil {
		return nil, apperrors.Newf("LSP.ensureClient", "%s not found in PATH", cfg.Command)
	}

	m.mu.Lock()
	// double check
	if client, ok = m.clients[cfg.Language]; ok && client.Running() {
		m.mu.Unlock()
		return client, nil
	}

	client = NewClient(cfg.Language)
	m.clients[cfg.Language] = client

	// 传递诊断回调
	if m.onDiag != nil {
		client.SetDiagnosticHandler(m.onDiag)
	}

	rootURI := m.rootURI
	m.mu.Unlock()

	// Start 可能阻塞 (等待 initialize 响应)，不持锁
	if err := client.Start(m.ctx, cmdPath, cfg.Args, rootURI); err != nil {
		m.mu.Lock()
		delete(m.clients, cfg.Language)
		m.mu.Unlock()
		return nil, err
	}

	// 通知状态变更
	m.mu.RLock()
	handler := m.onStatus
	m.mu.RUnlock()
	if handler != nil {
		handler(m.Statuses())
	}

	return client, nil
}

// pathToURI 将文件路径转为 file:// URI。
func pathToURI(path string) string {
	if strings.HasPrefix(path, "file://") {
		return path
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	abs = filepath.ToSlash(abs)
	if !strings.HasPrefix(abs, "/") {
		abs = "/" + abs
	}
	return (&url.URL{Scheme: "file", Path: abs}).String()
}
