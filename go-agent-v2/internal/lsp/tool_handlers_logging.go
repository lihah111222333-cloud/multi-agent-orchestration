package lsp

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/multi-agent/go-agent-v2/pkg/logger"
)

const lspToolLogSchema = "lsp_tool_v1"
const lspToolCallMetaKey = "__tool_call_meta"

var errLSPManagerUnavailable = errors.New("lsp manager unavailable")

type lspToolCallLogger struct {
	tool      string
	startedAt time.Time
	baseAttrs []any
}

func startLSPToolCallFromArgs(tool string, args json.RawMessage, attrs ...any) *lspToolCallLogger {
	baseAttrs := make([]any, 0, len(attrs)+8)
	baseAttrs = append(baseAttrs, "raw_args_len", len(args))
	baseAttrs = append(baseAttrs, attrs...)
	baseAttrs = append(baseAttrs, lspToolCallMetaAttrs(args)...)
	return startLSPToolCall(tool, baseAttrs...)
}

func startLSPToolCall(tool string, attrs ...any) *lspToolCallLogger {
	call := &lspToolCallLogger{
		tool:      strings.TrimSpace(tool),
		startedAt: time.Now(),
		baseAttrs: append([]any(nil), attrs...),
	}
	call.step("begin")
	return call
}

func (c *lspToolCallLogger) done(attrs ...any) {
	c.emit("done", nil, true, attrs...)
}

func (c *lspToolCallLogger) fail(err error, attrs ...any) {
	c.emit("failed", err, true, attrs...)
}

func (c *lspToolCallLogger) step(event string, attrs ...any) {
	c.emit(event, nil, false, attrs...)
}

func (c *lspToolCallLogger) emit(event string, err error, withDuration bool, attrs ...any) {
	if c == nil {
		return
	}
	fields := make([]any, 0, len(c.baseAttrs)+len(attrs)+12)
	fields = append(
		fields,
		"log_schema", lspToolLogSchema,
		"ai_readable", true,
		logger.FieldToolName, c.tool,
		"event", strings.TrimSpace(event),
	)
	if withDuration {
		fields = append(fields, logger.FieldDurationMS, time.Since(c.startedAt).Milliseconds())
	}
	fields = append(fields, c.baseAttrs...)
	fields = append(fields, attrs...)
	if err != nil {
		fields = append(fields, logger.FieldError, err)
		logger.Warn("lsp_tool_event", fields...)
		return
	}
	logger.Info("lsp_tool_event", fields...)
}

func lspToolLogPath(filePath string) string {
	path := strings.TrimSpace(filePath)
	if path == "" {
		return ""
	}
	if strings.HasPrefix(path, "file://") {
		return path
	}
	if abs, err := filepath.Abs(path); err == nil {
		return abs
	}
	return path
}

func lspToolCallMetaAttrs(args json.RawMessage) []any {
	if len(args) == 0 {
		return nil
	}
	var payload map[string]any
	if err := json.Unmarshal(args, &payload); err != nil || payload == nil {
		return nil
	}
	meta, ok := payload[lspToolCallMetaKey].(map[string]any)
	if !ok || meta == nil {
		return nil
	}

	attrs := make([]any, 0, 8)
	if value := strings.TrimSpace(fmt.Sprint(meta["agent_id"])); value != "" && value != "<nil>" {
		attrs = append(attrs, logger.FieldAgentID, value)
	}
	if value := strings.TrimSpace(fmt.Sprint(meta["call_id"])); value != "" && value != "<nil>" {
		attrs = append(attrs, logger.FieldCallID, value)
	}
	if value := strings.TrimSpace(fmt.Sprint(meta["thread_id"])); value != "" && value != "<nil>" {
		attrs = append(attrs, logger.FieldThreadID, value)
	}
	if reqID, ok := normalizeLSPRequestID(meta["request_id"]); ok {
		attrs = append(attrs, logger.FieldReqID, reqID)
	}
	return attrs
}

func normalizeLSPRequestID(value any) (string, bool) {
	if value == nil {
		return "", false
	}
	switch v := value.(type) {
	case string:
		trimmed := strings.TrimSpace(v)
		if trimmed == "" {
			return "", false
		}
		return trimmed, true
	case int:
		return strconv.FormatInt(int64(v), 10), true
	case int8:
		return strconv.FormatInt(int64(v), 10), true
	case int16:
		return strconv.FormatInt(int64(v), 10), true
	case int32:
		return strconv.FormatInt(int64(v), 10), true
	case int64:
		return strconv.FormatInt(v, 10), true
	case uint:
		return strconv.FormatUint(uint64(v), 10), true
	case uint8:
		return strconv.FormatUint(uint64(v), 10), true
	case uint16:
		return strconv.FormatUint(uint64(v), 10), true
	case uint32:
		return strconv.FormatUint(uint64(v), 10), true
	case uint64:
		return strconv.FormatUint(v, 10), true
	case float64:
		if v == 0 {
			return "0", true
		}
		return strconv.FormatInt(int64(v), 10), true
	case float32:
		if v == 0 {
			return "0", true
		}
		return strconv.FormatInt(int64(v), 10), true
	default:
		text := strings.TrimSpace(fmt.Sprint(value))
		if text == "" || text == "<nil>" {
			return "", false
		}
		return text, true
	}
}

func runAndMarshalLogged[T any](
	call *lspToolCallLogger,
	run func() (T, error),
	formatErr func(error) string,
	emptyMsg string,
	isEmpty func(T) bool,
	resultAttrs func(T) []any,
	doneAttrs ...any,
) string {
	result, err := run()
	if err != nil {
		call.fail(err, append(doneAttrs, "stage", "execute")...)
		if formatErr != nil {
			return formatErr(err)
		}
		return toolError(err)
	}

	empty := false
	if isEmpty != nil {
		empty = isEmpty(result)
	}

	finalAttrs := append([]any(nil), doneAttrs...)
	if resultAttrs != nil {
		finalAttrs = append(finalAttrs, resultAttrs(result)...)
	}
	finalAttrs = append(finalAttrs, "result_empty", empty)
	call.done(finalAttrs...)

	if empty {
		return emptyMsg
	}

	data, err := json.Marshal(result)
	if err != nil {
		call.fail(err, append(doneAttrs, "stage", "marshal")...)
		return toolError(err)
	}
	return string(data)
}
