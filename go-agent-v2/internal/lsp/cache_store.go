package lsp

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/multi-agent/go-agent-v2/pkg/logger"
)

type lspCacheStore struct {
	mu sync.Mutex

	config lspCacheConfig
	now    func() time.Time

	persistent      bool
	persistentReady bool
	fallbackWarned  bool
	cleanupRunning  bool

	memory      map[string]documentCacheRecord
	lastCleanup time.Time
}

type lspCacheKey struct {
	workspaceHash string
	language      string
	uriHash       string
}

func newLSPCacheStoreFromEnv() *lspCacheStore {
	return newLSPCacheStore(loadLSPCacheConfigFromEnv(), time.Now)
}

func newLSPCacheStore(config lspCacheConfig, nowFn func() time.Time) *lspCacheStore {
	if nowFn == nil {
		nowFn = time.Now
	}
	if strings.TrimSpace(config.dir) == "" {
		config.dir = defaultLSPCacheDir
	}
	if config.ttl <= 0 {
		config.ttl = defaultLSPCacheTTL
	}
	if config.cleanupInterval <= 0 {
		config.cleanupInterval = defaultLSPCacheCleanupInterval
	}

	return &lspCacheStore{
		config:     config,
		now:        nowFn,
		persistent: config.enabled,
		memory:     make(map[string]documentCacheRecord),
	}
}

func (s *lspCacheStore) Enabled() bool {
	return s != nil && s.config.enabled
}

func makeLSPCacheKey(workspaceID, language, uri string) (lspCacheKey, bool) {
	lang := normalizeLanguage(language)
	trimmedURI := strings.TrimSpace(uri)
	if lang == "" || trimmedURI == "" {
		return lspCacheKey{}, false
	}

	workspace := strings.TrimSpace(workspaceID)
	if workspace == "" {
		workspace = "default-workspace"
	}

	return lspCacheKey{
		workspaceHash: hashBytes([]byte(workspace)),
		language:      lang,
		uriHash:       hashBytes([]byte(trimmedURI)),
	}, true
}

func (k lspCacheKey) memoryKey() string {
	return k.workspaceHash + "|" + k.language + "|" + k.uriHash
}

func (s *lspCacheStore) cachePath(key lspCacheKey) string {
	return filepath.Join(s.config.dir, key.workspaceHash, key.language, key.uriHash+".json")
}

func (s *lspCacheStore) Load(workspaceID, language, uri string) (documentCacheRecord, bool) {
	var zero documentCacheRecord
	if s == nil || !s.config.enabled {
		return zero, false
	}

	key, ok := makeLSPCacheKey(workspaceID, language, uri)
	if !ok {
		return zero, false
	}
	s.maybeCleanup()

	memoryKey := key.memoryKey()
	now := s.now()

	s.mu.Lock()
	if record, found := s.memory[memoryKey]; found {
		if record.expired(now, s.config.ttl) {
			delete(s.memory, memoryKey)
			s.mu.Unlock()
			if s.ensurePersistentReady() {
				_ = os.Remove(s.cachePath(key))
			}
			return zero, false
		}
		s.mu.Unlock()
		return record, true
	}
	s.mu.Unlock()

	if !s.ensurePersistentReady() {
		return zero, false
	}

	path := s.cachePath(key)
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return zero, false
		}
		s.fallbackToMemory("read", err)
		return zero, false
	}

	var record documentCacheRecord
	if err := json.Unmarshal(data, &record); err != nil {
		logger.Warn("lsp cache: invalid record, dropped",
			logger.FieldPath, path,
			logger.FieldError, err,
		)
		_ = os.Remove(path)
		return zero, false
	}
	if record.URI != strings.TrimSpace(uri) {
		return zero, false
	}
	if record.expired(now, s.config.ttl) {
		_ = os.Remove(path)
		return zero, false
	}

	s.mu.Lock()
	s.memory[memoryKey] = record
	s.mu.Unlock()
	return record, true
}

func (s *lspCacheStore) Upsert(workspaceID, language string, record documentCacheRecord) {
	if s == nil || !s.config.enabled {
		return
	}

	record.URI = strings.TrimSpace(record.URI)
	key, ok := makeLSPCacheKey(workspaceID, language, record.URI)
	if !ok {
		return
	}
	record.LastSyncedAt = s.now().UnixNano()
	s.maybeCleanup()

	s.mu.Lock()
	s.memory[key.memoryKey()] = record
	s.mu.Unlock()

	if !s.ensurePersistentReady() {
		return
	}

	path := s.cachePath(key)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		s.fallbackToMemory("mkdir", err)
		return
	}

	payload, err := json.Marshal(record)
	if err != nil {
		logger.Warn("lsp cache: marshal failed", logger.FieldError, err)
		return
	}

	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, payload, 0o600); err != nil {
		s.fallbackToMemory("write", err)
		return
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		s.fallbackToMemory("rename", err)
		return
	}
}

