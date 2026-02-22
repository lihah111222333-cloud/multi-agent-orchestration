package lsp

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const (
	stressPlanDate             = "2026-02-22"
	stressPlanDefaultCorpusDir = "/Users/mima0000/Desktop/wj/e2e测试"
	stressPlanResultDir        = "test-results/lsp-stress/" + stressPlanDate
)

const (
	spToolOpen          = "lsp_open_file"
	spToolDocument      = "lsp_document_symbol"
	spToolHover         = "lsp_hover"
	spToolDiagnostics   = "lsp_diagnostics"
	spToolDefinition    = "lsp_definition"
	spToolReferences    = "lsp_references"
	spToolRename        = "lsp_rename"
	spToolCompletion    = "lsp_completion"
	spToolDidChange     = "lsp_did_change"
	spToolWorkspace     = "lsp_workspace_symbol"
	spToolImplement     = "lsp_implementation"
	spToolTypeDef       = "lsp_type_definition"
	spToolCallHierarchy = "lsp_call_hierarchy"
	spToolTypeHierarchy = "lsp_type_hierarchy"
	spToolCodeAction    = "lsp_code_action"
	spToolSignature     = "lsp_signature_help"
	spToolFormat        = "lsp_format_document"
	spToolSemantic      = "lsp_semantic_tokens"
	spToolFolding       = "lsp_folding_range"
)

var (
	stressPlanMarkerFiles = []string{"package.json", "go.mod", "Cargo.toml", "tsconfig.json"}
	stressPlanCore9Tools  = []string{spToolOpen, spToolDocument, spToolHover, spToolDiagnostics, spToolDefinition, spToolReferences, spToolRename, spToolCompletion, spToolDidChange}
)

type stressPlanProbeRecord struct {
	Language string `json:"language"`
	Source   string `json:"source"`
	Path     string `json:"path"`
	Valid    bool   `json:"valid"`
	Reason   string `json:"reason"`
}

type stressPlanToolResult struct {
	Stage      string `json:"stage"`
	Tool       string `json:"tool"`
	Language   string `json:"language"`
	File       string `json:"file,omitempty"`
	DurationMS int64  `json:"duration_ms"`
	Success    bool   `json:"success"`
	ErrorCode  string `json:"error_code,omitempty"`
	ErrorText  string `json:"error_text,omitempty"`
	Note       string `json:"note,omitempty"`
}

type stressPlanStat struct {
	Attempts    int     `json:"attempts"`
	Successes   int     `json:"successes"`
	SuccessRate float64 `json:"success_rate"`
}

type stressPlanStageSummary struct {
	Stage               string                               `json:"stage"`
	GeneratedAt         string                               `json:"generated_at"`
	GatePassed          bool                                 `json:"gate_passed"`
	Thresholds          map[string]any                       `json:"thresholds,omitempty"`
	ToolStats           map[string]stressPlanStat            `json:"tool_stats"`
	LanguageStats       map[string]stressPlanStat            `json:"language_stats"`
	ToolLanguageSuccess map[string]map[string]int            `json:"tool_language_success"`
	Results             []stressPlanToolResult               `json:"results"`
	Notes               []string                             `json:"notes,omitempty"`
	Extra               map[string]any                       `json:"extra,omitempty"`
	EmptyReasons        map[string]map[string]string         `json:"empty_reasons,omitempty"`
	LatencyP95MS        map[string]float64                   `json:"latency_p95_ms,omitempty"`
	ArtifactPaths       map[string]string                    `json:"artifact_paths,omitempty"`
	Checks              map[string]map[string]stressPlanStat `json:"checks,omitempty"`
}

type stressPlanDiagTracker struct {
	total  atomic.Int64
	mu     sync.Mutex
	byPath map[string]int
}

func stressPlanNewDiagTracker() *stressPlanDiagTracker {
	return &stressPlanDiagTracker{byPath: make(map[string]int)}
}

func (d *stressPlanDiagTracker) handler(uri string, diagnostics []Diagnostic) {
	d.total.Add(int64(len(diagnostics)))
	path := stressPlanURIToPath(uri)
	if path == "" {
		return
	}
	d.mu.Lock()
	d.byPath[stressPlanNormalizePath(path)] += len(diagnostics)
	d.mu.Unlock()
}

func (d *stressPlanDiagTracker) count(filePath string) int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.byPath[stressPlanNormalizePath(filePath)]
}

func (d *stressPlanDiagTracker) totalCount() int {
	return int(d.total.Load())
}

func stressPlanDefaultCorpusRoot() string {
	if v := strings.TrimSpace(os.Getenv("LSP_STRESS_CORPUS_DIR")); v != "" {
		return v
	}
	return stressPlanDefaultCorpusDir
}

func stressPlanRepoRoot() string {
	cwd, err := os.Getwd()
	if err != nil {
		return "."
	}
	dir := cwd
	for {
		if _, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return cwd
		}
		dir = parent
	}
}

func stressPlanOutputDir(t *testing.T) string {
	t.Helper()
	outDir := filepath.Join(stressPlanRepoRoot(), stressPlanResultDir)
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", outDir, err)
	}
	return outDir
}

func stressPlanWriteJSON(t *testing.T, outputPath string, v any) {
	t.Helper()
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatalf("marshal %s: %v", outputPath, err)
	}
	if err := os.WriteFile(outputPath, data, 0o644); err != nil {
		t.Fatalf("write %s: %v", outputPath, err)
	}
}

func stressPlanAppendReport(t *testing.T, stage string, gatePassed bool, conclusion string, unresolved []string, nextCond string, artifacts []string) {
	t.Helper()
	outDir := stressPlanOutputDir(t)
	reportPath := filepath.Join(outDir, "final-report.md")
	_, statErr := os.Stat(reportPath)

	var b strings.Builder
	if os.IsNotExist(statErr) {
		b.WriteString("# LSP Stress Final Report\n\n")
		b.WriteString("- Plan Date: `")
		b.WriteString(stressPlanDate)
		b.WriteString("`\n")
		b.WriteString("- Generated By: `internal/lsp/lsp_stress_plan_test.go`\n\n")
	}

	status := "FAIL"
	if gatePassed {
		status = "PASS"
	}
	b.WriteString("## ")
	b.WriteString(stage)
	b.WriteString(" (")
	b.WriteString(time.Now().Format(time.RFC3339))
	b.WriteString(")\n\n")
	b.WriteString("- Stage Gate: `")
	b.WriteString(status)
	b.WriteString("`\n")
	b.WriteString("- 结论: ")
	b.WriteString(conclusion)
	b.WriteString("\n")

	if len(artifacts) > 0 {
		for _, artifact := range artifacts {
			b.WriteString("- 产物: `")
			b.WriteString(artifact)
			b.WriteString("`\n")
		}
	}

	if len(unresolved) == 0 {
		b.WriteString("- 未解决问题: 无\n")
	} else {
		for _, item := range unresolved {
			b.WriteString("- 未解决问题: ")
			b.WriteString(item)
			b.WriteString("\n")
		}
	}
	b.WriteString("- 下一阶段入口条件: ")
	b.WriteString(nextCond)
	b.WriteString("\n\n")

	f, err := os.OpenFile(reportPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		t.Fatalf("open report: %v", err)
	}
	defer f.Close()
	if _, err := f.WriteString(b.String()); err != nil {
		t.Fatalf("append report: %v", err)
	}
}

