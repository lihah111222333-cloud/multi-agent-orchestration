package codex

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	apperrors "github.com/multi-agent/go-agent-v2/pkg/errors"
	"github.com/multi-agent/go-agent-v2/pkg/util"
)

func parseEnvInt(name string) (raw string, value int, err error) {
	if raw = strings.TrimSpace(os.Getenv(name)); raw == "" { return "", 0, nil }
	value, err = strconv.Atoi(raw)
	return raw, value, err
}

func mergeDetails(base map[string]any, extras ...map[string]any) map[string]any {
	if base == nil {
		base = map[string]any{}
	}
	for _, extra := range extras {
		for key, value := range extra {
			base[key] = value
		}
	}
	return base
}

func reconnectDetails(trigger, activeTurnID string, details map[string]any) map[string]any {
	return mergeDetails(map[string]any{
		"trigger":      trigger,
		"activeTurnId": activeTurnID,
	}, details)
}

func buildTurnStartInputs(prompt string, images, files []string) []asTurnStartInput {
	inputs := make([]asTurnStartInput, 0, 1+len(images)+len(files))
	trimmedPrompt := strings.TrimSpace(prompt)
	if trimmedPrompt != "" || (len(images) == 0 && len(files) == 0) {
		inputs = append(inputs, asTurnStartInput{Type: "text", Text: prompt})
	}

	for _, raw := range images {
		path := strings.TrimSpace(raw)
		if path == "" {
			continue
		}
		if util.IsRemoteImageURL(path) {
			inputs = append(inputs, asTurnStartInput{Type: "image", URL: path})
			continue
		}
		inputs = append(inputs, asTurnStartInput{Type: "localImage", Path: path})
	}

	for _, raw := range files {
		path := strings.TrimSpace(raw)
		if path == "" {
			continue
		}
		name := strings.TrimSpace(filepath.Base(path))
		if name == "" || name == "." || name == string(filepath.Separator) { name = "file" }
		inputs = append(inputs, asTurnStartInput{
			Type: "mention",
			Name: name,
			Path: path,
		})
	}

	if len(inputs) == 0 {
		inputs = append(inputs, asTurnStartInput{Type: "text", Text: prompt})
	}
	return inputs
}

func isNotInitializedRPCError(err error) bool {
	return rpcErrorContains(err, false, "not initialized")
}

func retryAfterNotInitialized[T any](
	scope string,
	markInitFailureAsRetried bool,
	markRetryFailureAsRetried bool,
	callFn func() (T, error),
	initializeFn func() error,
) (result T, retriedAfterInit bool, err error) {
	result, err = callFn()
	if err == nil || !isNotInitializedRPCError(err) || initializeFn == nil {
		return result, false, err
	}
	if initErr := initializeFn(); initErr != nil {
		return result, markInitFailureAsRetried, apperrors.Wrap(initErr, scope, "initialize")
	}
	result, err = callFn()
	if err != nil {
		return result, markRetryFailureAsRetried, err
	}
	return result, true, nil
}

func ensureListenerWithAutoInitialize(
	threadID string,
	rpcCall func(method string, params any, timeout time.Duration) (json.RawMessage, error),
	initializeFn func() error,
) (resolvedID string, retriedAfterInit bool, err error) {
	id := strings.TrimSpace(threadID)
	if id == "" { return "", false, apperrors.New("ensureListenerViaThreadResume", "thread id is required") }
	if rpcCall == nil { return "", false, apperrors.New("ensureListenerViaThreadResume", "rpc call func is nil") }
	return retryAfterNotInitialized(
		"ensureListenerWithAutoInitialize",
		true,
		true,
		func() (string, error) {
			return callThreadResume("ensureListenerViaThreadResume", rpcCall, asThreadResumeParams{ThreadID: id}, appServerListenerEnsureTimeout)
		},
		initializeFn,
	)
}

func callWithNotInitializedRecovery(
	rpcCall func(method string, params any, timeout time.Duration) (json.RawMessage, error),
	initializeFn func() error,
	method string,
	params any,
	timeout time.Duration,
) (json.RawMessage, bool, error) {
	if rpcCall == nil { return nil, false, apperrors.New("callWithNotInitializedRecovery", "rpc call func is nil") }
	return retryAfterNotInitialized(
		"callWithNotInitializedRecovery",
		false,
		false,
		func() (json.RawMessage, error) {
			return rpcCall(method, params, timeout)
		},
		initializeFn,
	)
}

func callThreadResume(
	scope string,
	rpcCall func(method string, params any, timeout time.Duration) (json.RawMessage, error),
	params asThreadResumeParams,
	timeout time.Duration,
) (string, error) {
	result, err := rpcCall("thread/resume", params, timeout)
	if err != nil {
		return "", apperrors.Wrap(err, scope, "thread/resume")
	}
	return parseThreadResumeResult(result, params.ThreadID)
}

func parseThreadResumeResult(raw json.RawMessage, fallbackID string) (string, error) {
	fallback := strings.TrimSpace(fallbackID)
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		if fallback == "" {
			return "", apperrors.New("parseThreadResumeResult", "thread/resume returned empty response without fallback thread ID")
		}
		return fallback, nil
	}

	var resp struct {
		Thread struct {
			ID string `json:"id"`
		} `json:"thread"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return "", apperrors.Wrap(err, "parseThreadResumeResult", "thread/resume decode")
	}
	if id := strings.TrimSpace(resp.Thread.ID); id != "" { return id, nil }
	if fallback != "" { return fallback, nil }
	return "", apperrors.New("parseThreadResumeResult", "thread/resume returned empty thread ID")
}

func rpcErrorContains(err error, requireAll bool, parts ...string) bool {
	if err == nil { return false }
	text := strings.ToLower(strings.TrimSpace(err.Error()))
	if requireAll {
		for _, part := range parts {
			if !strings.Contains(text, strings.ToLower(strings.TrimSpace(part))) {
				return false
			}
		}
		return true
	}
	for _, part := range parts {
		if strings.Contains(text, strings.ToLower(strings.TrimSpace(part))) {
			return true
		}
	}
	return false
}

func isMethodNotFoundRPCError(err error) bool {
	return rpcErrorContains(err, false, "method not found", "code -32601")
}

func isInvalidParamsRPCError(err error) bool {
	return rpcErrorContains(err, false, "invalid params", "code -32602")
}

func isRPCTimeoutError(err error) bool {
	if err == nil { return false }
	text := strings.ToLower(strings.TrimSpace(err.Error()))
	return strings.Contains(text, " timeout") || strings.HasSuffix(text, "timeout")
}

func buildTurnInterruptParams(threadID, turnID, turnScope string) map[string]any {
	interruptTurnID := strings.TrimSpace(turnID)
	if strings.EqualFold(strings.TrimSpace(turnScope), "thread_scoped") {
		interruptTurnID = ""
	}
	return map[string]any{
		"threadId": strings.TrimSpace(threadID),
		"turnId":   interruptTurnID,
	}
}

func isInterruptTurnIDMismatchError(err error) bool {
	return rpcErrorContains(err, false, "turn not found", "unknown turn", "invalid turn") ||
		rpcErrorContains(err, true, "turn id", "mismatch") ||
		rpcErrorContains(err, true, "turn_id", "mismatch")
}
