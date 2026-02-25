// server_dynamic_tools.go — LSP 动态工具: 注册、构建、调用 & 回传。
package apiserver

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/multi-agent/go-agent-v2/internal/agentcore"
	"github.com/multi-agent/go-agent-v2/internal/lsp"
	"github.com/multi-agent/go-agent-v2/internal/tooladapter"
	"github.com/multi-agent/go-agent-v2/internal/uistate"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
)

func toolAdapterProviders(s *Server) tooladapter.Providers {
	return tooladapter.Providers{
		LSP:            s.lspTools,
		CodeRun:        codeRunProvider{s: s},
		Approvals:      approvalProvider{s: s},
		Resource:       resourceProvider{s: s},
		Orchestration:  orchestrationProvider{s: s},
		AgentRuntime:   agentRuntimeProvider{s: s},
		Schema:         schemaProvider{s: s},
		Lookup:         runtimeLookupProvider{s: s},
		Counter:        toolCallCounterProvider{s: s},
		CodeRunTracker: codeRunTrackerProvider{s: s},
	}
}

// registerDynamicTools 注册所有动态工具处理函数。
func registerDynamicTools(s *Server) {
	if s == nil {
		return
	}
	if s.dynTools == nil {
		s.dynTools = make(map[string]tooladapter.RuntimeToolHandler)
	} else {
		for name := range s.dynTools {
			delete(s.dynTools, name)
		}
	}
	tooladapter.Register(runtimeRegistryProvider{s: s}, toolAdapterProviders(s))
}

// SetupLSP 初始化 LSP 事件转发: 诊断缓存 + 广播。
func SetupLSP(s *Server, rootDir string) {
	if s == nil {
		return
	}
	if s.lsp == nil {
		return
	}
	if rootDir != "" {
		s.lsp.SetRootURI("file://" + rootDir)
	}
	s.lsp.SetDiagnosticHandler(func(uri string, diagnostics []lsp.Diagnostic) {
		setDiagnostics(s, uri, diagnostics)

		// 广播诊断通知给前端
		items := make([]map[string]any, 0, len(diagnostics))
		for _, d := range diagnostics {
			items = append(items, map[string]any{
				"message":  d.Message,
				"severity": d.Severity.String(),
				"line":     d.Range.Start.Line,
				"column":   d.Range.Start.Character,
			})
		}
		notify(s, "lsp/diagnostics/published", map[string]any{
			"uri":         uri,
			"diagnostics": items,
		})
	})
}

func resolveDynamicToolThreadIDs(agentID, rawThreadID string) (threadID, codexThreadID string) {
	agentThreadID := strings.TrimSpace(agentID)
	codexThreadID = strings.TrimSpace(rawThreadID)
	if agentThreadID != "" {
		threadID = agentThreadID
	} else {
		threadID = codexThreadID
	}
	if codexThreadID == "" || codexThreadID == threadID {
		codexThreadID = ""
	}
	return threadID, codexThreadID
}

