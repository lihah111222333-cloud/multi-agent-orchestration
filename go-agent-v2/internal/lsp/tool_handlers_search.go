package lsp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/multi-agent/go-agent-v2/pkg/logger"
)

const (
	lspSearchTimeout     = 15 * time.Second
	lspSearchResultLimit = 50
	lspSearchSnippetMax  = 500
	lspSearchPayloadMax  = 16 * 1024
)

var (
	lspSearchLookPath       = exec.LookPath
	lspSearchCommandContext = exec.CommandContext
	lspSearchGetwd          = os.Getwd
)

var lspSearchExcludeDirs = []string{".git", "node_modules", "vendor", "dist"}

type lspTextSearchParam struct {
	Query         string `json:"query"`
	Path          string `json:"path"`
	Glob          string `json:"glob"`
	CaseSensitive bool   `json:"case_sensitive"`
	MaxResults    int    `json:"max_results"`
}

type lspASTSearchParam struct {
	Pattern    string `json:"pattern"`
	Language   string `json:"language"`
	Path       string `json:"path"`
	MaxResults int    `json:"max_results"`
}

type lspSearchMatch struct {
	Path   string `json:"path"`
	Line   int    `json:"line"`
	Column int    `json:"column,omitempty"`
	Text   string `json:"text"`
}

// TextSearch performs text search via ripgrep.
func (h *ToolHandlers) TextSearch(args json.RawMessage) string {
	call := startLSPToolCallFromArgs("lsp_text_search", args)
	params, err := decodeArgs[lspTextSearchParam](args)
	if err != nil {
		call.fail(err, "stage", "decode")
		return toolError(err)
	}
	params.Query = strings.TrimSpace(params.Query)
	if params.Query == "" {
		err := errors.New("query is required")
		call.fail(err, "stage", "validate")
		return toolError(err)
	}

	workspaceRoot, target, err := resolveSearchTarget(params.Path)
	if err != nil {
		call.fail(err, "stage", "validate")
		return toolError(err)
	}
	limit := normalizeSearchLimit(params.MaxResults)

	binaryPath, err := lspSearchLookPath("rg")
	if err != nil {
		err = errors.New("rg not found in PATH")
		call.fail(err,
			logger.FieldPath, target,
			"query_len", len(params.Query),
			"stage", "dependency",
		)
		return toolError(err)
	}

	cmdArgs := []string{"--vimgrep", "--no-heading", "--color", "never", "--max-count", strconv.Itoa(limit)}
	if params.CaseSensitive {
		cmdArgs = append(cmdArgs, "--case-sensitive")
	} else {
		cmdArgs = append(cmdArgs, "--ignore-case")
	}
	for _, excluded := range lspSearchExcludeDirs {
		cmdArgs = append(cmdArgs, "--glob", "!"+excluded+"/**")
	}
	if glob := strings.TrimSpace(params.Glob); glob != "" {
		cmdArgs = append(cmdArgs, "--glob", glob)
	}
	cmdArgs = append(cmdArgs, params.Query, target)

	output, err := runSearchCommand(binaryPath, cmdArgs, workspaceRoot)
	if err != nil {
		call.fail(err,
			logger.FieldPath, target,
			"query_len", len(params.Query),
			"stage", "execute",
		)
		return toolError(err)
	}

	matches := parseRipgrepVimgrepOutput(output, workspaceRoot, limit)
	matches = filterAndCapSearchMatches(matches)
	if len(matches) == 0 {
		call.done(
			logger.FieldPath, target,
			"query_len", len(params.Query),
			"result_count", 0,
			"result_empty", true,
		)
		return "no matches found"
	}

	data, err := json.Marshal(matches)
	if err != nil {
		call.fail(err, "stage", "marshal")
		return toolError(err)
	}
	call.done(
		logger.FieldPath, target,
		"query_len", len(params.Query),
		"result_count", len(matches),
		"result_empty", false,
	)
	return string(data)
}

