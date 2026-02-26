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

func TestDidChange_PersistToDiskWithoutLanguageServerStillReportsOK(t *testing.T) {
	m := NewManager(nil)

	tmp := t.TempDir()
	filePath := filepath.Join(tmp, "notes.txt")
	oldContent := "old value\n"
	newContent := "new value\n"
	if err := os.WriteFile(filePath, []byte(oldContent), 0o644); err != nil {
		t.Fatalf("write old file: %v", err)
	}

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
	lower := strings.ToLower(strings.TrimSpace(result))
	if strings.HasPrefix(lower, "error:") {
		t.Fatalf("DidChange result = %q, want ok-with-warning", result)
	}
	if !strings.Contains(result, "persisted to disk") {
		t.Fatalf("DidChange result = %q, want persisted hint", result)
	}
	if !strings.Contains(lower, "lsp sync unavailable") {
		t.Fatalf("DidChange result = %q, want lsp sync warning", result)
	}

	got, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if string(got) != newContent {
		t.Fatalf("disk content mismatch:\n%s", string(got))
	}
}

func TestReadFile_ReturnsFullContent(t *testing.T) {
	tmp := t.TempDir()
	filePath := filepath.Join(tmp, "full.txt")
	content := "line-1\nline-2\nline-3\n"
	if err := os.WriteFile(filePath, []byte(content), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	h := NewToolHandlers(NewManager(nil), nil)
	raw, err := json.Marshal(map[string]any{"file_path": filePath})
	if err != nil {
		t.Fatalf("marshal args: %v", err)
	}

	got := h.ReadFile(raw)
	if got != content {
		t.Fatalf("ReadFile() mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, content)
	}
}

func TestReplaceRange_PersistToDiskWritesFile(t *testing.T) {
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
		"line":            1,
		"column":          5,
		"end_line":        1,
		"end_column":      12,
		"new_text":        "newName",
		"persist_to_disk": true,
	})
	if err != nil {
		t.Fatalf("marshal args: %v", err)
	}

	result := h.ReplaceRange(raw)
	if !strings.Contains(result, "persisted to disk") {
		t.Fatalf("ReplaceRange result = %q, want persisted hint", result)
	}

	got, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if string(got) != newContent {
		t.Fatalf("disk content mismatch:\n--- got ---\n%s\n--- want ---\n%s", string(got), newContent)
	}
}

func TestReplaceRange_UsesUnsavedInMemoryContent(t *testing.T) {
	m := NewManager(nil)

	tmp := t.TempDir()
	filePath := filepath.Join(tmp, "main.go")
	diskContent := "package main\nfunc disk() {}\n"
	unsavedContent := "package main\nfunc memory() {}\n"
	expectedContent := "package main\nfunc hot() {}\n"
	if err := os.WriteFile(filePath, []byte(diskContent), 0o644); err != nil {
		t.Fatalf("write disk file: %v", err)
	}

	wc := &countingWriteCloser{}
	stub := newRunningStubClient("go", wc)
	m.mu.Lock()
	m.clients["go"] = stub
	m.mu.Unlock()

	h := NewToolHandlers(m, nil)
	didChangeRaw, err := json.Marshal(map[string]any{
		"file_path":   filePath,
		"new_content": unsavedContent,
		"version":     2,
	})
	if err != nil {
		t.Fatalf("marshal did_change args: %v", err)
	}
	didChangeResult := h.DidChange(didChangeRaw)
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(didChangeResult)), "error:") {
		t.Fatalf("DidChange result = %q, want success", didChangeResult)
	}

	replaceRaw, err := json.Marshal(map[string]any{
		"file_path":  filePath,
		"line":       1,
		"column":     5,
		"end_line":   1,
		"end_column": 11,
		"new_text":   "hot",
	})
	if err != nil {
		t.Fatalf("marshal replace_range args: %v", err)
	}
	replaceResult := h.ReplaceRange(replaceRaw)
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(replaceResult)), "error:") {
		t.Fatalf("ReplaceRange result = %q, want success", replaceResult)
	}

	uri := pathToURI(filePath)
	lock := m.documentLock(uri)
	lock.Lock()
	state := m.documentState(uri)
	stateContent := state.Content
	lock.Unlock()
	if stateContent != expectedContent {
		t.Fatalf("state content mismatch:\n--- got ---\n%s\n--- want ---\n%s", stateContent, expectedContent)
	}

	gotDisk, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("read disk file: %v", err)
	}
	if string(gotDisk) != diskContent {
		t.Fatalf("disk content should remain unchanged:\n--- got ---\n%s\n--- want ---\n%s", string(gotDisk), diskContent)
	}
}