// handleDynamicToolCall 处理 codex 发回的动态工具调用 — 调 LSP 并回传结果。
func handleDynamicToolCall(s *Server, agentID string, event agentcore.Event) {
	if s == nil {
		return
	}
	// 心跳: 防止 stall 检测在等待 tool 执行期间误杀。
	stopHeartbeat := s.codexAdapter.StartDynamicToolStallHeartbeat(agentID)
	defer stopHeartbeat()

	// 先查找 proc — 后续的所有错误路径都需要通过 codexAdapter 回传错误。
	proc := s.mgr.Get(agentID)
	if proc == nil {
		logger.Error("app-server: dynamic_tool_call dropped — agent gone",
			logger.FieldAgentID, agentID)
		if event.RespondFunc != nil {
			if respondErr := event.RespondFunc(-32603, "agent not found: "+agentID); respondErr != nil {
				logger.Warn("app-server: RespondFunc failed on agent-gone",
					logger.FieldAgentID, agentID, logger.FieldError, respondErr)
			}
		}
		return
	}

	// codex 事件信封: {"id": "...", "msg": {DynamicToolCallParams}, "conversationId": "..."}
	// 先提取 msg 字段, 再解析工具调用参数。
	var envelope struct {
		Msg json.RawMessage `json:"msg"`
	}
	raw := event.Data
	if err := json.Unmarshal(raw, &envelope); err == nil && len(envelope.Msg) > 0 {
		raw = envelope.Msg
	}

	var call agentcore.DynamicToolCallData
	if err := json.Unmarshal(raw, &call); err != nil {
		logger.Warn("app-server: bad dynamic_tool_call data", logger.FieldAgentID, agentID, logger.FieldError, err,
			"raw", string(event.Data))
		// 必须回复 error response，否则 codex turn 永挂。
		if event.RespondFunc != nil {
			if respErr := event.RespondFunc(-32602, "bad dynamic_tool_call data: "+err.Error()); respErr != nil {
				logger.Warn("app-server: respond error failed", logger.FieldAgentID, agentID, logger.FieldError, respErr)
			}
		} else if event.RequestID != nil {
			if respErr := s.codexAdapter.RespondError(proc, *event.RequestID, -32602, "bad dynamic_tool_call data: "+err.Error()); respErr != nil {
				logger.Warn("app-server: respond error failed", logger.FieldAgentID, agentID, logger.FieldError, respErr)
			}
		}
		return
	}
	threadID, codexThreadID := resolveDynamicToolThreadIDs(agentID, call.ThreadID)

	var argMap map[string]any
	if len(call.Arguments) > 0 {
		if err := json.Unmarshal(call.Arguments, &argMap); err != nil {
			logger.Debug("app-server: unmarshal tool arguments", logger.FieldToolName, call.Tool, logger.FieldError, err)
		}
	}
	filePath := extractToolFilePath(argMap)
	diffTracker := beginDynamicToolDiffTracker(s, agentID, call.Tool, argMap)

	start := time.Now()
	var totalCalls int64
	result, dispatchErr := tooladapter.Dispatch(tooladapter.DynamicToolCall{
		AgentID:    agentID,
		Tool:       call.Tool,
		CallID:     call.CallID,
		RequestID:  event.RequestID,
		Arguments:  call.Arguments,
		TotalCalls: &totalCalls,
	}, toolAdapterProviders(s))
	if dispatchErr != nil {
		result = dispatchErr.Error()
	}

	elapsed := time.Since(start)
	success := toolResultSuccess(result)

	logger.Info("dynamic-tool: completed",
		logger.FieldSource, "codex",
		logger.FieldComponent, "tool_call",
		logger.FieldAgentID, agentID,
		logger.FieldToolName, call.Tool,
		logger.FieldDurationMS, elapsed.Milliseconds(),
		logger.FieldEventType, "dynamic_tool_call",
		"result_len", len(result),
		"success", success,
	)

	// 递增活动统计 (lsp_ 前缀工具会自动累加到 lspCalls)
	if s.uiRuntime != nil {
		s.uiRuntime.IncrActivityStat(threadID, "toolCall", call.Tool)
	}

	// 广播到前端 — 让 UI 可以显示 LSP 调用
	notifyPayload := buildToolNotifyPayload(threadID, agentID, call, argMap, filePath, success, totalCalls, elapsed, result)
	if codexThreadID != "" {
		notifyPayload["codexThreadId"] = codexThreadID
	}
	notify(s, "dynamic-tool/called", notifyPayload)

	if success {
		maybeEmitDynamicToolDiffUpdate(s, threadID, codexThreadID, call.Tool, diffTracker)
	}

	// 回传结果: 优先使用 event 绑定的响应回调，兼容 string/int 两种 JSON-RPC id。
	if event.RespondResultFunc != nil {
		if err := event.RespondResultFunc(dynamicToolCallResultPayload(result)); err != nil {
			logger.Warn("app-server: send tool result failed", logger.FieldAgentID, agentID, logger.FieldToolName, call.Tool, logger.FieldError, err)
		}
		return
	}
	if err := s.codexAdapter.SendDynamicToolResult(proc, call.CallID, result, event.RequestID); err != nil {
		logger.Warn("app-server: send tool result failed", logger.FieldAgentID, agentID, logger.FieldToolName, call.Tool, logger.FieldError, err)
	}
}

