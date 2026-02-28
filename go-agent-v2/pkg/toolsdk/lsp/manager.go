package lsp

import (
	"context"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	apperrors "github.com/multi-agent/go-agent-v2/pkg/errors"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
)

type ServerConfig struct {
	Language   string
	Command    string
	Args       []string
	Extensions []string
}

var DefaultServers = []ServerConfig{
	{
		Language:   "go",
		Command:    "gopls",
		Extensions: []string{"go"},
	},
	{
		Language:   "rust",
		Command:    "rust-analyzer",
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
		Extensions: []string{"py"},
	},
	{
		Language:   "c",
		Command:    "clangd",
		Extensions: []string{"c", "h"},
	},
}

type ServerStatus struct {
	Language  string
	Command   string
	Available bool
	Running   bool
}

type Manager struct {
	mu          sync.RWMutex
	configs     map[string]*ServerConfig
	languages   map[string]*ServerConfig
	clients     map[string]*Client
	rootURI     string
	workspaceID string
	ctx         context.Context
	cancel      context.CancelFunc
	onDiag      DiagnosticHandler
	onStatus    func(statuses []ServerStatus) // 状态变更回调

	docMu     sync.Mutex
	docStates map[string]*documentSyncState
	docLocks  map[string]*sync.Mutex
	cache     *lspCacheStore
}

func NewManager(configs []ServerConfig) *Manager {
	if len(configs) == 0 {
		configs = DefaultServers
	}
	ctx, cancel := context.WithCancel(context.Background())
	workspaceID := "default-workspace"
	if cwd, err := os.Getwd(); err == nil {
		workspaceID = cwd
		if abs, err := filepath.Abs(cwd); err == nil {
			workspaceID = abs
		}
	}
	m := &Manager{
		configs:     make(map[string]*ServerConfig, len(configs)*3),
		languages:   make(map[string]*ServerConfig, len(configs)),
		clients:     make(map[string]*Client),
		docStates:   make(map[string]*documentSyncState),
		docLocks:    make(map[string]*sync.Mutex),
		cache:       newLSPCacheStoreFromEnv(),
		workspaceID: workspaceID,
		ctx:         ctx,
		cancel:      cancel,
	}
	for i := range configs {
		cfg := &configs[i]
		m.languages[normalizeLanguage(cfg.Language)] = cfg
		for _, ext := range cfg.Extensions {
			m.configs[ext] = cfg
		}
	}
	return m
}

func (m *Manager) SetRootURI(rootURI string) {
	m.mu.Lock()
	resolvedRootURI := effectiveRootURI(rootURI, m.workspaceID)
	if m.rootURI == resolvedRootURI {
		m.mu.Unlock()
		return
	}
	previousRootURI := m.rootURI
	m.rootURI = resolvedRootURI

	clientsToRestart := m.clients
	m.clients = make(map[string]*Client, len(clientsToRestart))
	handler := m.onStatus
	m.mu.Unlock()

	for language, client := range clientsToRestart {
		if client == nil {
			continue
		}
		if err := client.Stop(); err != nil {
			logger.Warn("lsp: failed to stop client after rootURI change",
				logger.FieldLanguage, language,
				logger.FieldError, err,
			)
		}
	}

	m.resetDocumentStates()

	if len(clientsToRestart) > 0 {
		logger.Warn("lsp: rootURI changed, clients will restart on next request",
			"old_root_uri", previousRootURI,
			"new_root_uri", resolvedRootURI,
			"client_count", len(clientsToRestart),
		)
	}
	if handler != nil {
		handler(m.Statuses())
	}
}

func (m *Manager) SetDiagnosticHandler(h DiagnosticHandler) {
	m.mu.Lock()
	m.onDiag = h
	m.mu.Unlock()
}

func (m *Manager) SetStatusHandler(h func([]ServerStatus)) {
	m.mu.Lock()
	m.onStatus = h
	m.mu.Unlock()
}

func (m *Manager) OpenFile(filePath, content string) error { return m.openDocument(filePath, content) }

func (m *Manager) CloseFile(filePath string) error { return m.closeDocument(filePath) }

func (m *Manager) ChangeFile(filePath string, version int, newContent string) error {
	return m.changeDocument(filePath, version, newContent)
}

func withBootstrappedResult[T any](
	m *Manager,
	filePath string,
	call func(client *Client, uri string) (T, error),
) (T, error) {
	var out T
	err := m.withBootstrappedDocument(filePath, func(client *Client, uri string) error {
		result, err := call(client, uri)
		if err != nil {
			return err
		}
		out = result
		return nil
	})
	return out, err
}

func (m *Manager) Hover(filePath string, line, character int) (*HoverResult, error) {
	return withBootstrappedResult(m, filePath, func(client *Client, uri string) (*HoverResult, error) {
		return client.Hover(m.ctx, uri, line, character)
	})
}

func (m *Manager) Definition(filePath string, line, character int) ([]Location, error) {
	return withBootstrappedResult(m, filePath, func(client *Client, uri string) ([]Location, error) {
		return client.Definition(m.ctx, uri, line, character)
	})
}

