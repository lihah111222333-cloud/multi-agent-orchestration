package logger

import (
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"
)

// ─── StderrCollector Tests ───

func TestStderrCollector_BasicLine(t *testing.T) {
	// Capture slog output via a test handler
	var records []slog.Record
	handler := &captureHandler{records: &records}
	slog.SetDefault(slog.New(handler))
	defer slog.SetDefault(slog.Default())

	c := NewStderrCollector("test-agent")
	_, _ = c.Write([]byte("hello from stderr\n"))
	_ = c.Close()

	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Message != "hello from stderr" {
		t.Errorf("unexpected message: %s", records[0].Message)
	}
	if records[0].Level != slog.LevelInfo {
		t.Errorf("expected INFO, got %s", records[0].Level)
	}
}

func TestStderrCollector_ErrorLevel(t *testing.T) {
	var records []slog.Record
	handler := &captureHandler{records: &records}
	slog.SetDefault(slog.New(handler))
	defer slog.SetDefault(slog.Default())

	c := NewStderrCollector("test-agent")
	_, _ = c.Write([]byte("something went Error here\n"))
	_ = c.Close()

	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Level != slog.LevelError {
		t.Errorf("expected ERROR, got %s", records[0].Level)
	}
}

func TestStderrCollector_EmptyLines(t *testing.T) {
	var records []slog.Record
	handler := &captureHandler{records: &records}
	slog.SetDefault(slog.New(handler))
	defer slog.SetDefault(slog.Default())

	c := NewStderrCollector("test-agent")
	_, _ = c.Write([]byte("\n\nactual line\n\n"))
	_ = c.Close()

	if len(records) != 1 {
		t.Fatalf("expected 1 record (empty lines skipped), got %d", len(records))
	}
}

func TestStderrCollector_MultipleLines(t *testing.T) {
	var records []slog.Record
	handler := &captureHandler{records: &records}
	slog.SetDefault(slog.New(handler))
	defer slog.SetDefault(slog.Default())

	c := NewStderrCollector("test-agent")
	_, _ = c.Write([]byte("line1\nline2\nline3\n"))
	_ = c.Close()

	if len(records) != 3 {
		t.Fatalf("expected 3 records, got %d", len(records))
	}
}

func TestContainsErrorKeyword(t *testing.T) {
	tests := []struct {
		line string
		want bool
	}{
		{"normal line", false},
		{"this has error in it", true},
		{"ERROR: something", true},
		{"panic: goroutine", true},
		{"FATAL crash", true},
		{"", false},
		{"err", false}, // shorter than "error"
	}
	for _, tt := range tests {
		got := containsErrorKeyword(tt.line)
		if got != tt.want {
			t.Errorf("containsErrorKeyword(%q) = %v, want %v", tt.line, got, tt.want)
		}
	}
}

// ─── MultiHandler Tests ───

func TestMultiHandler_FanOut(t *testing.T) {
	var records1, records2 []slog.Record
	h1 := &captureHandler{records: &records1}
	h2 := &captureHandler{records: &records2}
	multi := NewMultiHandler(h1, h2)

	logger := slog.New(multi)
	logger.Info("test message")

	if len(records1) != 1 || len(records2) != 1 {
		t.Errorf("expected 1 record in each handler, got %d and %d", len(records1), len(records2))
	}
}

// ─── applyAttr Tests ───