func extractToolFilePath(args map[string]any) string {
	if args == nil {
		return ""
	}
	for _, key := range []string{"file_path", "path", "file"} {
		value, ok := args[key].(string)
		if !ok {
			continue
		}
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func buildToolNotifyPayload(
	threadID string,
	agentID string,
	call agentcore.DynamicToolCallData,
	argMap map[string]any,
	filePath string,
	success bool,
	count int64,
	elapsed time.Duration,
	result string,
) map[string]any {
	payload := map[string]any{
		"threadId":   threadID,
		"agent":      agentID,
		"tool":       call.Tool,
		"callId":     call.CallID,
		"arguments":  argMap,
		"file":       filePath,
		"success":    success,
		"totalCalls": count,
		"elapsedMs":  elapsed.Milliseconds(),
		"resultLen":  len(result),
	}
	if result == "" {
		return payload
	}
	if len(result) > 500 {
		payload["resultPreview"] = result[:500]
		return payload
	}
	payload["resultPreview"] = result
	return payload
}

type fileContentSnapshot struct {
	exists  bool
	content string
}

type dynamicToolDiffTracker struct {
	enabled             bool
	repoRoot            string
	beforeByFile        map[string]string
	beforeFileSnapshots map[string]fileContentSnapshot
	beforeTextLen       int
}

func beginDynamicToolDiffTracker(s *Server, agentID, tool string, args map[string]any) dynamicToolDiffTracker {
	if !shouldCaptureDynamicToolDiff(tool, args) {
		return dynamicToolDiffTracker{}
	}
	repoRoot := resolveDynamicToolDiffRepoRoot(s, agentID, args)
	if repoRoot == "" {
		return dynamicToolDiffTracker{}
	}
	beforeByFile, err := captureRepoDiffSnapshotByFile(repoRoot)
	if err != nil {
		logger.Debug("dynamic-tool: capture pre-dispatch diff failed",
			logger.FieldAgentID, agentID,
			logger.FieldToolName, tool,
			logger.FieldPath, repoRoot,
			logger.FieldError, err,
		)
		return dynamicToolDiffTracker{}
	}
	beforeSnapshots := captureWorkingTreeFileSnapshots(repoRoot, sortedDiffMapKeys(beforeByFile))
	return dynamicToolDiffTracker{
		enabled:             true,
		repoRoot:            repoRoot,
		beforeByFile:        beforeByFile,
		beforeFileSnapshots: beforeSnapshots,
		beforeTextLen:       len(joinDiffBlocksByPath(beforeByFile)),
	}
}

func maybeEmitDynamicToolDiffUpdate(s *Server, threadID, codexThreadID, tool string, tracker dynamicToolDiffTracker) {
	if s == nil || !tracker.enabled {
		return
	}
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return
	}
	postByFile, err := captureRepoDiffSnapshotByFile(tracker.repoRoot)
	if err != nil {
		logger.Debug("dynamic-tool: capture post-dispatch diff failed",
			logger.FieldThreadID, threadID,
			logger.FieldToolName, tool,
			logger.FieldPath, tracker.repoRoot,
			logger.FieldError, err,
		)
		return
	}
	incrementalDiff, err := buildIncrementalDiffText(tracker.repoRoot, tracker.beforeByFile, postByFile, tracker.beforeFileSnapshots)
	if err != nil {
		logger.Debug("dynamic-tool: build incremental diff failed",
			logger.FieldThreadID, threadID,
			logger.FieldToolName, tool,
			logger.FieldPath, tracker.repoRoot,
			logger.FieldError, err,
		)
		return
	}
	if incrementalDiff == "" {
		return
	}

	payload := map[string]any{
		"threadId": threadID,
		"diff":     incrementalDiff,
		"uiText":   incrementalDiff,
	}
	if codexThreadID != "" {
		payload["codexThreadId"] = codexThreadID
	}
	files := parseFilesFromPatchDelta(incrementalDiff)
	if len(files) > 0 {
		payload["files"] = files
		payload["file"] = files[0]
	}

	normalized := uistate.NormalizeEventFromPayload(agentcore.EventTurnDiff, "turn/diff/updated", payload)
	payload["uiType"] = string(normalized.UIType)
	if s.uiRuntime != nil {
		s.uiRuntime.ApplyAgentEvent(threadID, normalized, payload)
	}
	notify(s, "turn/diff/updated", payload)

	logger.Info("dynamic-tool: turn diff updated",
		logger.FieldThreadID, threadID,
		logger.FieldToolName, tool,
		"repo_root", tracker.repoRoot,
		"old_len", tracker.beforeTextLen,
		"new_len", len(incrementalDiff),
	)
}

func shouldCaptureDynamicToolDiff(tool string, args map[string]any) bool {
	switch normalizeDynamicToolName(tool) {
	case "lsp_did_change":
		return extractBoolArg(args, "persist_to_disk")
	case "code_run", "run":
		return true
	default:
		return false
	}
}

func normalizeDynamicToolName(tool string) string {
	normalized := strings.ToLower(strings.TrimSpace(tool))
	normalized = strings.TrimPrefix(normalized, "functions.")
	normalized = strings.TrimPrefix(normalized, "tools.")
	return normalized
}

func extractStringArg(args map[string]any, keys ...string) string {
	if args == nil {
		return ""
	}
	for _, key := range keys {
		value, ok := args[key].(string)
		if !ok {
			continue
		}
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func extractBoolArg(args map[string]any, keys ...string) bool {
	if args == nil {
		return false
	}
	for _, key := range keys {
		value, ok := args[key]
		if !ok {
			continue
		}
		switch typed := value.(type) {
		case bool:
			return typed
		case string:
			switch strings.ToLower(strings.TrimSpace(typed)) {
			case "true", "1", "yes", "y":
				return true
			}
		case float64:
			if typed != 0 {
				return true
			}
		case int:
			if typed != 0 {
				return true
			}
		}
	}
	return false
}

func resolveDynamicToolDiffRepoRoot(s *Server, agentID string, args map[string]any) string {
	baseDir := strings.TrimSpace(getAgentWorkDirState(s, agentID))
	candidates := make([]string, 0, 12)
	for _, key := range []string{"work_dir", "workdir", "cwd", "file_path", "path", "file"} {
		rawPath := extractStringArg(args, key)
		if rawPath == "" {
			continue
		}
		candidates = appendDynamicToolPathCandidates(candidates, rawPath, baseDir)
	}
	if baseDir != "" {
		candidates = append(candidates, baseDir)
	}

	seen := make(map[string]struct{}, len(candidates))
	for _, candidate := range candidates {
		normalized := strings.TrimSpace(candidate)
		if normalized == "" {
			continue
		}
		normalized = filepath.Clean(normalized)
		if _, exists := seen[normalized]; exists {
			continue
		}
		seen[normalized] = struct{}{}
		repoRoot, err := gitRepoRootFromPath(normalized)
		if err == nil && repoRoot != "" {
			return repoRoot
		}
	}
	return ""
}

func appendDynamicToolPathCandidates(target []string, rawPath, baseDir string) []string {
	path := strings.TrimSpace(rawPath)
	if path == "" {
		return target
	}
	target = append(target, path)
	target = append(target, filepath.Dir(path))

	base := strings.TrimSpace(baseDir)
	if base == "" || filepath.IsAbs(path) {
		return target
	}
	absolute := filepath.Join(base, path)
	target = append(target, absolute)
	target = append(target, filepath.Dir(absolute))
	return target
}

func gitRepoRootFromPath(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", errors.New("git path is empty")
	}
	output, err := runGitOutput(path, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", err
	}
	repoRoot := strings.TrimSpace(output)
	if repoRoot == "" {
		return "", errors.New("git repo root is empty")
	}
	return repoRoot, nil
}

func captureRepoDiffSnapshot(repoRoot string) (string, error) {
	byFile, err := captureRepoDiffSnapshotByFile(repoRoot)
	if err != nil {
		return "", err
	}
	return joinDiffBlocksByPath(byFile), nil
}

func captureRepoDiffSnapshotByFile(repoRoot string) (map[string]string, error) {
	repoRoot = strings.TrimSpace(repoRoot)
	if repoRoot == "" {
		return nil, errors.New("repo root is empty")
	}
	trackedDiff, err := runGitOutput(repoRoot, "diff", "--no-color", "--no-ext-diff", "--", ".")
	if err != nil {
		return nil, err
	}
	trackedByFile := splitUnifiedDiffByFile(trackedDiff)
	untrackedByFile, err := captureUntrackedDiffSnapshotByFile(repoRoot)
	if err != nil {
		return nil, err
	}
	merged := make(map[string]string, len(trackedByFile)+len(untrackedByFile))
	for path, block := range trackedByFile {
		trimmed := strings.TrimSpace(block)
		if trimmed == "" {
			continue
		}
		merged[path] = trimmed
	}
	for path, block := range untrackedByFile {
		trimmed := strings.TrimSpace(block)
		if trimmed == "" {
			continue
		}
		merged[path] = trimmed
	}
	return merged, nil
}

func captureUntrackedDiffSnapshotByFile(repoRoot string) (map[string]string, error) {
	output, err := runGitOutput(repoRoot, "ls-files", "--others", "--exclude-standard", "--")
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimSpace(output), "\n")
	diffsByFile := make(map[string]string, len(lines))
	for _, line := range lines {
		relPath := strings.TrimSpace(line)
		if relPath == "" {
			continue
		}
		diff, diffErr := runGitNoIndexDiff(repoRoot, relPath)
		if diffErr != nil {
			return nil, diffErr
		}
		diff = strings.TrimSpace(diff)
		if diff == "" {
			continue
		}
		parsed := splitUnifiedDiffByFile(diff)
		if len(parsed) == 0 {
			diffsByFile[filepath.ToSlash(relPath)] = diff
			continue
		}
		for file, block := range parsed {
			trimmed := strings.TrimSpace(block)
			if trimmed == "" {
				continue
			}
			diffsByFile[file] = trimmed
		}
	}
	return diffsByFile, nil
}

func splitUnifiedDiffByFile(diffText string) map[string]string {
	trimmed := strings.TrimSpace(diffText)
	if trimmed == "" {
		return map[string]string{}
	}
	lines := strings.Split(trimmed, "\n")
	byFile := make(map[string]string)
	currentFile := ""
	blockLines := make([]string, 0, 32)
	flushBlock := func() {
		if currentFile == "" || len(blockLines) == 0 {
			return
		}
		block := strings.TrimSpace(strings.Join(blockLines, "\n"))
		if block == "" {
			return
		}
		byFile[currentFile] = block
	}

	for _, line := range lines {
		if strings.HasPrefix(line, "diff --git ") {
			flushBlock()
			currentFile = parseDiffGitHeaderPath(line)
			blockLines = blockLines[:0]
			if currentFile != "" {
				blockLines = append(blockLines, line)
			}
			continue
		}
		if currentFile == "" {
			continue
		}
		blockLines = append(blockLines, line)
	}
	flushBlock()
	return byFile
}

func parseDiffGitHeaderPath(line string) string {
	const prefix = "diff --git "
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, prefix) {
		return ""
	}
	rest := strings.TrimSpace(strings.TrimPrefix(trimmed, prefix))
	left, rest, ok := parseGitDiffPathToken(rest)
	if !ok {
		return ""
	}
	right, _, ok := parseGitDiffPathToken(rest)
	if !ok {
		return ""
	}
	path := strings.TrimSpace(strings.TrimPrefix(right, "b/"))
	if path == "" || path == "/dev/null" || path == "dev/null" {
		path = strings.TrimSpace(strings.TrimPrefix(left, "a/"))
	}
	if path == "" || path == "/dev/null" || path == "dev/null" {
		return ""
	}
	return filepath.ToSlash(path)
}