func stressPlanDiscoverAppDirs(corpusRoot string) (map[string]string, []stressPlanProbeRecord, error) {
	languages := []string{"go", "typescript", "rust"}
	corpusSub := map[string]string{"go": "go", "typescript": "js", "rust": "rust"}
	repoRoot := stressPlanRepoRoot()
	repoCandidates := []string{
		filepath.Join("cmd", "agent-terminal", "frontend", "vue-app"),
		filepath.Join("cmd", "agent-terminal", "frontend"),
	}
	envPath := strings.TrimSpace(os.Getenv("LSP_STRESS_APP_DIR"))

	resolved := make(map[string]string, len(languages))
	probes := make([]stressPlanProbeRecord, 0, len(languages)*8)

	for _, lang := range languages {
		type candidate struct {
			source string
			path   string
		}
		candidates := make([]candidate, 0, 8)
		if envPath != "" {
			candidates = append(candidates, candidate{source: "env:LSP_STRESS_APP_DIR", path: envPath})
		}
		candidates = append(candidates, candidate{source: "corpus-infer", path: filepath.Join(corpusRoot, corpusSub[lang])})
		for _, rel := range repoCandidates {
			candidates = append(candidates, candidate{source: "repo-candidate", path: rel})
		}

		seen := map[string]struct{}{}
		for _, cand := range candidates {
			path := cand.path
			if !filepath.IsAbs(path) {
				path = filepath.Clean(filepath.Join(repoRoot, path))
			}
			if _, ok := seen[path]; ok {
				continue
			}
			seen[path] = struct{}{}

			ok, reason := stressPlanValidateAppDir(path, lang)
			probes = append(probes, stressPlanProbeRecord{
				Language: lang,
				Source:   cand.source,
				Path:     path,
				Valid:    ok,
				Reason:   reason,
			})
			if ok {
				resolved[lang] = path
				break
			}
		}
	}

	if len(resolved) == len(languages) {
		return resolved, probes, nil
	}

	missing := make([]string, 0, len(languages))
	for _, lang := range languages {
		if _, ok := resolved[lang]; !ok {
			missing = append(missing, lang)
		}
	}
	sort.Strings(missing)

	var details []string
	for _, probe := range probes {
		if !probe.Valid {
			details = append(details, fmt.Sprintf("%s [%s] %s: %s", probe.Language, probe.Source, probe.Path, probe.Reason))
		}
	}
	sort.Strings(details)
	return resolved, probes, fmt.Errorf("discover app dirs failed for %v; probes=%v", missing, details)
}

func stressPlanValidateAppDir(path, language string) (bool, string) {
	info, err := os.Stat(path)
	if err != nil {
		return false, err.Error()
	}
	if !info.IsDir() {
		return false, "not a directory"
	}
	for _, marker := range stressPlanMarkerFiles {
		markerPath := filepath.Join(path, marker)
		markerInfo, markerErr := os.Stat(markerPath)
		if markerErr == nil && !markerInfo.IsDir() {
			return true, "marker: " + marker
		}
	}

	exts := stressPlanExtensionsForLanguage(language)
	if len(exts) > 0 {
		files, err := stressPlanCollectFiles(path, exts, 1)
		if err == nil && len(files) > 0 {
			return true, "fallback: language source files detected"
		}
	}
	return false, "missing marker file (package.json/go.mod/Cargo.toml/tsconfig.json)"
}

func stressPlanExtensionsForLanguage(language string) []string {
	switch normalizeLanguage(language) {
	case "go":
		return []string{".go"}
	case "typescript":
		return []string{".ts", ".tsx", ".js", ".jsx"}
	case "rust":
		return []string{".rs"}
	default:
		return nil
	}
}

func stressPlanCollectFiles(root string, exts []string, limit int) ([]string, error) {
	extSet := make(map[string]struct{}, len(exts))
	for _, ext := range exts {
		clean := strings.ToLower(strings.TrimSpace(ext))
		if clean == "" {
			continue
		}
		if !strings.HasPrefix(clean, ".") {
			clean = "." + clean
		}
		extSet[clean] = struct{}{}
	}

	files := make([]string, 0, max(64, limit))
	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			switch filepath.Base(path) {
			case ".git", "node_modules", "target", "vendor", ".vite", ".next", "dist", "build":
				return filepath.SkipDir
			}
			return nil
		}

		if len(extSet) > 0 {
			ext := strings.ToLower(filepath.Ext(path))
			if _, ok := extSet[ext]; !ok {
				return nil
			}
		}

		info, infoErr := d.Info()
		if infoErr != nil {
			return nil
		}
		if info.Size() > 1_000_000 {
			return nil
		}
		files = append(files, path)
		return nil
	})
	if walkErr != nil {
		return nil, walkErr
	}
	sort.Strings(files)
	if limit > 0 && len(files) > limit {
		files = files[:limit]
	}
	return files, nil
}

func stressPlanOpenWithRetry(mgr *Manager, filePath string, attempts int, wait time.Duration) error {
	if attempts < 1 {
		attempts = 1
	}
	content, err := os.ReadFile(filePath)
	if err != nil {
		return err
	}

	var lastErr error
	for i := 0; i < attempts; i++ {
		if err := mgr.OpenFile(filePath, string(content)); err == nil {
			return nil
		} else {
			lastErr = err
		}
		time.Sleep(wait)
	}
	return lastErr
}

func stressPlanClassifyErr(err error) string {
	if err == nil {
		return ""
	}
	text := strings.ToLower(err.Error())
	switch {
	case strings.Contains(text, "timeout"):
		return "timeout"
	case strings.Contains(text, "no language server configured"):
		return "unsupported_language"
	case strings.Contains(text, "unsupported"):
		return "unsupported"
	case strings.Contains(text, "not running"):
		return "server_not_running"
	case strings.Contains(text, "no such file"):
		return "file_not_found"
	case strings.Contains(text, "invalid"):
		return "invalid_argument"
	default:
		return "request_failed"
	}
}

func stressPlanRunTool(stage, tool, language, filePath, note string, fn func() error) stressPlanToolResult {
	start := time.Now()
	err := fn()
	out := stressPlanToolResult{
		Stage:      stage,
		Tool:       tool,
		Language:   language,
		File:       filePath,
		DurationMS: time.Since(start).Milliseconds(),
		Success:    err == nil,
		Note:       note,
	}
	if err != nil {
		out.ErrorCode = stressPlanClassifyErr(err)
		out.ErrorText = err.Error()
	}
	return out
}

func stressPlanBuildSummary(stage string, gatePassed bool, thresholds map[string]any, results []stressPlanToolResult, notes []string) stressPlanStageSummary {
	toolStats := make(map[string]stressPlanStat)
	langStats := make(map[string]stressPlanStat)
	toolLang := make(map[string]map[string]int)

	for _, r := range results {
		ts := toolStats[r.Tool]
		ts.Attempts++
		if r.Success {
			ts.Successes++
			if _, ok := toolLang[r.Tool]; !ok {
				toolLang[r.Tool] = map[string]int{}
			}
			toolLang[r.Tool][r.Language]++
		}
		if ts.Attempts > 0 {
			ts.SuccessRate = float64(ts.Successes) / float64(ts.Attempts)
		}
		toolStats[r.Tool] = ts

		ls := langStats[r.Language]
		ls.Attempts++
		if r.Success {
			ls.Successes++
		}
		if ls.Attempts > 0 {
			ls.SuccessRate = float64(ls.Successes) / float64(ls.Attempts)
		}
		langStats[r.Language] = ls
	}

	sort.Strings(notes)
	return stressPlanStageSummary{
		Stage:               stage,
		GeneratedAt:         time.Now().Format(time.RFC3339),
		GatePassed:          gatePassed,
		Thresholds:          thresholds,
		ToolStats:           toolStats,
		LanguageStats:       langStats,
		ToolLanguageSuccess: toolLang,
		Results:             results,
		Notes:               notes,
	}
}