func TestApplyAttr_KnownFields(t *testing.T) {
	e := &LogEntry{}

	applyAttr(e, slog.String(FieldSource, "codex"))
	applyAttr(e, slog.String(FieldComponent, "stderr"))
	applyAttr(e, slog.String(FieldAgentID, "agent-1"))
	applyAttr(e, slog.String(FieldThreadID, "thread-1"))
	applyAttr(e, slog.String(FieldTraceID, "trace-1"))
	applyAttr(e, slog.String(FieldEventType, "turn_started"))
	applyAttr(e, slog.String(FieldToolName, "lsp_definition"))
	applyAttr(e, slog.String("logger", "test.logger"))

	if e.Source != "codex" {
		t.Errorf("Source = %q", e.Source)
	}
	if e.Component != "stderr" {
		t.Errorf("Component = %q", e.Component)
	}
	if e.AgentID != "agent-1" {
		t.Errorf("AgentID = %q", e.AgentID)
	}
	if e.ThreadID != "thread-1" {
		t.Errorf("ThreadID = %q", e.ThreadID)
	}
	if e.TraceID != "trace-1" {
		t.Errorf("TraceID = %q", e.TraceID)
	}
	if e.EventType != "turn_started" {
		t.Errorf("EventType = %q", e.EventType)
	}
	if e.ToolName != "lsp_definition" {
		t.Errorf("ToolName = %q", e.ToolName)
	}
	if e.Logger != "test.logger" {
		t.Errorf("Logger = %q", e.Logger)
	}
}

func TestApplyAttr_UnknownGoesToExtra(t *testing.T) {
	e := &LogEntry{}
	applyAttr(e, slog.String("custom_key", "custom_val"))

	if e.Extra == nil {
		t.Fatal("Extra should not be nil")
	}
	if v, ok := e.Extra["custom_key"]; !ok || v != "custom_val" {
		t.Errorf("Extra[custom_key] = %v", v)
	}
}

func TestApplyAttr_DurationMS(t *testing.T) {
	e := &LogEntry{}
	applyAttr(e, slog.Int64(FieldDurationMS, 42))

	if e.DurationMS == nil || *e.DurationMS != 42 {
		t.Errorf("DurationMS = %v", e.DurationMS)
	}
}

// ─── DBHandler Tests (in-memory, no PG) ───

func TestDBHandler_Handle_PopulatesEntry(t *testing.T) {
	// Can't test full DB write without PG, but can test Handle populates buf
	// Use a nil pool — we'll drain the chan before flush tries to write
	h := &DBHandler{
		buf:   make(chan LogEntry, 10),
		level: slog.LevelInfo,
		done:  make(chan struct{}),
	}

	record := slog.NewRecord(time.Now(), slog.LevelInfo, "test msg", 0)
	record.AddAttrs(
		slog.String(FieldSource, "system"),
		slog.String(FieldAgentID, "a1"),
	)

	if err := h.Handle(context.Background(), record); err != nil {
		t.Fatal(err)
	}

	select {
	case entry := <-h.buf:
		if entry.Message != "test msg" {
			t.Errorf("Message = %q", entry.Message)
		}
		if entry.Source != "system" {
			t.Errorf("Source = %q", entry.Source)
		}
		if entry.AgentID != "a1" {
			t.Errorf("AgentID = %q", entry.AgentID)
		}
	default:
		t.Fatal("expected entry in buffer")
	}
}

func TestDBHandler_NotEnabled_BelowLevel(t *testing.T) {
	h := &DBHandler{level: slog.LevelWarn}
	if h.Enabled(context.Background(), slog.LevelInfo) {
		t.Error("should not be enabled for INFO when level is WARN")
	}
	if !h.Enabled(context.Background(), slog.LevelError) {
		t.Error("should be enabled for ERROR when level is WARN")
	}
}

// ─── captureHandler: test helper ───

type captureHandler struct {
	records *[]slog.Record
}

func (h *captureHandler) Enabled(_ context.Context, _ slog.Level) bool { return true }
func (h *captureHandler) Handle(_ context.Context, r slog.Record) error {
	*h.records = append(*h.records, r)
	return nil
}
func (h *captureHandler) WithAttrs(_ []slog.Attr) slog.Handler { return h }
func (h *captureHandler) WithGroup(_ string) slog.Handler      { return h }

// ─── containsErrorKeyword uses strings ───

func init() {
	// Ensure strings package is used (for test only)
	_ = strings.Contains
}
