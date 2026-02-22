package lsp

import (
	"os"
	"strings"
	"time"
)

const (
	envLSPCacheEnabled = "MULTI_AGENT_LSP_CACHE_ENABLED"
	envLSPCacheDir     = "MULTI_AGENT_LSP_CACHE_DIR"

	defaultLSPCacheTTL             = 7 * 24 * time.Hour
	defaultLSPCacheCleanupInterval = 30 * time.Minute
)

var defaultLSPCacheDir = "/Users/mima0000/.multi-agent/lsp-cache"

type lspCacheConfig struct {
	enabled         bool
	dir             string
	ttl             time.Duration
	cleanupInterval time.Duration
}

type documentCacheRecord struct {
	URI          string `json:"uri"`
	Version      int    `json:"version"`
	MtimeNS      int64  `json:"mtime_ns"`
	Size         int64  `json:"size"`
	ContentHash  string `json:"content_hash"`
	LastSyncedAt int64  `json:"last_synced_at"`
	OpenState    bool   `json:"open_state"`
}

func (r documentCacheRecord) expired(now time.Time, ttl time.Duration) bool {
	if ttl <= 0 {
		return false
	}
	if r.LastSyncedAt <= 0 {
		return true
	}
	syncedAt := time.Unix(0, r.LastSyncedAt)
	return now.After(syncedAt.Add(ttl))
}

func loadLSPCacheConfigFromEnv() lspCacheConfig {
	cfg := lspCacheConfig{
		dir:             defaultLSPCacheDir,
		ttl:             defaultLSPCacheTTL,
		cleanupInterval: defaultLSPCacheCleanupInterval,
	}

	switch raw := strings.ToLower(strings.TrimSpace(os.Getenv(envLSPCacheEnabled))); raw {
	case "1", "true", "yes", "on":
		cfg.enabled = true
	case "0", "false", "no", "off", "":
		cfg.enabled = false
	default:
		// Invalid values default to disabled to preserve fail-closed behavior.
		cfg.enabled = false
	}

	if dir := strings.TrimSpace(os.Getenv(envLSPCacheDir)); dir != "" {
		cfg.dir = dir
	}

	return cfg
}