func parseGitDiffPathToken(raw string) (token, rest string, ok bool) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", "", false
	}
	if value[0] != '"' {
		idx := strings.IndexByte(value, ' ')
		if idx < 0 {
			return value, "", true
		}
		return value[:idx], value[idx+1:], true
	}
	for i := 1; i < len(value); i++ {
		if value[i] != '"' {
			continue
		}
		if isEscapedQuote(value, i) {
			continue
		}
		rawToken := value[:i+1]
		decoded, err := strconv.Unquote(rawToken)
		if err != nil {
			decoded = strings.Trim(rawToken, "\"")
		}
		return decoded, value[i+1:], true
	}
	return "", "", false
}

func isEscapedQuote(text string, quotePos int) bool {
	if quotePos <= 0 || quotePos > len(text) {
		return false
	}
	count := 0
	for i := quotePos - 1; i >= 0; i-- {
		if text[i] != '\\' {
			break
		}
		count++
	}
	return count%2 == 1
}

func sortedDiffMapKeys(byFile map[string]string) []string {
	if len(byFile) == 0 {
		return nil
	}
	paths := make([]string, 0, len(byFile))
	for path, block := range byFile {
		if strings.TrimSpace(block) == "" {
			continue
		}
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths
}

func captureWorkingTreeFileSnapshots(repoRoot string, paths []string) map[string]fileContentSnapshot {
	snapshots := make(map[string]fileContentSnapshot, len(paths))
	for _, path := range paths {
		clean := filepath.ToSlash(strings.TrimSpace(path))
		if clean == "" {
			continue
		}
		snapshots[clean] = readWorkingTreeFileSnapshot(repoRoot, clean)
	}
	return snapshots
}

func readWorkingTreeFileSnapshot(repoRoot, relativePath string) fileContentSnapshot {
	clean := filepath.FromSlash(strings.TrimSpace(relativePath))
	if clean == "" {
		return fileContentSnapshot{}
	}
	absPath := filepath.Join(repoRoot, clean)
	content, err := os.ReadFile(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			return fileContentSnapshot{}
		}
		return fileContentSnapshot{}
	}
	return fileContentSnapshot{exists: true, content: string(content)}
}