// AstSearch performs AST pattern search via ast-grep.
func (h *ToolHandlers) AstSearch(args json.RawMessage) string {
	call := startLSPToolCallFromArgs("lsp_ast_search", args)
	params, err := decodeArgs[lspASTSearchParam](args)
	if err != nil {
		call.fail(err, "stage", "decode")
		return toolError(err)
	}
	params.Pattern = strings.TrimSpace(params.Pattern)
	params.Language = strings.TrimSpace(params.Language)
	if params.Pattern == "" {
		err := errors.New("pattern is required")
		call.fail(err, "stage", "validate")
		return toolError(err)
	}
	if params.Language == "" {
		err := errors.New("language is required")
		call.fail(err, "stage", "validate")
		return toolError(err)
	}

	workspaceRoot, target, err := resolveSearchTarget(params.Path)
	if err != nil {
		call.fail(err, "stage", "validate")
		return toolError(err)
	}
	limit := normalizeSearchLimit(params.MaxResults)

	binaryPath, err := lspSearchLookPath("sg")
	if err != nil {
		err = errors.New("sg not found in PATH")
		call.fail(err,
			logger.FieldPath, target,
			logger.FieldLanguage, params.Language,
			"pattern_len", len(params.Pattern),
			"stage", "dependency",
		)
		return toolError(err)
	}

	cmdArgs := []string{"scan", "--json=stream", "-p", params.Pattern, "-l", params.Language, target}
	output, err := runSearchCommand(binaryPath, cmdArgs, workspaceRoot)
	if err != nil {
		call.fail(err,
			logger.FieldPath, target,
			logger.FieldLanguage, params.Language,
			"pattern_len", len(params.Pattern),
			"stage", "execute",
		)
		return toolError(err)
	}

	matches := parseASTGrepOutput(output, workspaceRoot, limit)
	matches = filterAndCapSearchMatches(matches)
	if len(matches) == 0 {
		call.done(
			logger.FieldPath, target,
			logger.FieldLanguage, params.Language,
			"pattern_len", len(params.Pattern),
			"result_count", 0,
			"result_empty", true,
		)
		return "no matches found"
	}

	data, err := json.Marshal(matches)
	if err != nil {
		call.fail(err, "stage", "marshal")
		return toolError(err)
	}
	call.done(
		logger.FieldPath, target,
		logger.FieldLanguage, params.Language,
		"pattern_len", len(params.Pattern),
		"result_count", len(matches),
		"result_empty", false,
	)
	return string(data)
}

func normalizeSearchLimit(v int) int {
	if v <= 0 {
		return lspSearchResultLimit
	}
	if v > lspSearchResultLimit {
		return lspSearchResultLimit
	}
	return v
}

func resolveSearchTarget(pathArg string) (workspaceRoot string, target string, err error) {
	workspaceRoot, err = searchWorkspaceRoot()
	if err != nil {
		return "", "", err
	}
	target = workspaceRoot
	trimmed := strings.TrimSpace(pathArg)
	if trimmed == "" {
		return workspaceRoot, target, nil
	}

	candidate := trimmed
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(workspaceRoot, candidate)
	}
	absCandidate, err := filepath.Abs(candidate)
	if err != nil {
		return "", "", fmt.Errorf("resolve search path: %w", err)
	}
	resolvedCandidate, err := filepath.EvalSymlinks(absCandidate)
	if err != nil {
		return "", "", fmt.Errorf("path not found: %s", trimmed)
	}
	if !isWithinRoot(workspaceRoot, resolvedCandidate) {
		return "", "", fmt.Errorf("path out of workspace root: %s", trimmed)
	}
	return workspaceRoot, resolvedCandidate, nil
}

func searchWorkspaceRoot() (string, error) {
	wd, err := lspSearchGetwd()
	if err != nil {
		return "", fmt.Errorf("resolve workspace root: %w", err)
	}
	absWD, err := filepath.Abs(wd)
	if err != nil {
		return "", fmt.Errorf("resolve workspace root: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(absWD)
	if err == nil {
		return filepath.Clean(resolved), nil
	}
	return filepath.Clean(absWD), nil
}

func isWithinRoot(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	if rel == ".." {
		return false
	}
	return !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func runSearchCommand(binaryPath string, args []string, workDir string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), lspSearchTimeout)
	defer cancel()

	cmd := lspSearchCommandContext(ctx, binaryPath, args...)
	cmd.Dir = workDir
	output, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return nil, fmt.Errorf("search timed out after %s", lspSearchTimeout)
	}
	if err == nil {
		return output, nil
	}

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		return nil, nil
	}

	msg := strings.TrimSpace(string(output))
	if msg == "" {
		msg = err.Error()
	}
	if len(msg) > 500 {
		msg = msg[:500]
	}
	return nil, errors.New(msg)
}

func parseRipgrepVimgrepOutput(output []byte, workspaceRoot string, limit int) []lspSearchMatch {
	if len(output) == 0 {
		return nil
	}
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(lines) == 0 {
		return nil
	}
	matches := make([]lspSearchMatch, 0, lspSearchMinInt(limit, len(lines)))
	for _, raw := range lines {
		if len(matches) >= limit {
			break
		}
		match, ok := parseRipgrepVimgrepLine(raw, workspaceRoot)
		if !ok {
			continue
		}
		matches = append(matches, match)
	}
	return matches
}

