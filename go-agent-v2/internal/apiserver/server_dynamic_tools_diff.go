package apiserver

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/multi-agent/go-agent-v2/internal/agentcore"
	"github.com/multi-agent/go-agent-v2/internal/uistate"
	"github.com/multi-agent/go-agent-v2/pkg/logger"
	"github.com/pmezard/go-difflib/difflib"
)

type fileContentSnapshot struct {
	exists  bool
	content string
}

type dynamicToolDiffTracker struct {
	enabled             bool
	repoRoot            string
	beforeFileSnapshots map[string]fileContentSnapshot
}

func beginDynamicToolDiffTracker(s *Server, agentID, tool string, args map[string]any) dynamicToolDiffTracker {
	if !shouldCaptureDynamicToolDiff(tool, args) {
		return dynamicToolDiffTracker{}
	}
	repoRoot := resolveDynamicToolDiffRepoRoot(s, agentID, args)
	if repoRoot == "" {
		return dynamicToolDiffTracker{}
	}
	paths, err := listRepoDirtyPaths(repoRoot)
	if err != nil {
		logger.Debug("dynamic-tool: capture pre-dispatch dirty paths failed",
			logger.FieldAgentID, agentID,
			logger.FieldToolName, tool,
			logger.FieldPath, repoRoot,
			logger.FieldError, err,
		)
		return dynamicToolDiffTracker{}
	}
	return dynamicToolDiffTracker{
		enabled:             true,
		repoRoot:            repoRoot,
		beforeFileSnapshots: captureWorkingTreeFileSnapshots(repoRoot, paths),
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
	afterPaths, err := listRepoDirtyPaths(tracker.repoRoot)
	if err != nil {
		logger.Debug("dynamic-tool: capture post-dispatch dirty paths failed",
			logger.FieldThreadID, threadID,
			logger.FieldToolName, tool,
			logger.FieldPath, tracker.repoRoot,
			logger.FieldError, err,
		)
		return
	}
	incrementalDiff, err := buildIncrementalDiffText(tracker.repoRoot, tracker.beforeFileSnapshots, afterPaths)
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
		"before_paths", len(tracker.beforeFileSnapshots),
		"new_len", len(incrementalDiff),
	)
}

func shouldCaptureDynamicToolDiff(tool string, args map[string]any) bool {
	switch normalizeDynamicToolName(tool) {
	case "lsp_file", "lsp_edit":
		action := strings.ToLower(strings.TrimSpace(extractStringArg(args, "action")))
		if action != "did_change" {
			return false
		}
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
	candidates := make([]string, 0, 16)
	addCandidate := func(path string) {
		path = strings.TrimSpace(path)
		if path == "" {
			return
		}
		candidates = append(candidates, path, filepath.Dir(path))
		if baseDir != "" && !filepath.IsAbs(path) {
			absPath := filepath.Join(baseDir, path)
			candidates = append(candidates, absPath, filepath.Dir(absPath))
		}
	}

	for _, key := range []string{"work_dir", "workdir", "cwd", "file_path", "path", "file"} {
		addCandidate(extractStringArg(args, key))
	}
	if baseDir != "" {
		candidates = append(candidates, baseDir)
	}

	seen := make(map[string]struct{}, len(candidates))
	for _, candidate := range candidates {
		normalized := filepath.Clean(strings.TrimSpace(candidate))
		if normalized == "" {
			continue
		}
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

func gitRepoRootFromPath(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", errors.New("git path is empty")
	}
	output, err := runGit(path, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", err
	}
	repoRoot := strings.TrimSpace(output)
	if repoRoot == "" {
		return "", errors.New("git repo root is empty")
	}
	return repoRoot, nil
}

func listRepoDirtyPaths(repoRoot string) ([]string, error) {
	trackedOutput, err := runGit(repoRoot, "diff", "--name-only", "HEAD", "--", ".")
	if err != nil {
		return nil, err
	}
	untrackedOutput, err := runGit(repoRoot, "ls-files", "--others", "--exclude-standard", "--")
	if err != nil {
		return nil, err
	}
	paths := append(collectPathLines(trackedOutput), collectPathLines(untrackedOutput)...)
	return uniqueSortedPaths(paths), nil
}

func collectPathLines(output string) []string {
	trimmed := strings.TrimSpace(output)
	if trimmed == "" {
		return nil
	}
	lines := strings.Split(trimmed, "\n")
	paths := make([]string, 0, len(lines))
	for _, line := range lines {
		path := strings.TrimSpace(line)
		if path == "" {
			continue
		}
		if len(path) >= 2 && strings.HasPrefix(path, "\"") && strings.HasSuffix(path, "\"") {
			decoded, err := strconv.Unquote(path)
			if err == nil {
				path = decoded
			}
		}
		path = strings.TrimPrefix(path, "a/")
		path = strings.TrimPrefix(path, "b/")
		path = filepath.ToSlash(filepath.Clean(path))
		if path == "" || path == "." {
			continue
		}
		paths = append(paths, path)
	}
	return paths
}

func uniqueSortedPaths(paths []string) []string {
	if len(paths) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(paths))
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		clean := filepath.ToSlash(strings.TrimSpace(path))
		if clean == "" || clean == "." {
			continue
		}
		if _, ok := seen[clean]; ok {
			continue
		}
		seen[clean] = struct{}{}
		out = append(out, clean)
	}
	sort.Strings(out)
	return out
}

