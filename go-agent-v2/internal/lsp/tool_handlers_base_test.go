package lsp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDidChange_DefaultDoesNotPersistToDisk(t *testing.T) {
	m := NewManager(nil)

	tmp := t.TempDir()
	filePath := filepath.Join(tmp, "main.go")
	oldContent := "package main\nfunc oldName() {}\n"
	newContent := "package main\nfunc newName() {}\n"
	if err := os.WriteFile(filePath, []byte(oldContent), 0o644); err != nil {
		t.Fatalf("write old file: %v", err)
	}

	wc := &countingWriteCloser{}
	stub := newRunningStubClient("go", wc)
	m.mu.Lock()
	m.clients["go"] = stub
	m.mu.Unlock()

	h := NewToolHandlers(m, nil)
	raw, err := json.Marshal(map[string]any{
		"file_path":   filePath,
		"new_content": newContent,
		"version":     2,
	})
	if err != nil {
		t.Fatalf("marshal args: %v", err)
	}

	result := h.DidChange(raw)
	if !strings.Contains(result, "ok: file content updated") {
		t.Fatalf("DidChange result = %q, want ok", result)
	}
	if !strings.Contains(result, "disk not written") {
		t.Fatalf("DidChange result = %q, want disk-not-written hint", result)
	}

	got, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if string(got) != oldContent {
		t.Fatalf("disk content changed unexpectedly:\n%s", string(got))
	}
}

func TestDidChange_PersistToDiskWritesFile(t *testing.T) {
	m := NewManager(nil)

	tmp := t.TempDir()
	filePath := filepath.Join(tmp, "main.go")
	oldContent := "package main\nfunc oldName() {}\n"
	newContent := "package main\nfunc newName() {}\n"
	if err := os.WriteFile(filePath, []byte(oldContent), 0o644); err != nil {
		t.Fatalf("write old file: %v", err)
	}

	wc := &countingWriteCloser{}
	stub := newRunningStubClient("go", wc)
	m.mu.Lock()
	m.clients["go"] = stub
	m.mu.Unlock()

	h := NewToolHandlers(m, nil)
	raw, err := json.Marshal(map[string]any{
		"file_path":       filePath,
		"new_content":     newContent,
		"version":         2,
		"persist_to_disk": true,
	})
	if err != nil {
		t.Fatalf("marshal args: %v", err)
	}

	result := h.DidChange(raw)
	if !strings.Contains(result, "persisted to disk") {
		t.Fatalf("DidChange result = %q, want persisted hint", result)
	}

	got, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if string(got) != newContent {
		t.Fatalf("disk content mismatch:\n%s", string(got))
	}

	beforeWrites := wc.Count()
	if err := m.BootstrapDocument(filePath); err != nil {
		t.Fatalf("BootstrapDocument: %v", err)
	}
	afterWrites := wc.Count()
	if afterWrites != beforeWrites {
		t.Fatalf("bootstrap should not push stale content after persisted did_change: before=%d after=%d", beforeWrites, afterWrites)
	}
}