func parseRipgrepVimgrepLine(raw, workspaceRoot string) (lspSearchMatch, bool) {
	parts := strings.SplitN(raw, ":", 4)
	if len(parts) < 4 {
		return lspSearchMatch{}, false
	}
	line, err := strconv.Atoi(parts[1])
	if err != nil {
		return lspSearchMatch{}, false
	}
	column, err := strconv.Atoi(parts[2])
	if err != nil {
		return lspSearchMatch{}, false
	}
	path := strings.TrimSpace(parts[0])
	if path == "" {
		return lspSearchMatch{}, false
	}
	absPath := path
	if !filepath.IsAbs(absPath) {
		absPath = filepath.Join(workspaceRoot, path)
	}
	return lspSearchMatch{
		Path:   displayPath(workspaceRoot, absPath),
		Line:   line,
		Column: column,
		Text:   truncateSearchText(parts[3]),
	}, true
}

func parseASTGrepOutput(output []byte, workspaceRoot string, limit int) []lspSearchMatch {
	if len(output) == 0 {
		return nil
	}
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(lines) == 0 {
		return nil
	}
	matches := make([]lspSearchMatch, 0, lspSearchMinInt(limit, len(lines)))
	for _, raw := range lines {
		if len(matches) >= limit {
			break
		}
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			continue
		}
		match, ok := parseASTGrepLine(trimmed, workspaceRoot)
		if !ok {
			match = lspSearchMatch{Path: ".", Text: truncateSearchText(trimmed)}
		}
		if isExcludedPath(match.Path) {
			continue
		}
		matches = append(matches, match)
	}
	return matches
}

func parseASTGrepLine(raw, workspaceRoot string) (lspSearchMatch, bool) {
	var payload map[string]any
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return lspSearchMatch{}, false
	}
	path := firstString(payload, "file", "path", "filename")
	if path == "" {
		return lspSearchMatch{}, false
	}
	line := 0
	column := 0
	if rangeValue, ok := payload["range"].(map[string]any); ok {
		if startValue, ok := rangeValue["start"].(map[string]any); ok {
			line = toInt(startValue["line"]) + 1
			column = toInt(startValue["column"]) + 1
		}
	}
	text := firstString(payload, "text", "match", "matched", "snippet")
	if text == "" {
		text = raw
	}

	absPath := path
	if !filepath.IsAbs(absPath) {
		absPath = filepath.Join(workspaceRoot, absPath)
	}
	return lspSearchMatch{
		Path:   displayPath(workspaceRoot, absPath),
		Line:   line,
		Column: column,
		Text:   truncateSearchText(text),
	}, true
}

func filterAndCapSearchMatches(matches []lspSearchMatch) []lspSearchMatch {
	if len(matches) == 0 {
		return nil
	}
	filtered := make([]lspSearchMatch, 0, len(matches))
	for _, match := range matches {
		if isExcludedPath(match.Path) {
			continue
		}
		filtered = append(filtered, match)
	}
	for len(filtered) > 0 {
		data, err := json.Marshal(filtered)
		if err == nil && len(data) <= lspSearchPayloadMax {
			return filtered
		}
		filtered = filtered[:len(filtered)-1]
	}
	return nil
}

func isExcludedPath(path string) bool {
	slashPath := filepath.ToSlash(strings.ToLower(strings.TrimSpace(path)))
	if slashPath == "" {
		return false
	}
	for _, excluded := range lspSearchExcludeDirs {
		token := "/" + strings.ToLower(excluded) + "/"
		if strings.Contains("/"+slashPath+"/", token) {
			return true
		}
	}
	return false
}

func displayPath(workspaceRoot, absPath string) string {
	clean := filepath.Clean(absPath)
	if rel, err := filepath.Rel(workspaceRoot, clean); err == nil {
		if rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return filepath.ToSlash(rel)
		}
	}
	return filepath.ToSlash(clean)
}

func truncateSearchText(text string) string {
	trimmed := strings.TrimSpace(text)
	runes := []rune(trimmed)
	if len(runes) <= lspSearchSnippetMax {
		return trimmed
	}
	return string(runes[:lspSearchSnippetMax])
}

func firstString(payload map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := payload[key].(string); ok {
			trimmed := strings.TrimSpace(value)
			if trimmed != "" {
				return trimmed
			}
		}
	}
	return ""
}

func toInt(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int8:
		return int(typed)
	case int16:
		return int(typed)
	case int32:
		return int(typed)
	case int64:
		return int(typed)
	case uint:
		return int(typed)
	case uint8:
		return int(typed)
	case uint16:
		return int(typed)
	case uint32:
		return int(typed)
	case uint64:
		return int(typed)
	case float32:
		return int(typed)
	case float64:
		return int(typed)
	default:
		return 0
	}
}

func lspSearchMinInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