func captureWorkingTreeFileSnapshots(repoRoot string, paths []string) map[string]fileContentSnapshot {
	snapshots := make(map[string]fileContentSnapshot, len(paths))
	for _, path := range uniqueSortedPaths(paths) {
		snapshots[path] = readWorkingTreeFileSnapshot(repoRoot, path)
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
		return fileContentSnapshot{}
	}
	return fileContentSnapshot{exists: true, content: string(content)}
}

func readHeadFileSnapshot(repoRoot, relativePath string) fileContentSnapshot {
	path := filepath.ToSlash(strings.TrimSpace(relativePath))
	if path == "" {
		return fileContentSnapshot{}
	}
	output, err := runGit(repoRoot, "show", "HEAD:"+path)
	if err != nil {
		return fileContentSnapshot{}
	}
	return fileContentSnapshot{exists: true, content: output}
}

func buildIncrementalDiffText(
	repoRoot string,
	beforeFileSnapshots map[string]fileContentSnapshot,
	afterPaths []string,
) (string, error) {
	paths := uniqueSortedPaths(afterPaths)
	if len(paths) == 0 {
		return "", nil
	}
	blocks := make([]string, 0, len(paths))
	for _, path := range paths {
		beforeSnapshot, ok := beforeFileSnapshots[path]
		if !ok {
			beforeSnapshot = readHeadFileSnapshot(repoRoot, path)
		}
		afterSnapshot := readWorkingTreeFileSnapshot(repoRoot, path)
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

func buildUnifiedDiffBlock(path string, before, after fileContentSnapshot) (string, error) {
	if before.exists == after.exists && before.content == after.content {
		return "", nil
	}
	labelPath := filepath.ToSlash(strings.TrimSpace(path))
	if labelPath == "" {
		labelPath = "unknown"
	}
	fromPath := "a/" + labelPath
	if !before.exists {
		fromPath = "/dev/null"
	}
	toPath := "b/" + labelPath
	if !after.exists {
		toPath = "/dev/null"
	}
	patchText, err := difflib.GetUnifiedDiffString(difflib.UnifiedDiff{
		A:        difflib.SplitLines(before.content),
		B:        difflib.SplitLines(after.content),
		FromFile: fromPath,
		ToFile:   toPath,
		Context:  3,
	})
	if err != nil {
		return "", err
	}
	patchText = strings.TrimSpace(patchText)
	if patchText == "" {
		return "", nil
	}
	return "diff --git a/" + labelPath + " b/" + labelPath + "\n" + patchText, nil
}

func runGit(repoRoot string, args ...string) (string, error) {
	cmdArgs := make([]string, 0, len(args)+2)
	if strings.TrimSpace(repoRoot) != "" {
		cmdArgs = append(cmdArgs, "-C", repoRoot)
	}
	cmdArgs = append(cmdArgs, args...)
	cmd := exec.Command("git", cmdArgs...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", err
	}
	return string(output), nil
}