func (m *Manager) References(filePath string, line, character int, includeDecl bool) ([]Location, error) {
	return withBootstrappedResult(m, filePath, func(client *Client, uri string) ([]Location, error) {
		return client.References(m.ctx, uri, line, character, includeDecl)
	})
}

func (m *Manager) DocumentSymbol(filePath string) ([]DocumentSymbol, error) {
	path := strings.TrimSpace(filePath)
	if path == "" {
		return nil, apperrors.Newf("LSP.DocumentSymbol", "file_path is required")
	}
	ext := strings.TrimPrefix(strings.ToLower(filepathExt(path)), ".")
	if isMarkdownExtension(ext) {
		return m.markdownDocumentSymbols(path)
	}

	return withBootstrappedResult(m, path, func(client *Client, uri string) ([]DocumentSymbol, error) {
		return client.DocumentSymbol(m.ctx, uri)
	})
}

func (m *Manager) Completion(filePath string, line, character int) ([]CompletionItem, error) {
	return withBootstrappedResult(m, filePath, func(client *Client, uri string) ([]CompletionItem, error) {
		return client.Completion(m.ctx, uri, line, character)
	})
}

func (m *Manager) Rename(filePath string, line, character int, newName string) (*WorkspaceEdit, error) {
	return withBootstrappedResult(m, filePath, func(client *Client, uri string) (*WorkspaceEdit, error) {
		return client.Rename(m.ctx, uri, line, character, newName)
	})
}

func (m *Manager) WorkspaceSymbol(filePath, language, query string) ([]WorkspaceSymbolResult, error) {
	if strings.TrimSpace(query) == "" {
		return nil, apperrors.Newf("LSP.WorkspaceSymbol", "query is required")
	}

	path := strings.TrimSpace(filePath)
	resolvedLanguage, err := m.resolveWorkspaceSymbolLanguage(path, language)
	if err != nil {
		return nil, err
	}

	if path != "" {
		if err := m.BootstrapLanguageFromFile(path); err != nil {
			return nil, err
		}
	} else {
		if err := m.BootstrapLanguage(resolvedLanguage); err != nil {
			return nil, err
		}
	}

	m.mu.RLock()
	cfg := m.languages[resolvedLanguage]
	var client *Client
	if cfg != nil {
		client = m.clients[cfg.Language]
	}
	m.mu.RUnlock()

	if client == nil || !client.Running() {
		return nil, apperrors.Newf("LSP.WorkspaceSymbol", "language client is not running: %s", resolvedLanguage)
	}

	return client.WorkspaceSymbol(m.ctx, query)
}

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

func (m *Manager) StopAll() {
	m.cancel()
	m.mu.Lock()
	defer m.mu.Unlock()

	for lang, client := range m.clients {
		_ = client.Stop()
		delete(m.clients, lang)
	}
	m.ctx, m.cancel = context.WithCancel(context.Background())

	m.resetDocumentStates()
}

func (m *Manager) Reload() {
	m.cancel()
	m.mu.Lock()
	for lang, client := range m.clients {
		_ = client.Stop()
		delete(m.clients, lang)
	}
	m.ctx, m.cancel = context.WithCancel(context.Background())
	handler := m.onStatus
	m.mu.Unlock()

	m.resetDocumentStates()

	if handler != nil {
		handler(m.Statuses())
	}
}

func (m *Manager) ensureClient(cfg *ServerConfig) (*Client, error) {
	m.mu.RLock()
	client, ok := m.clients[cfg.Language]
	m.mu.RUnlock()
	if ok && client.Running() {
		return client, nil
	}

	cmdPath, err := exec.LookPath(cfg.Command)
	if err != nil {
		return nil, apperrors.Newf("LSP.ensureClient", "%s not found in PATH", cfg.Command)
	}

	m.mu.Lock()
	if client, ok = m.clients[cfg.Language]; ok && client.Running() {
		m.mu.Unlock()
		return client, nil
	}

	client = NewClient(cfg.Language)
	m.clients[cfg.Language] = client

	client.SetDiagnosticHandler(m.onDiag)

	rootURI := effectiveRootURI(m.rootURI, m.workspaceID)
	if m.rootURI == "" && rootURI != "" {
		m.rootURI = rootURI
	}
	m.mu.Unlock()

	if err := client.Start(m.ctx, cmdPath, cfg.Args, rootURI); err != nil {
		m.mu.Lock()
		delete(m.clients, cfg.Language)
		m.mu.Unlock()
		return nil, err
	}

	m.mu.RLock()
	handler := m.onStatus
	m.mu.RUnlock()
	if handler != nil {
		handler(m.Statuses())
	}

	return client, nil
}

func effectiveRootURI(rootURI, workspaceID string) string {
	resolved := strings.TrimSpace(rootURI)
	if resolved != "" {
		return resolved
	}
	workspace := strings.TrimSpace(workspaceID)
	if workspace == "" || workspace == "default-workspace" {
		return ""
	}
	if strings.HasPrefix(workspace, "file://") {
		return workspace
	}
	return pathToURI(workspace)
}

func (m *Manager) resetDocumentStates() {
	m.docMu.Lock()
	defer m.docMu.Unlock()
	for _, st := range m.docStates {
		st.Open = false
		st.Version = 0
	}
}

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