func stressPlanNormalizePath(filePath string) string {
	abs, err := filepath.Abs(filePath)
	if err != nil {
		return filepath.Clean(filePath)
	}
	return filepath.Clean(abs)
}

func stressPlanURIToPath(uri string) string {
	if strings.HasPrefix(uri, "file://") {
		parsed, err := url.Parse(uri)
		if err == nil {
			decoded, decodeErr := url.PathUnescape(parsed.Path)
			if decodeErr == nil {
				return stressPlanNormalizePath(filepath.FromSlash(decoded))
			}
			return stressPlanNormalizePath(filepath.FromSlash(parsed.Path))
		}
	}
	return stressPlanNormalizePath(uri)
}

func stressPlanSymbolNames(symbols []DocumentSymbol, limit int) []string {
	names := make([]string, 0, min(limit, len(symbols)))
	var walk func(nodes []DocumentSymbol)
	walk = func(nodes []DocumentSymbol) {
		for _, node := range nodes {
			if len(names) >= limit {
				return
			}
			names = append(names, node.Name)
			if len(node.Children) > 0 {
				walk(node.Children)
			}
		}
	}
	walk(symbols)
	return names
}

func stressPlanIsIdentByte(ch byte) bool {
	return (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '_'
}

func stressPlanIsIdentStartByte(ch byte) bool {
	return (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || ch == '_'
}

func stressPlanFindTokenPosition(content, token string) (line, col int, ok bool) {
	if token == "" {
		return 0, 0, false
	}
	lines := strings.Split(content, "\n")
	for i, ln := range lines {
		start := 0
		for {
			idx := strings.Index(ln[start:], token)
			if idx < 0 {
				break
			}
			p := start + idx
			beforeOK := p == 0 || !stressPlanIsIdentByte(ln[p-1])
			after := p + len(token)
			afterOK := after >= len(ln) || !stressPlanIsIdentByte(ln[after])
			if beforeOK && afterOK {
				return i, p, true
			}
			start = p + 1
		}
	}
	return 0, 0, false
}

func stressPlanFirstSymbolPoint(symbols []DocumentSymbol) (line, col int, ok bool) {
	var walk func(nodes []DocumentSymbol) (int, int, bool)
	walk = func(nodes []DocumentSymbol) (int, int, bool) {
		for _, node := range nodes {
			return node.SelectionRange.Start.Line, node.SelectionRange.Start.Character, true
		}
		return 0, 0, false
	}
	return walk(symbols)
}

func stressPlanFallbackIdentPoint(content string) (line, col int, ok bool) {
	curLine, curCol := 0, 0
	for i := 0; i < len(content); {
		ch := content[i]
		if stressPlanIsIdentStartByte(ch) {
			startLine, startCol := curLine, curCol
			i++
			curCol++
			length := 1
			for i < len(content) && stressPlanIsIdentByte(content[i]) {
				i++
				curCol++
				length++
			}
			if length >= 2 {
				return startLine, startCol, true
			}
			continue
		}
		if ch == '\n' {
			curLine++
			curCol = 0
			i++
			continue
		}
		i++
		curCol++
	}
	return 0, 0, false
}

func stressPlanPickPoint(content string, symbols []DocumentSymbol) (line, col int, ok bool) {
	if l, c, ok := stressPlanFirstSymbolPoint(symbols); ok {
		return l, c, true
	}
	return stressPlanFallbackIdentPoint(content)
}

func stressPlanCountWorkspaceEdits(edit *WorkspaceEdit) int {
	if edit == nil {
		return 0
	}
	total := 0
	for _, edits := range edit.Changes {
		total += len(edits)
	}
	for _, dc := range edit.DocumentChanges {
		total += len(dc.Edits)
	}
	return total
}

func stressPlanEditContainsText(edit *WorkspaceEdit, newText string) bool {
	if edit == nil {
		return false
	}
	for _, edits := range edit.Changes {
		for _, e := range edits {
			if e.NewText == newText {
				return true
			}
		}
	}
	for _, dc := range edit.DocumentChanges {
		for _, e := range dc.Edits {
			if e.NewText == newText {
				return true
			}
		}
	}
	return false
}

func stressPlanEditsForURI(edit *WorkspaceEdit, targetURI string) []TextEdit {
	if edit == nil {
		return nil
	}
	targetPath := stressPlanNormalizePath(stressPlanURIToPath(targetURI))
	var out []TextEdit
	for uri, edits := range edit.Changes {
		if stressPlanNormalizePath(stressPlanURIToPath(uri)) == targetPath {
			out = append(out, edits...)
		}
	}
	for _, dc := range edit.DocumentChanges {
		if stressPlanNormalizePath(stressPlanURIToPath(dc.TextDocument.URI)) == targetPath {
			out = append(out, dc.Edits...)
		}
	}
	return out
}

func stressPlanPositionToOffset(content string, pos Position) (int, error) {
	if pos.Line < 0 || pos.Character < 0 {
		return 0, fmt.Errorf("invalid position: line=%d char=%d", pos.Line, pos.Character)
	}
	lineStarts := []int{0}
	for i := 0; i < len(content); i++ {
		if content[i] == '\n' {
			lineStarts = append(lineStarts, i+1)
		}
	}
	if pos.Line >= len(lineStarts) {
		return 0, fmt.Errorf("line out of range: %d", pos.Line)
	}
	start := lineStarts[pos.Line]
	end := len(content)
	if pos.Line+1 < len(lineStarts) {
		end = lineStarts[pos.Line+1] - 1
	}
	offset := start + pos.Character
	if offset > end {
		offset = end
	}
	return offset, nil
}

func stressPlanApplyTextEdits(content string, edits []TextEdit) (string, error) {
	if len(edits) == 0 {
		return content, nil
	}
	type indexed struct {
		start int
		end   int
		edit  TextEdit
	}
	arr := make([]indexed, 0, len(edits))
	for _, e := range edits {
		start, err := stressPlanPositionToOffset(content, e.Range.Start)
		if err != nil {
			return "", err
		}
		end, err := stressPlanPositionToOffset(content, e.Range.End)
		if err != nil {
			return "", err
		}
		if start > end {
			return "", fmt.Errorf("invalid edit range: %d > %d", start, end)
		}
		arr = append(arr, indexed{start: start, end: end, edit: e})
	}
	sort.Slice(arr, func(i, j int) bool {
		if arr[i].start == arr[j].start {
			return arr[i].end > arr[j].end
		}
		return arr[i].start > arr[j].start
	})
	current := content
	for _, item := range arr {
		current = current[:item.start] + item.edit.NewText + current[item.end:]
	}
	return current, nil
}

func stressPlanEnvInt(key string, fallback int) int {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return fallback
	}
	return n
}

func stressPlanCore9Coverage(results []stressPlanToolResult, languages []string) map[string]map[string]bool {
	coverage := map[string]map[string]bool{}
	for _, tool := range stressPlanCore9Tools {
		coverage[tool] = map[string]bool{}
		for _, lang := range languages {
			coverage[tool][lang] = false
		}
	}
	for _, r := range results {
		if !r.Success {
			continue
		}
		if _, ok := coverage[r.Tool]; !ok {
			continue
		}
		coverage[r.Tool][r.Language] = true
	}
	return coverage
}

func stressPlanMissingCoverage(coverage map[string]map[string]bool) []string {
	var missing []string
	for tool, byLang := range coverage {
		for lang, ok := range byLang {
			if !ok {
				missing = append(missing, fmt.Sprintf("%s@%s", tool, lang))
			}
		}
	}
	sort.Strings(missing)
	return missing
}

func stressPlanCreateEditFixture(t *testing.T, language string) (root, filePath, content, oldName, renameNewName, changedContent, changedSymbol string) {
	t.Helper()
	lang := normalizeLanguage(language)
	root = t.TempDir()

	switch lang {
	case "go":
		must(t, os.WriteFile(filepath.Join(root, "go.mod"), []byte("module stressp0b\ngo 1.22\n"), 0o644))
		oldName = "OldName"
		renameNewName = "RenamedName"
		changedSymbol = "ChangedName"
		content = "package stressp0b\n\nfunc OldName() int {\n\treturn 1\n}\n\nfunc Use() int {\n\treturn OldName()\n}\n"
		changedContent = strings.ReplaceAll(content, oldName, changedSymbol)
		filePath = filepath.Join(root, "main.go")
		must(t, os.WriteFile(filePath, []byte(content), 0o644))
	case "typescript":
		must(t, os.WriteFile(filepath.Join(root, "tsconfig.json"), []byte(`{"compilerOptions":{"strict":true},"include":["*.ts"]}`), 0o644))
		oldName = "oldName"
		renameNewName = "renamedName"
		changedSymbol = "changedName"
		content = "function oldName(): number { return 1; }\nfunction useName(): number { return oldName(); }\nexport const value = useName();\n"
		changedContent = strings.ReplaceAll(content, oldName, changedSymbol)
		filePath = filepath.Join(root, "main.ts")
		must(t, os.WriteFile(filePath, []byte(content), 0o644))
	case "rust":
		must(t, os.WriteFile(filepath.Join(root, "Cargo.toml"), []byte("[package]\nname = \"stressp0b\"\nversion = \"0.1.0\"\nedition = \"2021\"\n"), 0o644))
		must(t, os.MkdirAll(filepath.Join(root, "src"), 0o755))
		oldName = "old_name"
		renameNewName = "renamed_name"
		changedSymbol = "changed_name"
		content = "fn old_name() -> i32 { 1 }\nfn use_name() -> i32 { old_name() }\nfn main() { let _ = use_name(); }\n"
		changedContent = strings.ReplaceAll(content, oldName, changedSymbol)
		filePath = filepath.Join(root, "src", "main.rs")
		must(t, os.WriteFile(filePath, []byte(content), 0o644))
	default:
		t.Fatalf("unsupported language fixture: %s", language)
	}

	return root, filePath, content, oldName, renameNewName, changedContent, changedSymbol
}

func stressPlanRunRenameDidChange(t *testing.T, language string) (stressPlanToolResult, stressPlanToolResult) {
	t.Helper()
	root, filePath, content, oldName, renameNewName, changedContent, changedSymbol := stressPlanCreateEditFixture(t, language)

	mgr := NewManager(nil)
	mgr.SetRootURI(pathToURI(root))
	defer mgr.StopAll()

	openErr := stressPlanOpenWithRetry(mgr, filePath, 3, 200*time.Millisecond)
	if openErr != nil {
		fail := stressPlanToolResult{
			Stage:     "P0-B",
			Tool:      spToolRename,
			Language:  language,
			File:      filePath,
			Success:   false,
			ErrorCode: stressPlanClassifyErr(openErr),
			ErrorText: "precondition open failed: " + openErr.Error(),
		}
		return fail, fail
	}
	time.Sleep(1200 * time.Millisecond)

	line, col, ok := stressPlanFindTokenPosition(content, oldName)
	if !ok {
		err := errors.New("rename token not found")
		failRename := stressPlanToolResult{Stage: "P0-B", Tool: spToolRename, Language: language, File: filePath, Success: false, ErrorCode: stressPlanClassifyErr(err), ErrorText: err.Error()}
		failChange := stressPlanToolResult{Stage: "P0-B", Tool: spToolDidChange, Language: language, File: filePath, Success: false, ErrorCode: "precondition_failed", ErrorText: err.Error()}
		return failRename, failChange
	}

	renameResult := stressPlanRunTool("P0-B", spToolRename, language, filePath, "", func() error {
		edit, err := mgr.Rename(filePath, line, col, renameNewName)
		if err != nil {
			return err
		}
		if stressPlanCountWorkspaceEdits(edit) == 0 {
			return fmt.Errorf("rename returned no edits")
		}
		if !stressPlanEditContainsText(edit, renameNewName) {
			return fmt.Errorf("rename edits missing expected text %q", renameNewName)
		}
		targetEdits := stressPlanEditsForURI(edit, pathToURI(filePath))
		if len(targetEdits) > 0 {
			applied, applyErr := stressPlanApplyTextEdits(content, targetEdits)
			if applyErr != nil {
				return fmt.Errorf("apply rename edits: %w", applyErr)
			}
			if !strings.Contains(applied, renameNewName) {
				return fmt.Errorf("rename applied text missing %q", renameNewName)
			}
		}
		return nil
	})

	didChangeResult := stressPlanRunTool("P0-B", spToolDidChange, language, filePath, "", func() error {
		if err := mgr.ChangeFile(filePath, 2, changedContent); err != nil {
			return err
		}
		for i := 0; i < 8; i++ {
			symbols, err := mgr.DocumentSymbol(filePath)
			if err == nil {
				for _, name := range stressPlanSymbolNames(symbols, 128) {
					if name == changedSymbol {
						return nil
					}
				}
			}
			time.Sleep(400 * time.Millisecond)
		}
		return fmt.Errorf("didChange not reflected in symbols: want %s", changedSymbol)
	})

	return renameResult, didChangeResult
}

type stressPlanXrefFixture struct {
	Root           string
	FilePath       string
	Content        string
	WorkspaceQuery string
	ImplToken      string
	TypeDefToken   string
	CallToken      string
	TypeToken      string
}

func stressPlanCreateXrefFixture(t *testing.T, language string) stressPlanXrefFixture {
	t.Helper()
	lang := normalizeLanguage(language)
	root := t.TempDir()

	switch lang {
	case "go":
		must(t, os.WriteFile(filepath.Join(root, "go.mod"), []byte("module xreffixture\ngo 1.22\n"), 0o644))
		content := "package main\n\ntype Speaker interface {\n\tSpeak() string\n}\n\ntype Dog struct{}\n\nfunc (Dog) Speak() string {\n\treturn \"woof\"\n}\n\nfunc useSpeaker(s Speaker) string {\n\treturn s.Speak()\n}\n\nfunc main() {\n\td := Dog{}\n\t_ = useSpeaker(d)\n}\n"
		filePath := filepath.Join(root, "main.go")
		must(t, os.WriteFile(filePath, []byte(content), 0o644))
		return stressPlanXrefFixture{Root: root, FilePath: filePath, Content: content, WorkspaceQuery: "useSpeaker", ImplToken: "Speaker", TypeDefToken: "d", CallToken: "useSpeaker", TypeToken: "Dog"}
	case "typescript":
		must(t, os.WriteFile(filepath.Join(root, "tsconfig.json"), []byte(`{"compilerOptions":{"target":"ES2020","strict":true},"include":["*.ts"]}`), 0o644))
		content := "class Animal {\n  speak(): string {\n    return \"animal\";\n  }\n}\n\nclass Dog extends Animal {\n  override speak(): string {\n    return \"dog\";\n  }\n}\n\nfunction makeSound(a: Animal): string {\n  return a.speak();\n}\n\nconst d: Dog = new Dog();\nconsole.log(makeSound(d));\n"
		filePath := filepath.Join(root, "main.ts")
		must(t, os.WriteFile(filePath, []byte(content), 0o644))
		return stressPlanXrefFixture{Root: root, FilePath: filePath, Content: content, WorkspaceQuery: "makeSound", ImplToken: "speak", TypeDefToken: "d", CallToken: "makeSound", TypeToken: "Dog"}
	case "rust":
		must(t, os.WriteFile(filepath.Join(root, "Cargo.toml"), []byte("[package]\nname = \"xreffixture\"\nversion = \"0.1.0\"\nedition = \"2021\"\n"), 0o644))
		must(t, os.MkdirAll(filepath.Join(root, "src"), 0o755))
		content := "trait Speak {\n    fn speak(&self) -> String;\n}\n\nstruct Dog;\n\nimpl Speak for Dog {\n    fn speak(&self) -> String {\n        \"woof\".to_string()\n    }\n}\n\nfn use_speak(s: &dyn Speak) -> String {\n    s.speak()\n}\n\nfn main() {\n    let d = Dog;\n    let _ = use_speak(&d);\n}\n"
		filePath := filepath.Join(root, "src", "main.rs")
		must(t, os.WriteFile(filePath, []byte(content), 0o644))
		return stressPlanXrefFixture{Root: root, FilePath: filePath, Content: content, WorkspaceQuery: "use_speak", ImplToken: "Speak", TypeDefToken: "d", CallToken: "use_speak", TypeToken: "Dog"}
	default:
		t.Fatalf("unsupported xref fixture language: %s", language)
	}
	return stressPlanXrefFixture{}
}

type stressPlanActionsFixture struct {
	Root            string
	Language        string
	FilePath        string
	Content         string
	CodeActionToken string
	SignatureToken  string
}

func stressPlanCreateActionsFixture(t *testing.T, language string) stressPlanActionsFixture {
	t.Helper()
	lang := normalizeLanguage(language)
	root := t.TempDir()

	switch lang {
	case "go":
		must(t, os.WriteFile(filepath.Join(root, "go.mod"), []byte("module actionsfixture\ngo 1.22\n"), 0o644))
		content := "package main\n\nfunc  add(a int,b int)int{ return a+b }\n\nfunc main() {\n\t_ = add(1, 2)\n\tx := 1\n}\n"
		filePath := filepath.Join(root, "main.go")
		must(t, os.WriteFile(filePath, []byte(content), 0o644))
		return stressPlanActionsFixture{Root: root, Language: lang, FilePath: filePath, Content: content, CodeActionToken: "x", SignatureToken: "add("}
	case "typescript":
		must(t, os.WriteFile(filepath.Join(root, "tsconfig.json"), []byte(`{"compilerOptions":{"strict":true},"include":["*.ts"]}`), 0o644))
		content := "function add(a: number, b: number): number {\n  return a + b;\n}\n\nconst x: number = \"oops\";\nconsole.log(add(1, 2), x);\n"
		filePath := filepath.Join(root, "main.ts")
		must(t, os.WriteFile(filePath, []byte(content), 0o644))
		return stressPlanActionsFixture{Root: root, Language: lang, FilePath: filePath, Content: content, CodeActionToken: "oops", SignatureToken: "add("}
	default:
		t.Fatalf("unsupported actions fixture language: %s", language)
	}
	return stressPlanActionsFixture{}
}

func stressPlanPickSampleCount() int {
	return stressPlanEnvInt("LSP_STRESS_SAMPLE_PER_LANG", 20)
}

func TestLSP_Stress_P0A(t *testing.T) {
	corpusRoot := stressPlanDefaultCorpusRoot()
	appDirs, probes, err := stressPlanDiscoverAppDirs(corpusRoot)
	if err != nil {
		t.Fatalf("P0-A discoverAppDirs failed: %v", err)
	}

	languages := []string{"go", "typescript", "rust"}
	results := make([]stressPlanToolResult, 0, len(languages)*2)
	notes := make([]string, 0, len(probes)+4)

	for _, p := range probes {
		if p.Valid {
			notes = append(notes, fmt.Sprintf("probe ok: %s <- %s (%s)", p.Language, p.Path, p.Source))
		}
	}

	for _, language := range languages {
		language := language
		t.Run(language, func(t *testing.T) {
			requireServerOrSkip(t, language)
			root := appDirs[language]
			files, err := stressPlanCollectFiles(root, stressPlanExtensionsForLanguage(language), 1)
			if err != nil {
				t.Fatalf("collect files %s: %v", language, err)
			}
			if len(files) == 0 {
				t.Fatalf("no %s files found under %s", language, root)
			}
			filePath := files[0]

			mgr := NewManager(nil)
			mgr.SetRootURI(pathToURI(root))
			defer mgr.StopAll()

			openRec := stressPlanRunTool("P0-A", spToolOpen, language, filePath, "smoke open", func() error {
				return stressPlanOpenWithRetry(mgr, filePath, 3, 250*time.Millisecond)
			})
			results = append(results, openRec)

			docRec := stressPlanRunTool("P0-A", spToolDocument, language, filePath, "smoke open->documentSymbol", func() error {
				if !openRec.Success {
					return fmt.Errorf("open precondition failed")
				}
				_, err := mgr.DocumentSymbol(filePath)
				return err
			})
			results = append(results, docRec)
		})
	}

	coverage := stressPlanCore9Coverage(results, languages)
	missing := stressPlanMissingCoverage(map[string]map[string]bool{
		spToolOpen:     coverage[spToolOpen],
		spToolDocument: coverage[spToolDocument],
	})
	gatePassed := len(missing) == 0
	if len(missing) > 0 {
		notes = append(notes, "P0-A missing coverage: "+strings.Join(missing, ", "))
	}

	summary := stressPlanBuildSummary("P0-A", gatePassed, map[string]any{"smoke": "open->documentSymbol"}, results, notes)
	p0aPath := filepath.Join(stressPlanOutputDir(t), "p0a-summary.json")
	stressPlanWriteJSON(t, p0aPath, summary)

	conclusion := "目录探测与 smoke 流程已执行"
	if gatePassed {
		conclusion = "目录探测与 smoke 流程通过"
	}
	stressPlanAppendReport(t, "P0-A", gatePassed, conclusion, notes, "每种语言至少 1 个文件完成 open->documentSymbol", []string{p0aPath})
}

func TestLSP_Stress_P0B_Core9(t *testing.T) {
	corpusRoot := stressPlanDefaultCorpusRoot()
	appDirs, _, err := stressPlanDiscoverAppDirs(corpusRoot)
	if err != nil {
		t.Fatalf("P0-B discoverAppDirs failed: %v", err)
	}

	sampleCount := stressPlanPickSampleCount()
	languages := []string{"go", "typescript", "rust"}
	allResults := make([]stressPlanToolResult, 0, sampleCount*len(languages)*7)
	unresolved := make([]string, 0, 32)

	for _, language := range languages {
		language := language
		t.Run(language, func(t *testing.T) {
			requireServerOrSkip(t, language)

			root := appDirs[language]
			files, err := stressPlanCollectFiles(root, stressPlanExtensionsForLanguage(language), sampleCount)
			if err != nil {
				t.Fatalf("collect files: %v", err)
			}
			if len(files) == 0 {
				t.Fatalf("no %s files under %s", language, root)
			}

			mgr := NewManager(nil)
			mgr.SetRootURI(pathToURI(root))
			diag := stressPlanNewDiagTracker()
			mgr.SetDiagnosticHandler(diag.handler)
			defer mgr.StopAll()

			opened := make([]string, 0, len(files))
			for _, filePath := range files {
				openRec := stressPlanRunTool("P0-B", spToolOpen, language, filePath, "", func() error {
					return stressPlanOpenWithRetry(mgr, filePath, 3, 200*time.Millisecond)
				})
				allResults = append(allResults, openRec)
				if openRec.Success {
					opened = append(opened, filePath)
				}
			}
			time.Sleep(2 * time.Second)

			for _, filePath := range opened {
				content, readErr := os.ReadFile(filePath)
				if readErr != nil {
					unresolved = append(unresolved, fmt.Sprintf("%s read failed: %v", filePath, readErr))
					continue
				}

				var symbols []DocumentSymbol
				docRec := stressPlanRunTool("P0-B", spToolDocument, language, filePath, "", func() error {
					out, err := mgr.DocumentSymbol(filePath)
					symbols = out
					return err
				})
				allResults = append(allResults, docRec)

				line, col, pointOK := stressPlanPickPoint(string(content), symbols)
				if !pointOK {
					line, col = 0, 0
				}

				hoverRec := stressPlanRunTool("P0-B", spToolHover, language, filePath, "", func() error {
					_, err := mgr.Hover(filePath, line, col)
					return err
				})
				allResults = append(allResults, hoverRec)

				defRec := stressPlanRunTool("P0-B", spToolDefinition, language, filePath, "", func() error {
					_, err := mgr.Definition(filePath, line, col)
					return err
				})
				allResults = append(allResults, defRec)

				refRec := stressPlanRunTool("P0-B", spToolReferences, language, filePath, "", func() error {
					_, err := mgr.References(filePath, line, col, true)
					return err
				})
				allResults = append(allResults, refRec)

				compRec := stressPlanRunTool("P0-B", spToolCompletion, language, filePath, "", func() error {
					_, err := mgr.Completion(filePath, line, col)
					return err
				})
				allResults = append(allResults, compRec)

				diagCount := diag.count(filePath)
				diagRec := stressPlanToolResult{
					Stage:      "P0-B",
					Tool:       spToolDiagnostics,
					Language:   language,
					File:       filePath,
					DurationMS: 0,
					Success:    true,
					Note:       fmt.Sprintf("diagnostics=%d", diagCount),
				}
				allResults = append(allResults, diagRec)
			}

			if diag.totalCount() == 0 {
				unresolved = append(unresolved, fmt.Sprintf("%s diagnostics callback count=0", language))
			}

			renameRec, didChangeRec := stressPlanRunRenameDidChange(t, language)
			allResults = append(allResults, renameRec, didChangeRec)
			if !renameRec.Success {
				unresolved = append(unresolved, fmt.Sprintf("%s rename failed: %s", language, renameRec.ErrorText))
			}
			if !didChangeRec.Success {
				unresolved = append(unresolved, fmt.Sprintf("%s didChange failed: %s", language, didChangeRec.ErrorText))
			}
		})
	}

	summary := stressPlanBuildSummary("P0-B", false, map[string]any{"core9_success_rate": ">= 0.98"}, allResults, unresolved)

	coreAttempts := 0
	coreSuccesses := 0
	for _, tool := range stressPlanCore9Tools {
		stats := summary.ToolStats[tool]
		coreAttempts += stats.Attempts
		coreSuccesses += stats.Successes
	}
	coreRate := 0.0
	if coreAttempts > 0 {
		coreRate = float64(coreSuccesses) / float64(coreAttempts)
	}
	coverage := stressPlanCore9Coverage(allResults, languages)
	missing := stressPlanMissingCoverage(coverage)

	gatePassed := coreRate >= 0.98 && len(missing) == 0
	summary.GatePassed = gatePassed
	summary.Extra = map[string]any{
		"core9_attempts":     coreAttempts,
		"core9_successes":    coreSuccesses,
		"core9_success_rate": coreRate,
		"sample_per_lang":    sampleCount,
		"coverage_missing":   missing,
	}
	if len(missing) > 0 {
		summary.Notes = append(summary.Notes, "coverage missing: "+strings.Join(missing, ", "))
	}

	corePath := filepath.Join(stressPlanOutputDir(t), "core9-summary.json")
	stressPlanWriteJSON(t, corePath, summary)

	conclusion := fmt.Sprintf("Core9 success rate=%.2f%%", coreRate*100)
	stressPlanAppendReport(t, "P0-B", gatePassed, conclusion, summary.Notes, "工具 1-9 每语言至少成功 1 次，且 success_rate>=98%", []string{corePath})
}

func TestLSP_Stress_P1_Smoke(t *testing.T) {
	languages := []string{"go", "typescript", "rust"}
	results := make([]stressPlanToolResult, 0, 256)
	emptyReasons := map[string]map[string]string{}
	notes := make([]string, 0, 32)

	for _, tool := range []string{spToolWorkspace, spToolImplement, spToolTypeDef, spToolCallHierarchy, spToolTypeHierarchy} {
		emptyReasons[tool] = map[string]string{}
	}

	nonEmptyCount := map[string]int{}

	for _, language := range languages {
		language := language
		t.Run(language, func(t *testing.T) {
			requireServerOrSkip(t, language)

			fx := stressPlanCreateXrefFixture(t, language)
			mgr := NewManager(nil)
			mgr.SetRootURI(pathToURI(fx.Root))
			defer mgr.StopAll()

			openRec := stressPlanRunTool("P1", spToolOpen, language, fx.FilePath, "fixture bootstrap", func() error {
				return stressPlanOpenWithRetry(mgr, fx.FilePath, 3, 200*time.Millisecond)
			})
			results = append(results, openRec)
			if !openRec.Success {
				notes = append(notes, fmt.Sprintf("%s fixture open failed", language))
				return
			}
			time.Sleep(1500 * time.Millisecond)

			implLine, implCol, implOK := stressPlanFindTokenPosition(fx.Content, fx.ImplToken)
			typeLine, typeCol, typeOK := stressPlanFindTokenPosition(fx.Content, fx.TypeDefToken)
			callLine, callCol, callOK := stressPlanFindTokenPosition(fx.Content, fx.CallToken)
			typeHierLine, typeHierCol, typeHierOK := stressPlanFindTokenPosition(fx.Content, fx.TypeToken)
			if !implOK || !typeOK || !callOK || !typeHierOK {
				notes = append(notes, fmt.Sprintf("%s fixture token position fallback used", language))
			}

			workspaceRec := stressPlanRunTool("P1", spToolWorkspace, language, fx.FilePath, "", func() error {
				items, err := mgr.WorkspaceSymbol("", language, fx.WorkspaceQuery)
				if err != nil {
					return err
				}
				if len(items) == 0 {
					emptyReasons[spToolWorkspace][language] = "no workspace symbol matches"
					return nil
				}
				nonEmptyCount[spToolWorkspace]++
				return nil
			})
			results = append(results, workspaceRec)

			implRec := stressPlanRunTool("P1", spToolImplement, language, fx.FilePath, "", func() error {
				locs, err := mgr.Implementation(fx.FilePath, implLine, implCol)
				if err != nil {
					return err
				}
				if len(locs) == 0 {
					emptyReasons[spToolImplement][language] = "no implementation result"
					return nil
				}
				nonEmptyCount[spToolImplement]++
				return nil
			})
			results = append(results, implRec)

			typeDefRec := stressPlanRunTool("P1", spToolTypeDef, language, fx.FilePath, "", func() error {
				locs, err := mgr.TypeDefinition(fx.FilePath, typeLine, typeCol)
				if err != nil {
					return err
				}
				if len(locs) == 0 {
					emptyReasons[spToolTypeDef][language] = "no type definition result"
					return nil
				}
				nonEmptyCount[spToolTypeDef]++
				return nil
			})
			results = append(results, typeDefRec)

			callRec := stressPlanRunTool("P1", spToolCallHierarchy, language, fx.FilePath, "", func() error {
				items, err := mgr.CallHierarchy(fx.FilePath, callLine, callCol, "both")
				if err != nil {
					return err
				}
				if len(items) == 0 {
					emptyReasons[spToolCallHierarchy][language] = "no call hierarchy items"
					return nil
				}
				nonEmptyCount[spToolCallHierarchy]++
				return nil
			})
			results = append(results, callRec)

			typeHierRec := stressPlanRunTool("P1", spToolTypeHierarchy, language, fx.FilePath, "", func() error {
				items, err := mgr.TypeHierarchy(fx.FilePath, typeHierLine, typeHierCol, "both")
				if err != nil {
					return err
				}
				if len(items) == 0 {
					emptyReasons[spToolTypeHierarchy][language] = "no type hierarchy items"
					return nil
				}
				nonEmptyCount[spToolTypeHierarchy]++
				return nil
			})
			results = append(results, typeHierRec)
		})
	}

	summary := stressPlanBuildSummary("P1", false, map[string]any{"non_empty_languages_per_tool": ">=2"}, results, notes)
	summary.EmptyReasons = emptyReasons

	gatePassed := true
	for _, tool := range []string{spToolWorkspace, spToolImplement, spToolTypeDef, spToolCallHierarchy, spToolTypeHierarchy} {
		if nonEmptyCount[tool] < 2 {
			gatePassed = false
			summary.Notes = append(summary.Notes, fmt.Sprintf("%s non-empty languages=%d (<2)", tool, nonEmptyCount[tool]))
		}
	}
	summary.GatePassed = gatePassed
	p1Path := filepath.Join(stressPlanOutputDir(t), "xref-hierarchy-summary.json")
	stressPlanWriteJSON(t, p1Path, summary)

	conclusion := "P1 跨文件/层级能力 smoke 完成"
	stressPlanAppendReport(t, "P1", gatePassed, conclusion, summary.Notes, "工具 10-14 至少两种语言出现非空结果；空结果已记录原因", []string{p1Path})
}

func TestLSP_Stress_P2_Smoke(t *testing.T) {
	results := make([]stressPlanToolResult, 0, 128)
	notes := make([]string, 0, 32)
	positives := map[string]bool{
		spToolCodeAction: false,
		spToolSignature:  false,
		spToolFormat:     false,
		spToolSemantic:   false,
		spToolFolding:    false,
	}

	goFX := stressPlanCreateActionsFixture(t, "go")
	mgrGo := NewManager(nil)
	mgrGo.SetRootURI(pathToURI(goFX.Root))
	defer mgrGo.StopAll()

	openGo := stressPlanRunTool("P2", spToolOpen, "go", goFX.FilePath, "", func() error {
		return stressPlanOpenWithRetry(mgrGo, goFX.FilePath, 3, 200*time.Millisecond)
	})
	results = append(results, openGo)
	if !openGo.Success {
		notes = append(notes, "go action fixture open failed")
	}
	time.Sleep(1200 * time.Millisecond)

	if codeLine, codeCol, ok := stressPlanFindTokenPosition(goFX.Content, goFX.CodeActionToken); ok {
		codeRec := stressPlanRunTool("P2", spToolCodeAction, "go", goFX.FilePath, "", func() error {
			actions, err := mgrGo.CodeAction(goFX.FilePath, codeLine, codeCol, codeLine, codeCol+1, nil)
			if err != nil {
				return err
			}
			if len(actions) > 0 {
				positives[spToolCodeAction] = true
			}
			return nil
		})
		results = append(results, codeRec)
	}

	if sigLine, sigCol, ok := stressPlanFindTokenPosition(goFX.Content, goFX.SignatureToken); ok {
		sigRec := stressPlanRunTool("P2", spToolSignature, "go", goFX.FilePath, "", func() error {
			h, err := mgrGo.SignatureHelp(goFX.FilePath, sigLine, sigCol+len("add("))
			if err != nil {
				return err
			}
			if h != nil && len(h.Signatures) > 0 {
				positives[spToolSignature] = true
			}
			return nil
		})
		results = append(results, sigRec)
	}

	formatRec := stressPlanRunTool("P2", spToolFormat, "go", goFX.FilePath, "format idempotence", func() error {
		edits1, err := mgrGo.Format(goFX.FilePath, 4, true)
		if err != nil {
			return err
		}
		formatted, err := stressPlanApplyTextEdits(goFX.Content, edits1)
		if err != nil {
			return err
		}
		if err := mgrGo.ChangeFile(goFX.FilePath, 2, formatted); err != nil {
			return err
		}
		time.Sleep(800 * time.Millisecond)
		edits2, err := mgrGo.Format(goFX.FilePath, 4, true)
		if err != nil {
			return err
		}
		if len(edits2) == 0 {
			positives[spToolFormat] = true
			return nil
		}
		return fmt.Errorf("format not idempotent: second edits=%d", len(edits2))
	})
	results = append(results, formatRec)

	semanticRec := stressPlanRunTool("P2", spToolSemantic, "go", goFX.FilePath, "", func() error {
		st, err := mgrGo.SemanticTokens(goFX.FilePath)
		if err != nil {
			return err
		}
		if st != nil && (len(st.Data) > 0 || len(st.Decoded) > 0) {
			positives[spToolSemantic] = true
		}
		return nil
	})
	results = append(results, semanticRec)

	foldingRec := stressPlanRunTool("P2", spToolFolding, "go", goFX.FilePath, "", func() error {
		ranges, err := mgrGo.FoldingRange(goFX.FilePath)
		if err != nil {
			return err
		}
		if len(ranges) > 0 {
			positives[spToolFolding] = true
		}
		return nil
	})
	results = append(results, foldingRec)

	if !positives[spToolCodeAction] {
		tsFX := stressPlanCreateActionsFixture(t, "typescript")
		mgrTS := NewManager(nil)
		mgrTS.SetRootURI(pathToURI(tsFX.Root))
		defer mgrTS.StopAll()

		openTS := stressPlanRunTool("P2", spToolOpen, "typescript", tsFX.FilePath, "codeAction fallback", func() error {
			return stressPlanOpenWithRetry(mgrTS, tsFX.FilePath, 3, 200*time.Millisecond)
		})
		results = append(results, openTS)
		if openTS.Success {
			time.Sleep(1200 * time.Millisecond)
			if codeLine, codeCol, ok := stressPlanFindTokenPosition(tsFX.Content, tsFX.CodeActionToken); ok {
				codeRecTS := stressPlanRunTool("P2", spToolCodeAction, "typescript", tsFX.FilePath, "fallback", func() error {
					actions, err := mgrTS.CodeAction(tsFX.FilePath, codeLine, codeCol, codeLine, codeCol+len(tsFX.CodeActionToken), nil)
					if err != nil {
						return err
					}
					if len(actions) > 0 {
						positives[spToolCodeAction] = true
					}
					return nil
				})
				results = append(results, codeRecTS)
			}
		}
	}

	gatePassed := true
	for tool, ok := range positives {
		if !ok {
			gatePassed = false
			notes = append(notes, fmt.Sprintf("%s no positive sample", tool))
		}
	}

	summary := stressPlanBuildSummary("P2", gatePassed, map[string]any{"positive_sample_each_tool": true, "format_idempotence": true}, results, notes)
	summary.Extra = map[string]any{"positive_tools": positives}
	p2Path := filepath.Join(stressPlanOutputDir(t), "actions-semantic-summary.json")
	stressPlanWriteJSON(t, p2Path, summary)

	conclusion := "P2 编辑动作与语义展示能力 smoke 完成"
	stressPlanAppendReport(t, "P2", gatePassed, conclusion, summary.Notes, "工具 15-19 每项至少 1 个正样例，formatting 幂等", []string{p2Path})
}

func TestLSP_Stress_P3_Stability(t *testing.T) {
	requireServerOrSkip(t, "go")
	corpusRoot := stressPlanDefaultCorpusRoot()
	appDirs, _, err := stressPlanDiscoverAppDirs(corpusRoot)
	if err != nil {
		t.Fatalf("P3 discoverAppDirs failed: %v", err)
	}

	goRoot := appDirs["go"]
	files, err := stressPlanCollectFiles(goRoot, []string{".go"}, 12)
	if err != nil {
		t.Fatalf("collect go files: %v", err)
	}
	if len(files) == 0 {
		t.Fatalf("no go files for stability test")
	}

	mgr := NewManager(nil)
	mgr.SetRootURI(pathToURI(goRoot))
	defer mgr.StopAll()

	opened := make([]string, 0, len(files))
	for _, filePath := range files {
		if err := stressPlanOpenWithRetry(mgr, filePath, 3, 200*time.Millisecond); err == nil {
			opened = append(opened, filePath)
		}
	}
	if len(opened) == 0 {
		t.Fatalf("P3 precondition failed: no opened go files")
	}
	time.Sleep(2 * time.Second)

	targetFile := opened[0]
	content, readErr := os.ReadFile(targetFile)
	if readErr != nil {
		t.Fatalf("read target file: %v", readErr)
	}
	symbols, _ := mgr.DocumentSymbol(targetFile)
	line, col, ok := stressPlanPickPoint(string(content), symbols)
	if !ok {
		line, col = 0, 0
	}

	workers := stressPlanEnvInt("LSP_STRESS_WORKERS", 8)
	soakRounds := stressPlanEnvInt("LSP_STRESS_SOAK_ROUNDS", 20)

	var panicCount atomic.Int64
	var opCount atomic.Int64
	var errCount atomic.Int64

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					panicCount.Add(1)
				}
			}()
			for round := 0; round < 6; round++ {
				if _, err := mgr.DocumentSymbol(targetFile); err != nil {
					errCount.Add(1)
				}
				opCount.Add(1)
				if _, err := mgr.Hover(targetFile, line, col); err != nil {
					errCount.Add(1)
				}
				opCount.Add(1)
				if _, err := mgr.Definition(targetFile, line, col); err != nil {
					errCount.Add(1)
				}
				opCount.Add(1)
				if _, err := mgr.References(targetFile, line, col, true); err != nil {
					errCount.Add(1)
				}
				opCount.Add(1)
				if _, err := mgr.Completion(targetFile, line, col); err != nil {
					errCount.Add(1)
				}
				opCount.Add(1)
			}
		}(i)
	}
	wg.Wait()

	recoveryOK := false
	mgr.Reload()
	time.Sleep(600 * time.Millisecond)
	if err := stressPlanOpenWithRetry(mgr, targetFile, 3, 300*time.Millisecond); err == nil {
		for i := 0; i < 6; i++ {
			if _, err := mgr.DocumentSymbol(targetFile); err == nil {
				recoveryOK = true
				break
			}
			time.Sleep(400 * time.Millisecond)
		}
	}

	soakOps := 0
	soakFailures := 0
	for i := 0; i < soakRounds; i++ {
		if _, err := mgr.Hover(targetFile, line, col); err != nil {
			soakFailures++
		}
		soakOps++
		if _, err := mgr.Definition(targetFile, line, col); err != nil {
			soakFailures++
		}
		soakOps++
		if _, err := mgr.References(targetFile, line, col, true); err != nil {
			soakFailures++
		}
		soakOps++
		if _, err := mgr.Completion(targetFile, line, col); err != nil {
			soakFailures++
		}
		soakOps++
	}

	failureRate := 0.0
	if soakOps > 0 {
		failureRate = float64(soakFailures) / float64(soakOps)
	}
	gatePassed := panicCount.Load() == 0 && recoveryOK && failureRate < 0.02

	notes := make([]string, 0, 8)
	if panicCount.Load() > 0 {
		notes = append(notes, fmt.Sprintf("panic_count=%d", panicCount.Load()))
	}
	if !recoveryOK {
		notes = append(notes, "recovery failed after Reload")
	}
	if failureRate >= 0.02 {
		notes = append(notes, fmt.Sprintf("soak failure rate %.2f%% >= 2%%", failureRate*100))
	}

	summary := stressPlanBuildSummary("P3", gatePassed, map[string]any{"soak_failure_rate": "<2%", "panic": 0}, nil, notes)
	summary.Extra = map[string]any{
		"workers":          workers,
		"concurrency_ops":  opCount.Load(),
		"concurrency_errors": errCount.Load(),
		"panic_count":      panicCount.Load(),
		"recovery_ok":      recoveryOK,
		"soak_rounds":      soakRounds,
		"soak_ops":         soakOps,
		"soak_failures":    soakFailures,
		"soak_failure_rate": failureRate,
	}
	p3Path := filepath.Join(stressPlanOutputDir(t), "stability-summary.json")
	stressPlanWriteJSON(t, p3Path, summary)

	conclusion := fmt.Sprintf("P3 并发/恢复/soak 已执行，soak failure rate=%.2f%%", failureRate*100)
	stressPlanAppendReport(t, "P3", gatePassed, conclusion, summary.Notes, "panic=0，恢复成功，soak 累计失败率<2%", []string{p3Path})
}

func TestLSP_Stress_QuickGate_CommandHint(t *testing.T) {
	t.Log("Quick Gate command:")
	t.Log("go test ./internal/lsp -run 'TestLSP_Stress_P0A|TestLSP_Stress_P0B_Core9|TestLSP_Stress_P1_Smoke|TestLSP_Stress_P2_Smoke' -count=1 -timeout 20m")
}

func TestLSP_Stress_FullGate_CommandHint(t *testing.T) {
	t.Log("Full Gate command:")
	t.Log("go test -tags lspstress ./internal/lsp -run 'TestLSP_Stress_' -count=1 -timeout 60m")
	t.Log("go test -race ./internal/lsp -run 'TestLSP_Stress_' -count=1 -timeout 60m")
}
