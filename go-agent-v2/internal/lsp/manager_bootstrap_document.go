package lsp

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"strings"
	"sync"

	apperrors "github.com/multi-agent/go-agent-v2/pkg/errors"
)

type documentSyncState struct {
	Version     int
	MtimeNS     int64
	Size        int64
	ContentHash string
	Content     string
	Language    string
	DiskBacked  bool
	Open        bool
}

// BootstrapDocument 自动保证文档与 LSP 服务端状态同步。
func (m *Manager) BootstrapDocument(filePath string) error {
	return m.withBootstrappedDocument(filePath, func(_ *Client, _ string) error { return nil })
}

func (m *Manager) withBootstrappedDocument(filePath string, fn func(client *Client, uri string) error) error {
	path := strings.TrimSpace(filePath)
	if path == "" {
		return apperrors.Newf("LSP.withBootstrappedDocument", "file_path is required")
	}
	uri := pathToURI(path)
	lock := m.documentLock(uri)
	lock.Lock()
	defer lock.Unlock()

	client, _, _, err := m.bootstrapDocumentLocked(path, uri)
	if err != nil {
		return err
	}
	return fn(client, uri)
}

func (m *Manager) bootstrapDocumentLocked(filePath, uri string) (*Client, *ServerConfig, *documentSyncState, error) {
	client, cfg, err := m.ensureClientForFile(filePath)
	if err != nil {
		return nil, nil, nil, err
	}

	state := m.documentState(uri)
	m.restoreDocumentStateFromCache(filePath, uri, cfg.Language, state)
	// 最新内容来自 lsp_did_change（未落盘）时，不可用磁盘快照回写旧内容。
	// 若磁盘已追平内存哈希，则切回 disk-backed。
	if state.Open && !state.DiskBacked {
		if content, stat, hash, readErr := loadDocumentSnapshot(filePath); readErr == nil && hash == state.ContentHash {
			state.MtimeNS = stat.ModTime().UnixNano()
			state.Size = stat.Size()
			state.Content = content
			state.DiskBacked = true
			m.upsertDocumentStateCache(filePath, uri, state)
		}
		return client, cfg, state, nil
	}

	content, stat, hash, err := loadDocumentSnapshot(filePath)
	if err != nil {
		return nil, nil, nil, err
	}

	if !state.Open {
		openVersion := nextOpenVersionFromBaseline(state, stat.ModTime().UnixNano(), stat.Size(), hash)
		if err := client.DidOpenVersioned(uri, cfg.Language, openVersion, content); err != nil {
			return nil, nil, nil, apperrors.Wrap(err, "LSP.BootstrapDocument", "didOpen")
		}
		state.openWith(openVersion, stat.ModTime().UnixNano(), stat.Size(), hash, content, cfg.Language, true)
		m.upsertDocumentStateCache(filePath, uri, state)
		return client, cfg, state, nil
	}

	isStale := state.MtimeNS != stat.ModTime().UnixNano() || state.Size != stat.Size() || state.ContentHash != hash
	if !isStale {
		return client, cfg, state, nil
	}

	nextVersion := state.Version + 1
	if nextVersion <= 0 {
		nextVersion = 1
	}
	if err := client.DidChange(uri, nextVersion, content); err == nil {
		state.Version = nextVersion
		state.MtimeNS = stat.ModTime().UnixNano()
		state.Size = stat.Size()
		state.ContentHash = hash
		state.Content = content
		state.DiskBacked = true
		m.upsertDocumentStateCache(filePath, uri, state)
		return client, cfg, state, nil
	}

	if err := m.reopenDocument(client, cfg, uri, nextVersion, content); err == nil {
		state.openWith(nextVersion, stat.ModTime().UnixNano(), stat.Size(), hash, content, cfg.Language, true)
		m.upsertDocumentStateCache(filePath, uri, state)
		return client, cfg, state, nil
	}

	restarted, restartedCfg, err := m.restartClientForLanguage(cfg.Language)
	if err != nil {
		return nil, nil, nil, apperrors.Wrap(err, "LSP.BootstrapDocument", "restart client")
	}
	if err := restarted.DidOpenVersioned(uri, restartedCfg.Language, nextVersion, content); err != nil {
		return nil, nil, nil, apperrors.Wrap(err, "LSP.BootstrapDocument", "didOpen after restart")
	}
	state.openWith(nextVersion, stat.ModTime().UnixNano(), stat.Size(), hash, content, restartedCfg.Language, true)
	m.upsertDocumentStateCache(filePath, uri, state)
	return restarted, restartedCfg, state, nil
}

