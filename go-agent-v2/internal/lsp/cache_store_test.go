package lsp

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func setTestEnv(t *testing.T, key, value string) {
	t.Helper()
	oldValue, hadOld := os.LookupEnv(key)
	if value == "" {
		_ = os.Unsetenv(key)
	} else {
		if err := os.Setenv(key, value); err != nil {
			t.Fatalf("Setenv(%s): %v", key, err)
		}
	}
	t.Cleanup(func() {
		if hadOld {
			_ = os.Setenv(key, oldValue)
			return
		}
		_ = os.Unsetenv(key)
	})
}

func TestCacheDir_DefaultAndCustom(t *testing.T) {
	setTestEnv(t, envLSPCacheEnabled, "true")

	t.Run("default-dir", func(t *testing.T) {
		setTestEnv(t, envLSPCacheDir, "")
		cfg := loadLSPCacheConfigFromEnv()
		if !cfg.enabled {
			t.Fatal("cache should be enabled")
		}
		if got, want := cfg.dir, defaultLSPCacheDir; got != want {
			t.Fatalf("cache dir = %q, want %q", got, want)
		}
	})

	t.Run("custom-dir", func(t *testing.T) {
		customDir := filepath.Join(t.TempDir(), "custom-lsp-cache")
		setTestEnv(t, envLSPCacheDir, customDir)
		cfg := loadLSPCacheConfigFromEnv()
		if !cfg.enabled {
			t.Fatal("cache should be enabled")
		}
		if got, want := cfg.dir, customDir; got != want {
			t.Fatalf("cache dir = %q, want %q", got, want)
		}
	})
}

func TestCacheFallback_UnwritableDirectoryDowngradesToMemory(t *testing.T) {
	setTestEnv(t, envLSPCacheEnabled, "true")
	badPath := filepath.Join(t.TempDir(), "cache-file")
	if err := os.WriteFile(badPath, []byte("x"), 0o644); err != nil {
		t.Fatalf("write cache-file sentinel: %v", err)
	}
	setTestEnv(t, envLSPCacheDir, badPath)

	m := NewManager(nil)
	if m.cache == nil || !m.cache.Enabled() {
		t.Fatal("cache store should be initialized and enabled")
	}

	record := documentCacheRecord{
		URI:         "file:///tmp/cache-fallback.go",
		Version:     7,
		MtimeNS:     123,
		Size:        12,
		ContentHash: "hash-1",
		OpenState:   true,
	}
	m.cache.Upsert("workspace-A", "go", record)

	if m.cache.persistent {
		t.Fatal("cache should fallback to memory mode when cache dir is unwritable")
	}

	got, ok := m.cache.Load("workspace-A", "go", record.URI)
	if !ok {
		t.Fatal("expected to read record from in-memory fallback cache")
	}
	if got.ContentHash != record.ContentHash {
		t.Fatalf("content hash = %q, want %q", got.ContentHash, record.ContentHash)
	}
}

func TestCacheTTL_ExpiredEntryCleaned(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	cacheDir := filepath.Join(t.TempDir(), "ttl-cache")
	store := newLSPCacheStore(lspCacheConfig{
		enabled:         true,
		dir:             cacheDir,
		ttl:             7 * 24 * time.Hour,
		cleanupInterval: time.Minute,
	}, func() time.Time {
		return now
	})

	record := documentCacheRecord{
		URI:         "file:///tmp/cache-ttl.go",
		Version:     3,
		MtimeNS:     55,
		Size:        66,
		ContentHash: "hash-ttl",
		OpenState:   true,
	}
	store.Upsert("workspace-B", "go", record)
	if _, ok := store.Load("workspace-B", "go", record.URI); !ok {
		t.Fatal("expected freshly upserted record to be loadable")
	}

	now = now.Add(8 * 24 * time.Hour)
	store.maybeCleanup()
	if ok := store.waitCleanupIdle(2 * time.Second); !ok {
		t.Fatal("timed out waiting for async ttl cleanup to complete")
	}

	if _, ok := store.Load("workspace-B", "go", record.URI); ok {
		t.Fatal("expired record should be removed by TTL cleanup")
	}

	var jsonCount int
	walkErr := filepath.WalkDir(cacheDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && filepath.Ext(path) == ".json" {
			jsonCount++
		}
		return nil
	})
	if walkErr != nil {
		t.Fatalf("walk cache dir: %v", walkErr)
	}
	if jsonCount != 0 {
		t.Fatalf("expected ttl cleanup to remove persisted json cache files, found %d", jsonCount)
	}
}