func readHeadFileSnapshot(repoRoot, relativePath string) (fileContentSnapshot, error) {
	content, exists, err := readFileFromHead(repoRoot, relativePath)
	if err != nil {
		return fileContentSnapshot{}, err
	}
	if !exists {
		return fileContentSnapshot{}, nil
	}
	return fileContentSnapshot{exists: true, content: content}, nil
}

func readFileFromHead(repoRoot, relativePath string) (content string, exists bool, err error) {
	path := filepath.ToSlash(strings.TrimSpace(relativePath))
	if path == "" {
		return "", false, nil
	}
	cmd := exec.Command("git", "-C", repoRoot, "show", "HEAD:"+path)
	output, runErr := cmd.CombinedOutput()
	if runErr == nil {
		return string(output), true, nil
	}
	var exitErr *exec.ExitError
	if errors.As(runErr, &exitErr) {
		return "", false, nil
	}
	return "", false, runErr
}

func buildUnifiedDiffBlock(path string, before, after fileContentSnapshot) (string, error) {
	if before.exists == after.exists && before.content == after.content {
		return "", nil
	}
	tmpDir, err := os.MkdirTemp("", ".dyn-tool-diff-*")
	if err != nil {
		return "", err
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	beforePath := filepath.Join(tmpDir, "before")
	afterPath := filepath.Join(tmpDir, "after")
	if err := os.WriteFile(beforePath, []byte(before.content), 0o600); err != nil {
		return "", err
	}
	if err := os.WriteFile(afterPath, []byte(after.content), 0o600); err != nil {
		return "", err
	}
	block, err := runGitNoIndexDiffAgainstFiles(path, beforePath, afterPath)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(block), nil
}

func buildIncrementalDiffText(
	repoRoot string,
	beforeByFile map[string]string,
	afterByFile map[string]string,
	beforeFileSnapshots map[string]fileContentSnapshot,
) (string, error) {
	if len(afterByFile) == 0 {
		return "", nil
	}
	pathSet := make(map[string]struct{}, len(beforeByFile)+len(afterByFile))
	for path := range beforeByFile {
		pathSet[path] = struct{}{}
	}
	for path := range afterByFile {
		pathSet[path] = struct{}{}
	}

	paths := make([]string, 0, len(pathSet))
	for path := range pathSet {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	blocks := make([]string, 0, len(paths))
	for _, path := range paths {
		beforeBlock := strings.TrimSpace(beforeByFile[path])
		afterBlock := strings.TrimSpace(afterByFile[path])
		if beforeBlock == afterBlock {
			continue
		}
		if afterBlock == "" {
			continue
		}

		beforeSnapshot, ok := beforeFileSnapshots[path]
		if !ok {
			headSnapshot, err := readHeadFileSnapshot(repoRoot, path)
			if err != nil {
				return "", err
			}
			beforeSnapshot = headSnapshot
		}
		afterSnapshot := readWorkingTreeFileSnapshot(repoRoot, path)
		if !afterSnapshot.exists {
			continue
		}

		block, err := buildUnifiedDiffBlock(path, beforeSnapshot, afterSnapshot)
		if err != nil {
			return "", err
		}
		if strings.TrimSpace(block) == "" {
			continue
		}
		blocks = append(blocks, block)
	}
	return strings.Join(blocks, "\n"), nil
}

func joinDiffBlocksByPath(byFile map[string]string) string {
	if len(byFile) == 0 {
		return ""
	}
	paths := sortedDiffMapKeys(byFile)
	blocks := make([]string, 0, len(paths))
	for _, path := range paths {
		block := strings.TrimSpace(byFile[path])
		if block == "" {
			continue
		}
		blocks = append(blocks, block)
	}
	return strings.Join(blocks, "\n")
}

func runGitOutput(repoRoot string, args ...string) (string, error) {
	cmdArgs := make([]string, 0, 2+len(args))
	cmdArgs = append(cmdArgs, "-C", repoRoot)
	cmdArgs = append(cmdArgs, args...)
	cmd := exec.Command("git", cmdArgs...)
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(output), nil
}

func runGitNoIndexDiff(repoRoot, relPath string) (string, error) {
	cmd := exec.Command("git", "-C", repoRoot, "diff", "--no-color", "--no-ext-diff", "--no-index", "--", "/dev/null", relPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) || exitErr.ExitCode() != 1 {
			return "", err
		}
	}
	return string(output), nil
}

func runGitNoIndexDiffAgainstFiles(path, beforePath, afterPath string) (string, error) {
	labelPath := filepath.ToSlash(strings.TrimSpace(path))
	if labelPath == "" {
		labelPath = "unknown"
	}
	cmd := exec.Command(
		"git",
		"diff",
		"--no-color",
		"--no-ext-diff",
		"--no-index",
		"--label",
		"a/"+labelPath,
		"--label",
		"b/"+labelPath,
		"--",
		beforePath,
		afterPath,
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) || exitErr.ExitCode() != 1 {
			return "", err
		}
	}
	return string(output), nil
}

func dynamicToolCallResultPayload(output string) map[string]any {
	return map[string]any{
		"contentItems": []map[string]any{{
			"type": "inputText",
			"text": output,
		}},
		"success": true,
	}
}