func (m *Manager) openDocument(filePath, content string) error {
	path := strings.TrimSpace(filePath)
	if path == "" {
		return apperrors.Newf("LSP.openDocument", "file_path is required")
	}

	client, cfg, err := m.ensureClientForFile(path)
	if err != nil {
		return err
	}

	uri := pathToURI(path)
	lock := m.documentLock(uri)
	lock.Lock()
	defer lock.Unlock()

	state := m.documentState(uri)
	openVersion := state.Version + 1
	if openVersion <= 0 {
		openVersion = 1
	}

	if err := client.DidOpenVersioned(uri, cfg.Language, openVersion, content); err != nil {
		return err
	}

	hash := hashBytes([]byte(content))

	if stat, err := os.Stat(path); err == nil {
		state.openWith(openVersion, stat.ModTime().UnixNano(), stat.Size(), hash, content, cfg.Language, true)
		m.upsertDocumentStateCache(path, uri, state)
		return nil
	}

	state.openWith(openVersion, 0, int64(len(content)), hash, content, cfg.Language, false)
	m.upsertDocumentStateCache(path, uri, state)
	return nil
}

func (m *Manager) closeDocument(filePath string) error {
	path := strings.TrimSpace(filePath)
	if path == "" {
		return nil
	}
	uri := pathToURI(path)
	lock := m.documentLock(uri)
	lock.Lock()
	defer lock.Unlock()

	state := m.documentState(uri)
	if !state.Open {
		return nil
	}

	ext := strings.TrimPrefix(strings.ToLower(filepathExt(path)), ".")
	if ext == "" {
		state.Open = false
		m.upsertDocumentStateCache(path, uri, state)
		return nil
	}

	m.mu.RLock()
	cfg := m.configs[ext]
	var client *Client
	if cfg != nil {
		client = m.clients[cfg.Language]
	}
	m.mu.RUnlock()

	if client != nil && client.Running() {
		if err := client.DidClose(uri); err != nil {
			return err
		}
	}

	state.Open = false
	m.upsertDocumentStateCache(path, uri, state)
	return nil
}

func (m *Manager) changeDocument(filePath string, version int, newContent string) error {
	path := strings.TrimSpace(filePath)
	if path == "" {
		return apperrors.Newf("LSP.changeDocument", "file_path is required")
	}

	uri := pathToURI(path)
	lock := m.documentLock(uri)
	lock.Lock()
	defer lock.Unlock()

	client, cfg, state, err := m.bootstrapDocumentLocked(path, uri)
	if err != nil {
		return err
	}

	hash := hashBytes([]byte(newContent))

	if version <= state.Version {
		version = state.Version + 1
	}
	if version <= 0 {
		version = 1
	}

	if err := client.DidChange(uri, version, newContent); err != nil {
		if reopenErr := m.reopenDocument(client, cfg, uri, version, newContent); reopenErr == nil {
			if stat, statErr := os.Stat(path); statErr == nil {
				state.openWith(version, stat.ModTime().UnixNano(), stat.Size(), hash, newContent, cfg.Language, true)
			} else {
				state.openWith(version, 0, int64(len(newContent)), hash, newContent, cfg.Language, false)
			}
			m.upsertDocumentStateCache(path, uri, state)
			return nil
		}
		restarted, restartedCfg, restartErr := m.restartClientForLanguage(cfg.Language)
		if restartErr != nil {
			return apperrors.Wrap(restartErr, "LSP.changeDocument", "restart client")
		}
		if err := restarted.DidOpenVersioned(uri, restartedCfg.Language, version, newContent); err != nil {
			return apperrors.Wrap(err, "LSP.changeDocument", "didOpen after restart")
		}
		if stat, statErr := os.Stat(path); statErr == nil {
			state.openWith(version, stat.ModTime().UnixNano(), stat.Size(), hash, newContent, restartedCfg.Language, true)
		} else {
			state.openWith(version, 0, int64(len(newContent)), hash, newContent, restartedCfg.Language, false)
		}
		m.upsertDocumentStateCache(path, uri, state)
		return nil
	}

	state.Version = version
	state.Content = newContent
	state.ContentHash = hash
	state.Size = int64(len(newContent))
	state.DiskBacked = false
	state.Language = normalizeLanguage(cfg.Language)
	state.Open = true
	if stat, err := os.Stat(path); err == nil {
		state.MtimeNS = stat.ModTime().UnixNano()
	} else {
		state.MtimeNS = 0
	}
	m.upsertDocumentStateCache(path, uri, state)
	return nil
}