func (s *lspCacheStore) maybeCleanup() {
	if s == nil || !s.config.enabled || s.config.ttl <= 0 {
		return
	}

	now := s.now()
	s.mu.Lock()
	if !s.lastCleanup.IsZero() && now.Sub(s.lastCleanup) < s.config.cleanupInterval {
		s.mu.Unlock()
		return
	}
	if s.cleanupRunning {
		s.mu.Unlock()
		return
	}
	s.lastCleanup = now
	s.cleanupRunning = true
	s.mu.Unlock()

	s.cleanupMemory(now)
	go s.cleanupPersistentAsync(now)
}

func (s *lspCacheStore) cleanupMemory(now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, record := range s.memory {
		if record.expired(now, s.config.ttl) {
			delete(s.memory, key)
		}
	}
}

func (s *lspCacheStore) cleanupPersistent(now time.Time) error {
	root := s.config.dir
	if root == "" {
		return nil
	}
	if _, err := os.Stat(root); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}

	return filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() || !strings.HasSuffix(d.Name(), ".json") {
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}

		var record documentCacheRecord
		if err := json.Unmarshal(data, &record); err != nil {
			if rmErr := os.Remove(path); rmErr != nil && !errors.Is(rmErr, fs.ErrNotExist) {
				return rmErr
			}
			return nil
		}
		if !record.expired(now, s.config.ttl) {
			return nil
		}
		if rmErr := os.Remove(path); rmErr != nil && !errors.Is(rmErr, fs.ErrNotExist) {
			return rmErr
		}
		return nil
	})
}

func (s *lspCacheStore) cleanupPersistentAsync(now time.Time) {
	defer func() {
		s.mu.Lock()
		s.cleanupRunning = false
		s.mu.Unlock()
	}()

	if !s.ensurePersistentReady() {
		return
	}
	if err := s.cleanupPersistent(now); err != nil {
		logger.Warn("lsp cache: ttl cleanup failed",
			logger.FieldPath, s.config.dir,
			logger.FieldError, err,
		)
	}
}

func (s *lspCacheStore) waitCleanupIdle(timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		s.mu.Lock()
		running := s.cleanupRunning
		s.mu.Unlock()
		if !running {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func (s *lspCacheStore) ensurePersistentReady() bool {
	if s == nil || !s.config.enabled {
		return false
	}

	s.mu.Lock()
	if !s.persistent {
		s.mu.Unlock()
		return false
	}
	if s.persistentReady {
		s.mu.Unlock()
		return true
	}
	dir := s.config.dir
	s.mu.Unlock()

	if err := os.MkdirAll(dir, 0o755); err != nil {
		s.fallbackToMemory("mkdir", err)
		return false
	}

	probePath := filepath.Join(dir, ".cache-write-probe")
	probeFile, err := os.OpenFile(probePath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		s.fallbackToMemory("open-probe", err)
		return false
	}
	if _, err := probeFile.Write([]byte("ok")); err != nil {
		_ = probeFile.Close()
		_ = os.Remove(probePath)
		s.fallbackToMemory("write-probe", err)
		return false
	}
	if err := probeFile.Close(); err != nil {
		_ = os.Remove(probePath)
		s.fallbackToMemory("close-probe", err)
		return false
	}
	_ = os.Remove(probePath)

	s.mu.Lock()
	if s.persistent {
		s.persistentReady = true
	}
	s.mu.Unlock()
	return true
}

func (s *lspCacheStore) fallbackToMemory(action string, err error) {
	if s == nil {
		return
	}

	s.mu.Lock()
	alreadyWarned := s.fallbackWarned
	dir := s.config.dir
	s.persistent = false
	s.persistentReady = false
	s.fallbackWarned = true
	s.mu.Unlock()

	if alreadyWarned {
		return
	}
	logger.Warn("lsp cache: persistent mode unavailable, fallback to memory",
		logger.FieldAction, action,
		logger.FieldPath, dir,
		logger.FieldError, err,
	)
}