func (m *Manager) reopenDocument(client *Client, cfg *ServerConfig, uri string, version int, content string) error {
	_ = client.DidClose(uri)
	return client.DidOpenVersioned(uri, cfg.Language, version, content)
}

func (m *Manager) documentLock(uri string) *sync.Mutex {
	m.docMu.Lock()
	defer m.docMu.Unlock()
	lock := m.docLocks[uri]
	if lock == nil {
		lock = &sync.Mutex{}
		m.docLocks[uri] = lock
	}
	return lock
}

func (m *Manager) documentState(uri string) *documentSyncState {
	m.docMu.Lock()
	defer m.docMu.Unlock()
	state := m.docStates[uri]
	if state == nil {
		state = &documentSyncState{}
		m.docStates[uri] = state
	}
	return state
}

func (m *Manager) restoreDocumentStateFromCache(filePath, uri, language string, state *documentSyncState) {
	if state == nil || state.Version != 0 || state.Open {
		return
	}
	if m.cache == nil || !m.cache.Enabled() {
		return
	}

	record, ok := m.cache.Load(m.cacheWorkspaceHint(filePath), language, uri)
	if !ok {
		return
	}

	if record.Version > 0 {
		state.Version = record.Version
	}
	state.MtimeNS = record.MtimeNS
	state.Size = record.Size
	state.ContentHash = record.ContentHash
	state.Content = ""
	state.Language = normalizeLanguage(language)
	if state.Language == "" {
		state.Language = m.languageForFile(filePath)
	}
	state.DiskBacked = true
	// 缓存只作为基线；重启后仍需以真实磁盘状态+实时同步为准。
	state.Open = false
}

func (m *Manager) upsertDocumentStateCache(filePath, uri string, state *documentSyncState) {
	if state == nil || m.cache == nil || !m.cache.Enabled() {
		return
	}

	language := normalizeLanguage(state.Language)
	if language == "" {
		language = m.languageForFile(filePath)
	}
	if language == "" {
		return
	}

	m.cache.Upsert(m.cacheWorkspaceHint(filePath), language, documentCacheRecord{
		URI:         uri,
		Version:     state.Version,
		MtimeNS:     state.MtimeNS,
		Size:        state.Size,
		ContentHash: state.ContentHash,
		OpenState:   state.Open,
	})
}

func (m *Manager) cacheWorkspaceHint(filePath string) string {
	_ = filePath
	m.mu.RLock()
	root := strings.TrimSpace(m.rootURI)
	workspaceID := strings.TrimSpace(m.workspaceID)
	m.mu.RUnlock()
	if root != "" {
		return root
	}
	if workspaceID != "" {
		return workspaceID
	}
	return "default-workspace"
}

func (s *documentSyncState) openWith(
	version int,
	mtimeNS, size int64,
	hash, content, language string,
	diskBacked bool,
) {
	if version <= 0 {
		version = 1
	}
	s.Version = version
	s.MtimeNS = mtimeNS
	s.Size = size
	s.ContentHash = hash
	s.Content = content
	s.Language = normalizeLanguage(language)
	s.DiskBacked = diskBacked
	s.Open = true
}

func nextOpenVersionFromBaseline(state *documentSyncState, diskMtimeNS, diskSize int64, diskHash string) int {
	if state == nil || state.Version <= 0 {
		return 1
	}
	if state.MtimeNS != diskMtimeNS || state.Size != diskSize || state.ContentHash != diskHash {
		return state.Version + 1
	}
	return state.Version
}

func hashBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func loadDocumentSnapshot(filePath string) (content string, stat os.FileInfo, hash string, err error) {
	data, readErr := os.ReadFile(filePath)
	if readErr != nil {
		return "", nil, "", apperrors.Newf(
			"LSP.BootstrapDocument",
			"failed to read file %s: %v; provide file_path or open explicitly via lsp_open_file",
			filePath, readErr,
		)
	}
	stat, statErr := os.Stat(filePath)
	if statErr != nil {
		return "", nil, "", apperrors.Newf("LSP.BootstrapDocument", "failed to stat file %s: %v", filePath, statErr)
	}
	return string(data), stat, hashBytes(data), nil
}

func filepathExt(path string) string {
	lastDot := strings.LastIndex(path, ".")
	lastSlash := strings.LastIndexAny(path, "/\\")
	if lastDot <= lastSlash {
		return ""
	}
	return path[lastDot:]
}
