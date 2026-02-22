// lsp_stress_test.go — LSP 大规模冒烟测试（严格验收版）。
//
// 对 3 个真实项目执行全链路 LSP 操作:
//
//	open → documentSymbol → definition → references → hover → diagnostics
//
// 运行:
//
//	go test -v -run TestLSP_Stress -timeout 600s ./internal/lsp/
package lsp

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const testCorpusDir = "/Users/mima0000/Desktop/wj/e2e测试"

// TestResult 单个文件的测试结果。
type TestResult struct {
	File            string   `json:"file"`
	Language        string   `json:"language"`
	OpenOK          bool     `json:"open_ok"`
	SymbolCount     int      `json:"symbol_count"`
	SymbolNames     []string `json:"symbol_names,omitempty"`
	DefinitionOK    bool     `json:"definition_ok"`
	DefinitionLoc   string   `json:"definition_loc,omitempty"`
	ReferencesOK    bool     `json:"references_ok"`
	ReferenceCount  int      `json:"reference_count"`
	HoverOK         bool     `json:"hover_ok"`
	HoverPreview    string   `json:"hover_preview,omitempty"`
	DiagnosticsOK   bool     `json:"diagnostics_ok"`
	DiagnosticCount int      `json:"diagnostic_count"`
	Errors          []string `json:"errors,omitempty"`
}

type acceptanceThreshold struct {
	OpenRate       float64
	SymbolRate     float64
	DefinitionRate float64
	ReferenceRate  float64
	HoverRate      float64
	MaxErrorRate   float64
}

type symbolPoint struct {
	line      int
	character int
}

type renameTarget struct {
	name      string
	line      int
	character int
}

type diagTracker struct {
	total  atomic.Int64
	mu     sync.Mutex
	byPath map[string]int
}

var identifierKeywordSet = map[string]struct{}{
	"as": {}, "async": {}, "await": {}, "break": {}, "case": {}, "chan": {}, "class": {}, "const": {},
	"continue": {}, "crate": {}, "default": {}, "defer": {}, "else": {}, "enum": {}, "false": {}, "fn": {},
	"for": {}, "func": {}, "function": {}, "go": {}, "if": {}, "impl": {}, "import": {}, "in": {}, "interface": {},
	"let": {}, "loop": {}, "map": {}, "match": {}, "mod": {}, "mut": {}, "new": {}, "nil": {}, "package": {},
	"pub": {}, "range": {}, "return": {}, "self": {}, "select": {}, "struct": {}, "switch": {}, "this": {},
	"trait": {}, "true": {}, "type": {}, "unsafe": {}, "use": {}, "var": {}, "where": {}, "while": {},
}

func newDiagTracker() *diagTracker {
	return &diagTracker{
		byPath: make(map[string]int),
	}
}

func (d *diagTracker) add(uri string, diagnostics []Diagnostic) {
	d.total.Add(int64(len(diagnostics)))
	path := uriToPath(uri)
	if path == "" {
		return
	}
	d.mu.Lock()
	d.byPath[path] += len(diagnostics)
	d.mu.Unlock()
}

func (d *diagTracker) count(filePath string) int {
	normalized := normalizePath(filePath)
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.byPath[normalized]
}

func (d *diagTracker) totalCount() int {
	return int(d.total.Load())
}

func normalizePath(filePath string) string {
	abs, err := filepath.Abs(filePath)
	if err != nil {
		return filepath.Clean(filePath)
	}
	return filepath.Clean(abs)
}

func uriToPath(uri string) string {
	if strings.HasPrefix(uri, "file://") {
		parsed, err := url.Parse(uri)
		if err == nil {
			unescaped, unescapeErr := url.PathUnescape(parsed.Path)
			if unescapeErr == nil {
				return normalizePath(filepath.FromSlash(unescaped))
			}
			return normalizePath(filepath.FromSlash(parsed.Path))
		}
	}
	return normalizePath(uri)
}

func collectFiles(t *testing.T, dir string, exts ...string) []string {
	t.Helper()
	extSet := make(map[string]struct{}, len(exts))
	for _, ext := range exts {
		clean := ext
		if !strings.HasPrefix(clean, ".") {
			clean = "." + clean
		}
		extSet[strings.ToLower(clean)] = struct{}{}
	}

	var files []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if d.IsDir() {
			switch filepath.Base(path) {
			case ".git", ".vite", "node_modules", "target", "vendor":
				return filepath.SkipDir
			}
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		if info.Size() > 500*1024 {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if _, ok := extSet[ext]; ok {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("collectFiles(%s): %v", dir, err)
	}
	sort.Strings(files)
	return files
}

func openFiles(mgr *Manager, files []string) (map[string]error, int) {
	openErrs := make(map[string]error, len(files))
	opened := 0
	for _, filePath := range files {
		content, err := os.ReadFile(filePath)
		if err != nil {
			openErrs[filePath] = err
			continue
		}
		if err := mgr.OpenFile(filePath, string(content)); err != nil {
			openErrs[filePath] = err
			continue
		}
		opened++
	}
	return openErrs, opened
}

func isIdentByte(ch byte) bool {
	return (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '_'
}

func isIdentStartByte(ch byte) bool {
	return (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || ch == '_'
}

func findIdentifierInLines(lines []string, ident string) (line, character int, ok bool) {
	if ident == "" {
		return 0, 0, false
	}
	for i, ln := range lines {
		start := 0
		for {
			idx := strings.Index(ln[start:], ident)
			if idx < 0 {
				break
			}
			col := start + idx
			beforeOK := col == 0 || !isIdentByte(ln[col-1])
			after := col + len(ident)
			afterOK := after >= len(ln) || !isIdentByte(ln[after])
			if beforeOK && afterOK {
				return i, col, true
			}
			start = col + 1
		}
	}
	return 0, 0, false
}

func collectIdentifierPoints(content string, limit int) []symbolPoint {
	points := make([]symbolPoint, 0, limit)
	seenToken := make(map[string]struct{}, limit*2)
	line, col := 0, 0
	for i := 0; i < len(content) && len(points) < limit; {
		ch := content[i]
		if isIdentStartByte(ch) {
			start := i
			startLine, startCol := line, col
			i++
			col++
			for i < len(content) && isIdentByte(content[i]) {
				i++
				col++
			}
			token := content[start:i]
			if len(token) < 2 {
				continue
			}
			if _, isKeyword := identifierKeywordSet[token]; isKeyword {
				continue
			}
			if _, ok := seenToken[token]; ok {
				continue
			}
			seenToken[token] = struct{}{}
			points = append(points, symbolPoint{line: startLine, character: startCol})
			continue
		}
		if ch == '\n' {
			line++
			col = 0
			i++
			continue
		}
		i++
		col++
	}
	return points
}

func collectSymbolNames(symbols []DocumentSymbol, limit int) []string {
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

func collectSymbolPoints(content string, symbols []DocumentSymbol, limit int) []symbolPoint {
	points := make([]symbolPoint, 0, limit)
	seen := make(map[symbolPoint]struct{}, limit*2)
	push := func(p symbolPoint) {
		if len(points) >= limit {
			return
		}
		if _, ok := seen[p]; ok {
			return
		}
		seen[p] = struct{}{}
		points = append(points, p)
	}

	lines := strings.Split(content, "\n")
	names := collectSymbolNames(symbols, limit*4)
	for _, name := range names {
		if len(points) >= limit {
			break
		}
		if len(name) < 2 {
			continue
		}
		line, col, ok := findIdentifierInLines(lines, name)
		if ok {
			push(symbolPoint{line: line, character: col})
		}
	}

	for _, p := range collectIdentifierPoints(content, limit*4) {
		if len(points) >= limit {
			break
		}
		push(p)
	}

	var walk func(nodes []DocumentSymbol)
	walk = func(nodes []DocumentSymbol) {
		for _, node := range nodes {
			if len(points) >= limit {
				return
			}
			push(symbolPoint{
				line:      node.SelectionRange.Start.Line,
				character: node.SelectionRange.Start.Character,
			})
			if len(node.Children) > 0 {
				walk(node.Children)
			}
		}
	}
	walk(symbols)
	return points
}

func testFile(t *testing.T, mgr *Manager, filePath, lang string) TestResult {
	t.Helper()
	r := TestResult{
		File:          filePath,
		Language:      lang,
		DiagnosticsOK: true,
	}

	content, readErr := os.ReadFile(filePath)
	if readErr != nil {
		r.Errors = append(r.Errors, "readFile: "+readErr.Error())
		return r
	}

	symbols, err := mgr.DocumentSymbol(filePath)
	if err != nil {
		r.Errors = append(r.Errors, "documentSymbol: "+err.Error())
		return r
	}

	r.SymbolCount = len(symbols)
	r.SymbolNames = collectSymbolNames(symbols, 32)
	points := collectSymbolPoints(string(content), symbols, 12)
	if len(points) == 0 {
		return r
	}

	var definitionErr error
	for _, p := range points {
		locs, defErr := mgr.Definition(filePath, p.line, p.character)
		if defErr != nil {
			definitionErr = defErr
			continue
		}
		if len(locs) == 0 {
			continue
		}
		r.DefinitionOK = true
		r.DefinitionLoc = fmt.Sprintf("L%d:%d", locs[0].Range.Start.Line+1, locs[0].Range.Start.Character+1)
		break
	}
	if !r.DefinitionOK && definitionErr != nil {
		r.Errors = append(r.Errors, "definition: "+definitionErr.Error())
	}

	var referencesErr error
	for _, p := range points {
		refs, refErr := mgr.References(filePath, p.line, p.character, true)
		if refErr != nil {
			referencesErr = refErr
			continue
		}
		if len(refs) == 0 {
			continue
		}
		r.ReferencesOK = true
		r.ReferenceCount = len(refs)
		break
	}
	if !r.ReferencesOK && referencesErr != nil {
		r.Errors = append(r.Errors, "references: "+referencesErr.Error())
	}

	var hoverErr error
	for _, p := range points {
		hover, hErr := mgr.Hover(filePath, p.line, p.character)
		if hErr != nil {
			hoverErr = hErr
			continue
		}
		if hover == nil {
			continue
		}
		r.HoverOK = true
		preview := strings.TrimSpace(hover.Contents.Value)
		if len(preview) > 100 {
			preview = preview[:100] + "..."
		}
		r.HoverPreview = preview
		break
	}
	if !r.HoverOK && hoverErr != nil {
		r.Errors = append(r.Errors, "hover: "+hoverErr.Error())
	}

	return r
}

func assertRate(t *testing.T, metric string, success, total int, minRate float64) {
	t.Helper()
	if total == 0 {
		t.Fatalf("%s: total is 0", metric)
	}
	rate := float64(success) / float64(total)
	if rate < minRate {
		t.Errorf("%s 成功率低于阈值: %.1f%% < %.1f%% (%d/%d)", metric, rate*100, minRate*100, success, total)
	}
}

func summarize(t *testing.T, results []TestResult, lang string, threshold acceptanceThreshold) {
	t.Helper()
	total := len(results)
	if total == 0 {
		t.Fatalf("%s: no files to test", lang)
	}

	openOK, symbolOK, defOK, refOK, hoverOK, diagOK := 0, 0, 0, 0, 0, 0
	totalSymbols, totalRefs, totalDiags, totalErrors := 0, 0, 0, 0
	for _, r := range results {
		if r.OpenOK {
			openOK++
		}
		if r.SymbolCount > 0 {
			symbolOK++
		}
		if r.DefinitionOK {
			defOK++
		}
		if r.ReferencesOK {
			refOK++
		}
		if r.HoverOK {
			hoverOK++
		}
		if r.DiagnosticsOK {
			diagOK++
		}
		totalSymbols += r.SymbolCount
		totalRefs += r.ReferenceCount
		totalDiags += r.DiagnosticCount
		totalErrors += len(r.Errors)
	}

	t.Logf("=== %s 汇总 (%d files) ===", lang, total)
	t.Logf("  Open:           %d/%d 成功", openOK, total)
	t.Logf("  DocumentSymbol: %d/%d 有符号 (共 %d 个符号)", symbolOK, total, totalSymbols)
	t.Logf("  Definition:     %d/%d 成功", defOK, total)
	t.Logf("  References:     %d/%d 成功 (共 %d 引用)", refOK, total, totalRefs)
	t.Logf("  Hover:          %d/%d 成功", hoverOK, total)
	t.Logf("  Diagnostics:    %d/%d 成功 (共 %d 条诊断)", diagOK, total, totalDiags)
	t.Logf("  Errors:         %d 个错误", totalErrors)

	assertRate(t, "Open", openOK, total, threshold.OpenRate)
	assertRate(t, "DocumentSymbol", symbolOK, total, threshold.SymbolRate)
	assertRate(t, "Definition", defOK, total, threshold.DefinitionRate)
	assertRate(t, "References", refOK, total, threshold.ReferenceRate)
	assertRate(t, "Hover", hoverOK, total, threshold.HoverRate)

	errorRate := float64(totalErrors) / float64(total)
	if errorRate > threshold.MaxErrorRate {
		t.Errorf("错误率超阈值: %.1f%% > %.1f%% (%d/%d)", errorRate*100, threshold.MaxErrorRate*100, totalErrors, total)
	}
}

func writeResultsJSON(t *testing.T, outputPath string, results []TestResult) {
	t.Helper()
	data, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		t.Fatalf("marshal results %s: %v", outputPath, err)
	}
	if err := os.WriteFile(outputPath, data, 0644); err != nil {
		t.Fatalf("write results %s: %v", outputPath, err)
	}
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0644)
}

func prepareRustWorkspace(t *testing.T, rustRoot string) (string, []string) {
	t.Helper()

	srcRoot := filepath.Join(rustRoot, "src")
	srcFiles := collectFiles(t, srcRoot, ".rs")
	if len(srcFiles) == 0 {
		t.Fatalf("expected Rust source files under %s", srcRoot)
	}

	workRoot := t.TempDir()
	workSrc := filepath.Join(workRoot, "src")
	if err := os.MkdirAll(workSrc, 0755); err != nil {
		t.Fatalf("mkdir %s: %v", workSrc, err)
	}

	dstFiles := make([]string, 0, len(srcFiles))
	for _, srcFile := range srcFiles {
		rel, err := filepath.Rel(srcRoot, srcFile)
		if err != nil {
			t.Fatalf("filepath.Rel(%s, %s): %v", srcRoot, srcFile, err)
		}
		dstFile := filepath.Join(workSrc, rel)
		if err := os.MkdirAll(filepath.Dir(dstFile), 0755); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(dstFile), err)
		}
		if err := copyFile(srcFile, dstFile); err != nil {
			t.Fatalf("copy %s -> %s: %v", srcFile, dstFile, err)
		}
		dstFiles = append(dstFiles, dstFile)
	}
	sort.Strings(dstFiles)

	hasMain := false
	hasLib := false
	topLevelMods := make([]string, 0, len(dstFiles))
	for _, f := range dstFiles {
		rel, err := filepath.Rel(workSrc, f)
		if err != nil {
			continue
		}
		if strings.Contains(rel, string(filepath.Separator)) {
			continue
		}
		base := strings.TrimSuffix(filepath.Base(f), ".rs")
		switch base {
		case "main":
			hasMain = true
		case "lib":
			hasLib = true
		default:
			topLevelMods = append(topLevelMods, base)
		}
	}
	sort.Strings(topLevelMods)

	if !hasLib {
		var b strings.Builder
		for _, modName := range topLevelMods {
			b.WriteString("pub mod ")
			b.WriteString(modName)
			b.WriteString(";\n")
		}
		libPath := filepath.Join(workSrc, "lib.rs")
		if err := os.WriteFile(libPath, []byte(b.String()), 0644); err != nil {
			t.Fatalf("write %s: %v", libPath, err)
		}
		dstFiles = append(dstFiles, libPath)
		sort.Strings(dstFiles)
	}

	var cargo strings.Builder
	cargo.WriteString("[package]\n")
	cargo.WriteString("name = \"lsp_stress_rust\"\n")
	cargo.WriteString("version = \"0.1.0\"\n")
	cargo.WriteString("edition = \"2021\"\n\n")
	cargo.WriteString("[lib]\n")
	cargo.WriteString("path = \"src/lib.rs\"\n")
	if hasMain {
		cargo.WriteString("\n[[bin]]\n")
		cargo.WriteString("name = \"lsp_stress_rust_bin\"\n")
		cargo.WriteString("path = \"src/main.rs\"\n")
	}

	cargoPath := filepath.Join(workRoot, "Cargo.toml")
	if err := os.WriteFile(cargoPath, []byte(cargo.String()), 0644); err != nil {
		t.Fatalf("write %s: %v", cargoPath, err)
	}
	t.Logf("Prepared temporary Rust workspace: %s", workRoot)

	return workRoot, dstFiles
}

func runStressGo(t *testing.T) {
	skipIfNotAvailable(t, "gopls")

	goRoot := filepath.Join(testCorpusDir, "go")
	mgr := NewManager(nil)
	mgr.SetRootURI("file://" + goRoot)
	diag := newDiagTracker()
	mgr.SetDiagnosticHandler(diag.add)
	defer mgr.StopAll()

	goFiles := collectFiles(t, goRoot, ".go")
	if len(goFiles) < 100 {
		t.Fatalf("expected at least 100 Go files, got %d", len(goFiles))
	}
	const maxOpen = 150
	if len(goFiles) > maxOpen {
		t.Logf("Go files sampled: %d/%d (strict sample window)", maxOpen, len(goFiles))
		goFiles = goFiles[:maxOpen]
	}

	openErrs, opened := openFiles(mgr, goFiles)
	t.Logf("Opened %d/%d files", opened, len(goFiles))
	time.Sleep(12 * time.Second)

	results := make([]TestResult, 0, len(goFiles))
	for _, filePath := range goFiles {
		r := testFile(t, mgr, filePath, "go")
		openErr := openErrs[filePath]
		r.OpenOK = openErr == nil
		if openErr != nil {
			r.Errors = append([]string{"open: " + openErr.Error()}, r.Errors...)
		}
		r.DiagnosticsOK = r.OpenOK
		r.DiagnosticCount = diag.count(filePath)
		results = append(results, r)
	}

	summarize(t, results, "Go", acceptanceThreshold{
		OpenRate:       0.95,
		SymbolRate:     0.80,
		DefinitionRate: 0.50,
		ReferenceRate:  0.50,
		HoverRate:      0.60,
		MaxErrorRate:   0.40,
	})
	if diag.totalCount() == 0 {
		t.Errorf("Go diagnostics callback not triggered")
	}
	writeResultsJSON(t, filepath.Join(testCorpusDir, "go-results.json"), results)
}

func runStressJS(t *testing.T) {
	skipIfNotAvailable(t, "typescript-language-server")

	jsRoot := filepath.Join(testCorpusDir, "js")
	mgr := NewManager(nil)
	mgr.SetRootURI("file://" + jsRoot)
	diag := newDiagTracker()
	mgr.SetDiagnosticHandler(diag.add)
	defer mgr.StopAll()

	files := collectFiles(t, filepath.Join(jsRoot, "src"), ".ts", ".tsx")
	if len(files) < 20 {
		t.Fatalf("expected at least 20 TS/TSX files, got %d", len(files))
	}

	openErrs, opened := openFiles(mgr, files)
	t.Logf("Opened %d/%d files", opened, len(files))
	time.Sleep(10 * time.Second)

	results := make([]TestResult, 0, len(files))
	for _, filePath := range files {
		r := testFile(t, mgr, filePath, "typescript")
		openErr := openErrs[filePath]
		r.OpenOK = openErr == nil
		if openErr != nil {
			r.Errors = append([]string{"open: " + openErr.Error()}, r.Errors...)
		}
		r.DiagnosticsOK = r.OpenOK
		r.DiagnosticCount = diag.count(filePath)
		results = append(results, r)
	}

	summarize(t, results, "JS/TS", acceptanceThreshold{
		OpenRate:       0.90,
		SymbolRate:     0.70,
		DefinitionRate: 0.50,
		ReferenceRate:  0.50,
		HoverRate:      0.50,
		MaxErrorRate:   0.25,
	})
	if diag.totalCount() == 0 {
		t.Errorf("JS diagnostics callback not triggered")
	}
	writeResultsJSON(t, filepath.Join(testCorpusDir, "js-results.json"), results)
}

func runStressRust(t *testing.T) {
	skipIfNotAvailable(t, "rust-analyzer")

	rustRoot := filepath.Join(testCorpusDir, "rust")
	workspaceRoot, files := prepareRustWorkspace(t, rustRoot)
	mgr := NewManager(nil)
	mgr.SetRootURI("file://" + workspaceRoot)
	diag := newDiagTracker()
	mgr.SetDiagnosticHandler(diag.add)
	defer mgr.StopAll()

	if len(files) < 5 {
		t.Fatalf("expected at least 5 Rust files, got %d", len(files))
	}

	openErrs, opened := openFiles(mgr, files)
	t.Logf("Opened %d/%d files", opened, len(files))
	time.Sleep(12 * time.Second)

	results := make([]TestResult, 0, len(files))
	for _, filePath := range files {
		r := testFile(t, mgr, filePath, "rust")
		openErr := openErrs[filePath]
		r.OpenOK = openErr == nil
		if openErr != nil {
			r.Errors = append([]string{"open: " + openErr.Error()}, r.Errors...)
		}
		r.DiagnosticsOK = r.OpenOK
		r.DiagnosticCount = diag.count(filePath)
		results = append(results, r)
	}

	summarize(t, results, "Rust", acceptanceThreshold{
		OpenRate:       0.90,
		SymbolRate:     0.70,
		DefinitionRate: 0.50,
		ReferenceRate:  0.50,
		HoverRate:      0.50,
		MaxErrorRate:   0.30,
	})
	if diag.totalCount() == 0 {
		t.Errorf("Rust diagnostics callback not triggered")
	}
	writeResultsJSON(t, filepath.Join(testCorpusDir, "rust-results.json"), results)
}

func countIdentifiers(content string) map[string]int {
	counts := make(map[string]int)
	for i := 0; i < len(content); {
		ch := content[i]
		if !isIdentStartByte(ch) {
			i++
			continue
		}
		j := i + 1
		for j < len(content) && isIdentByte(content[j]) {
			j++
		}
		token := content[i:j]
		if _, isKeyword := identifierKeywordSet[token]; !isKeyword {
			counts[token]++
		}
		i = j
	}
	return counts
}

func pickRenameTargets(content string, symbols []DocumentSymbol, limit int) []renameTarget {
	lines := strings.Split(content, "\n")
	seen := make(map[string]struct{}, limit*2)
	targets := make([]renameTarget, 0, limit)

	push := func(name string, line, col int) {
		if len(targets) >= limit {
			return
		}
		key := fmt.Sprintf("%s:%d:%d", name, line, col)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		targets = append(targets, renameTarget{name: name, line: line, character: col})
	}

	for _, name := range collectSymbolNames(symbols, 64) {
		if len(name) < 3 {
			continue
		}
		if _, isKeyword := identifierKeywordSet[name]; isKeyword {
			continue
		}
		line, col, ok := findIdentifierInLines(lines, name)
		if ok {
			push(name, line, col)
		}
	}
	if len(targets) >= limit {
		return targets
	}

	type kv struct {
		name  string
		count int
	}
	var ranked []kv
	for name, count := range countIdentifiers(content) {
		if len(name) < 3 || count < 2 {
			continue
		}
		if _, isKeyword := identifierKeywordSet[name]; isKeyword {
			continue
		}
		ranked = append(ranked, kv{name: name, count: count})
	}
	sort.Slice(ranked, func(i, j int) bool {
		if ranked[i].count == ranked[j].count {
			return ranked[i].name < ranked[j].name
		}
		return ranked[i].count > ranked[j].count
	})
	for _, item := range ranked {
		if len(targets) >= limit {
			break
		}
		line, col, ok := findIdentifierInLines(lines, item.name)
		if ok {
			push(item.name, line, col)
		}
	}
	return targets
}

func countWorkspaceEdits(edit *WorkspaceEdit) int {
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

func TestLSP_Stress_Go(t *testing.T) {
	runStressGo(t)
}

func TestLSP_Stress_JS(t *testing.T) {
	runStressJS(t)
}

func TestLSP_Stress_Rust(t *testing.T) {
	runStressRust(t)
}

func TestLSP_Stress_Rename(t *testing.T) {
	skipIfNotAvailable(t, "gopls")

	goRoot := filepath.Join(testCorpusDir, "go")
	mgr := NewManager(nil)
	mgr.SetRootURI("file://" + goRoot)
	defer mgr.StopAll()

	files := collectFiles(t, goRoot, ".go")
	if len(files) == 0 {
		t.Fatal("no Go files found for rename test")
	}
	if len(files) > 80 {
		files = files[:80]
	}

	openErrs, opened := openFiles(mgr, files)
	if opened == 0 {
		t.Fatalf("rename precondition failed: no files opened")
	}
	time.Sleep(10 * time.Second)

	var attempts int
	var debug []string
	for _, filePath := range files {
		if err := openErrs[filePath]; err != nil {
			debug = append(debug, fmt.Sprintf("%s: open failed: %v", filepath.Base(filePath), err))
			continue
		}
		content, err := os.ReadFile(filePath)
		if err != nil {
			debug = append(debug, fmt.Sprintf("%s: read failed: %v", filepath.Base(filePath), err))
			continue
		}
		symbols, err := mgr.DocumentSymbol(filePath)
		if err != nil || len(symbols) == 0 {
			debug = append(debug, fmt.Sprintf("%s: documentSymbol unavailable: %v", filepath.Base(filePath), err))
			continue
		}
		targets := pickRenameTargets(string(content), symbols, 10)
		for _, target := range targets {
			attempts++
			newName := target.name + "RenamedStress"
			edit, renameErr := mgr.Rename(filePath, target.line, target.character, newName)
			if renameErr != nil {
				debug = append(debug, fmt.Sprintf("%s:%s rename error: %v", filepath.Base(filePath), target.name, renameErr))
				continue
			}
			totalEdits := countWorkspaceEdits(edit)
			if totalEdits >= 2 {
				t.Logf("Rename succeeded: %s:%s -> %s (%d edits)", filepath.Base(filePath), target.name, newName, totalEdits)
				return
			}
			debug = append(debug, fmt.Sprintf("%s:%s rename edits=%d", filepath.Base(filePath), target.name, totalEdits))
		}
	}

	if len(debug) > 12 {
		debug = debug[:12]
	}
	t.Fatalf("rename strict gate failed: attempts=%d sample=%v", attempts, debug)
}

func TestLSP_Stress_All(t *testing.T) {
	t.Run("Go", runStressGo)
	t.Run("JS", runStressJS)
	t.Run("Rust", runStressRust)
	t.Run("Rename", TestLSP_Stress_Rename)
}

type LatencyStats struct {
	Name    string
	Samples []time.Duration
}

func (ls *LatencyStats) Add(d time.Duration) {
	ls.Samples = append(ls.Samples, d)
}

func (ls *LatencyStats) Count() int {
	return len(ls.Samples)
}

func (ls *LatencyStats) percentile(percent int) time.Duration {
	if len(ls.Samples) == 0 {
		return 0
	}
	cp := make([]time.Duration, len(ls.Samples))
	copy(cp, ls.Samples)
	sort.Slice(cp, func(i, j int) bool { return cp[i] < cp[j] })

	n := len(cp)
	if percent <= 0 {
		return cp[0]
	}
	if percent >= 100 {
		return cp[n-1]
	}
	idx := (n*percent + 99) / 100
	if idx <= 0 {
		idx = 1
	}
	if idx > n {
		idx = n
	}
	return cp[idx-1]
}

func (ls *LatencyStats) P50() time.Duration {
	return ls.percentile(50)
}

func (ls *LatencyStats) P95() time.Duration {
	return ls.percentile(95)
}

func (ls *LatencyStats) Max() time.Duration {
	if len(ls.Samples) == 0 {
		return 0
	}
	max := ls.Samples[0]
	for _, d := range ls.Samples[1:] {
		if d > max {
			max = d
		}
	}
	return max
}

func (ls *LatencyStats) Report(t *testing.T) {
	t.Helper()
	if len(ls.Samples) == 0 {
		t.Logf("  %s: no samples", ls.Name)
		return
	}
	t.Logf("  %s: n=%d p50=%v p95=%v max=%v", ls.Name, len(ls.Samples), ls.P50(), ls.P95(), ls.Max())
}

func commandAvailable(cmd string) bool {
	_, err := exec.LookPath(cmd)
	return err == nil
}

func openSampleFiles(t *testing.T, mgr *Manager, files []string, limit int) []string {
	t.Helper()
	if limit > len(files) {
		limit = len(files)
	}
	opened := make([]string, 0, limit)
	for i := 0; i < limit; i++ {
		content, err := os.ReadFile(files[i])
		if err != nil {
			continue
		}
		if err := mgr.OpenFile(files[i], string(content)); err != nil {
			continue
		}
		opened = append(opened, files[i])
	}
	return opened
}

func firstPointForFile(t *testing.T, mgr *Manager, filePath string) (symbolPoint, bool) {
	t.Helper()
	content, err := os.ReadFile(filePath)
	if err != nil {
		return symbolPoint{}, false
	}
	symbols, err := mgr.DocumentSymbol(filePath)
	if err != nil || len(symbols) == 0 {
		return symbolPoint{}, false
	}
	points := collectSymbolPoints(string(content), symbols, 8)
	if len(points) == 0 {
		return symbolPoint{}, false
	}
	return points[0], true
}

func collectOperationLatencies(t *testing.T, mgr *Manager, files []string, maxFiles int) map[string]*LatencyStats {
	t.Helper()
	if maxFiles > len(files) {
		maxFiles = len(files)
	}
	stats := map[string]*LatencyStats{
		"document_symbol": {Name: "DocumentSymbol"},
		"definition":      {Name: "Definition"},
		"references":      {Name: "References"},
		"hover":           {Name: "Hover"},
	}

	for i := 0; i < maxFiles; i++ {
		filePath := files[i]
		start := time.Now()
		symbols, err := mgr.DocumentSymbol(filePath)
		stats["document_symbol"].Add(time.Since(start))
		if err != nil || len(symbols) == 0 {
			continue
		}

		content, readErr := os.ReadFile(filePath)
		if readErr != nil {
			continue
		}
		points := collectSymbolPoints(string(content), symbols, 4)
		if len(points) == 0 {
			continue
		}
		point := points[0]

		start = time.Now()
		_, _ = mgr.Definition(filePath, point.line, point.character)
		stats["definition"].Add(time.Since(start))

		start = time.Now()
		_, _ = mgr.References(filePath, point.line, point.character, true)
		stats["references"].Add(time.Since(start))

		start = time.Now()
		_, _ = mgr.Hover(filePath, point.line, point.character)
		stats["hover"].Add(time.Since(start))
	}
	return stats
}

func collectP95Millis(stats map[string]*LatencyStats) map[string]float64 {
	out := make(map[string]float64, len(stats))
	for k, st := range stats {
		if st == nil || st.Count() == 0 {
			continue
		}
		out[k] = float64(st.P95().Milliseconds())
	}
	return out
}

func flattenSymbolNames(symbols []DocumentSymbol) []string {
	names := make([]string, 0, len(symbols))
	var walk func(nodes []DocumentSymbol)
	walk = func(nodes []DocumentSymbol) {
		for _, node := range nodes {
			names = append(names, node.Name)
			if len(node.Children) > 0 {
				walk(node.Children)
			}
		}
	}
	walk(symbols)
	return names
}

func containsSymbolName(symbols []DocumentSymbol, target string) bool {
	for _, name := range flattenSymbolNames(symbols) {
		if name == target {
			return true
		}
	}
	return false
}

func positionToOffset(content string, pos Position) (int, error) {
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
		return 0, fmt.Errorf("line out of range: %d >= %d", pos.Line, len(lineStarts))
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

func applyTextEdits(content string, edits []TextEdit) (string, error) {
	if len(edits) == 0 {
		return content, nil
	}

	type indexedEdit struct {
		start int
		end   int
		edit  TextEdit
	}

	indexed := make([]indexedEdit, 0, len(edits))
	for _, e := range edits {
		start, err := positionToOffset(content, e.Range.Start)
		if err != nil {
			return "", err
		}
		end, err := positionToOffset(content, e.Range.End)
		if err != nil {
			return "", err
		}
		if start > end {
			return "", fmt.Errorf("invalid edit range: start=%d end=%d", start, end)
		}
		indexed = append(indexed, indexedEdit{start: start, end: end, edit: e})
	}

	sort.Slice(indexed, func(i, j int) bool {
		if indexed[i].start == indexed[j].start {
			return indexed[i].end > indexed[j].end
		}
		return indexed[i].start > indexed[j].start
	})

	current := content
	for _, ie := range indexed {
		current = current[:ie.start] + ie.edit.NewText + current[ie.end:]
	}
	return current, nil
}

func editsForURI(edit *WorkspaceEdit, targetURI string) []TextEdit {
	if edit == nil {
		return nil
	}
	targetPath := normalizePath(uriToPath(targetURI))
	var out []TextEdit

	for uri, edits := range edit.Changes {
		if normalizePath(uriToPath(uri)) == targetPath {
			out = append(out, edits...)
		}
	}
	for _, dc := range edit.DocumentChanges {
		if normalizePath(uriToPath(dc.TextDocument.URI)) == targetPath {
			out = append(out, dc.Edits...)
		}
	}
	return out
}

func runningByLanguage(statuses []ServerStatus) map[string]bool {
	running := make(map[string]bool, len(statuses))
	for _, st := range statuses {
		running[st.Language] = st.Running
	}
	return running
}

func TestLSP_Stress_CrossFileDefinition(t *testing.T) {
	skipIfNotAvailable(t, "gopls")

	goRoot := filepath.Join(testCorpusDir, "go")
	mgr := NewManager(nil)
	mgr.SetRootURI("file://" + goRoot)
	defer mgr.StopAll()

	goFiles := collectFiles(t, goRoot, ".go")
	if len(goFiles) < 10 {
		t.Skip("not enough Go files")
	}
	opened := openSampleFiles(t, mgr, goFiles, 50)
	if len(opened) < 10 {
		t.Skip("not enough opened Go files")
	}

	time.Sleep(15 * time.Second)

	crossFileCount := 0
	sameFileCount := 0
	tested := 0

nextFile:
	for _, filePath := range opened {
		content, readErr := os.ReadFile(filePath)
		if readErr != nil {
			continue
		}

		symbols, err := mgr.DocumentSymbol(filePath)
		if err != nil || len(symbols) == 0 {
			continue
		}

		points := collectSymbolPoints(string(content), symbols, 24)
		for _, point := range points {
			locs, defErr := mgr.Definition(filePath, point.line, point.character)
			if defErr != nil || len(locs) == 0 {
				continue
			}
			tested++

			defPath := normalizePath(uriToPath(locs[0].URI))
			if defPath != normalizePath(filePath) {
				crossFileCount++
				t.Logf("  跨文件: %s -> %s", filepath.Base(filePath), filepath.Base(defPath))
				continue nextFile
			}
			sameFileCount++
		}
	}

	t.Logf("跨文件 Definition: tested=%d, cross=%d, same=%d", tested, crossFileCount, sameFileCount)
	if tested == 0 {
		t.Skip("no definition targets available for verification")
	}
	if crossFileCount == 0 {
		t.Fatalf("cross-file definition expected >=1, got 0 (tested=%d)", tested)
	}
}

func TestLSP_Stress_RenameApply(t *testing.T) {
	skipIfNotAvailable(t, "gopls")

	goRoot := filepath.Join(testCorpusDir, "go")
	mgr := NewManager(nil)
	mgr.SetRootURI("file://" + goRoot)
	defer mgr.StopAll()

	goFiles := collectFiles(t, goRoot, ".go")
	if len(goFiles) == 0 {
		t.Fatal("no Go files")
	}

	opened := openSampleFiles(t, mgr, goFiles, min(30, len(goFiles)))
	if len(opened) == 0 {
		t.Fatal("rename precondition failed: no files opened")
	}
	time.Sleep(10 * time.Second)

	for _, filePath := range opened {
		content, err := os.ReadFile(filePath)
		if err != nil {
			continue
		}
		symbols, err := mgr.DocumentSymbol(filePath)
		if err != nil || len(symbols) == 0 {
			continue
		}

		targets := pickRenameTargets(string(content), symbols, 12)
		for _, target := range targets {
			if len(target.name) == 0 || target.name[0] < 'A' || target.name[0] > 'Z' {
				continue
			}
			oldName := target.name
			newName := oldName + "V2"
			edit, renameErr := mgr.Rename(filePath, target.line, target.character, newName)
			if renameErr != nil || edit == nil {
				continue
			}

			totalEdits := countWorkspaceEdits(edit)
			if totalEdits == 0 {
				continue
			}

			for _, edits := range edit.Changes {
				for _, e := range edits {
					if e.NewText != newName {
						t.Fatalf("unexpected NewText in changes: got %q want %q", e.NewText, newName)
					}
				}
			}
			for _, dc := range edit.DocumentChanges {
				for _, e := range dc.Edits {
					if e.NewText != newName {
						t.Fatalf("unexpected NewText in documentChanges: got %q want %q", e.NewText, newName)
					}
				}
			}

			targetURI := pathToURI(filePath)
			targetEdits := editsForURI(edit, targetURI)
			if len(targetEdits) > 0 {
				updated, applyErr := applyTextEdits(string(content), targetEdits)
				if applyErr != nil {
					t.Fatalf("apply edits failed: %v", applyErr)
				}
				if !strings.Contains(updated, newName) {
					t.Fatalf("applied content missing new name %q", newName)
				}
			}

			t.Logf("Rename apply: %s %s -> %s (%d edits)", filepath.Base(filePath), oldName, newName, totalEdits)
			return
		}
	}

	t.Skip("no suitable rename target produced editable result")
}

func TestLSP_Stress_Latency(t *testing.T) {
	skipIfNotAvailable(t, "gopls")

	goRoot := filepath.Join(testCorpusDir, "go")
	mgr := NewManager(nil)
	mgr.SetRootURI("file://" + goRoot)
	defer mgr.StopAll()

	goFiles := collectFiles(t, goRoot, ".go")
	if len(goFiles) == 0 {
		t.Fatal("no Go files")
	}

	opened := openSampleFiles(t, mgr, goFiles, min(50, len(goFiles)))
	if len(opened) == 0 {
		t.Fatal("latency precondition failed: no files opened")
	}
	time.Sleep(12 * time.Second)

	stats := collectOperationLatencies(t, mgr, opened, len(opened))
	t.Log("=== 延迟统计 (Go) ===")
	for _, key := range []string{"document_symbol", "definition", "references", "hover"} {
		stats[key].Report(t)
	}
	if stats["document_symbol"].Count() == 0 {
		t.Fatal("no latency samples collected")
	}
}

func TestLSP_Stress_Concurrent(t *testing.T) {
	skipIfNotAvailable(t, "gopls")

	goRoot := filepath.Join(testCorpusDir, "go")
	mgr := NewManager(nil)
	mgr.SetRootURI("file://" + goRoot)
	defer mgr.StopAll()

	goFiles := collectFiles(t, goRoot, ".go")
	opened := openSampleFiles(t, mgr, goFiles, min(20, len(goFiles)))
	if len(opened) == 0 {
		t.Skip("no opened Go files")
	}
	time.Sleep(10 * time.Second)

	type target struct {
		file string
		line int
		col  int
	}
	var targets []target
	for _, filePath := range opened {
		point, ok := firstPointForFile(t, mgr, filePath)
		if !ok {
			continue
		}
		targets = append(targets, target{file: filePath, line: point.line, col: point.character})
	}
	if len(targets) == 0 {
		t.Skip("no valid symbol targets for concurrent test")
	}

	var wg sync.WaitGroup
	var panicCount atomic.Int64
	var opCount atomic.Int64
	var errCount atomic.Int64

	const concurrency = 8
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					panicCount.Add(1)
					t.Errorf("goroutine %d panicked: %v", id, r)
				}
			}()

			tgt := targets[id%len(targets)]
			for round := 0; round < 5; round++ {
				if _, err := mgr.DocumentSymbol(tgt.file); err != nil {
					errCount.Add(1)
				}
				opCount.Add(1)
				if _, err := mgr.Definition(tgt.file, tgt.line, tgt.col); err != nil {
					errCount.Add(1)
				}
				opCount.Add(1)
				if _, err := mgr.References(tgt.file, tgt.line, tgt.col, true); err != nil {
					errCount.Add(1)
				}
				opCount.Add(1)
				if _, err := mgr.Hover(tgt.file, tgt.line, tgt.col); err != nil {
					errCount.Add(1)
				}
				opCount.Add(1)
			}
		}(i)
	}
	wg.Wait()

	t.Logf("并发测试: concurrency=%d operations=%d panics=%d errors=%d", concurrency, opCount.Load(), panicCount.Load(), errCount.Load())
	if panicCount.Load() > 0 {
		t.Fatalf("concurrent stress observed %d panic(s)", panicCount.Load())
	}
}

func TestLSP_Stress_SymbolDepth(t *testing.T) {
	skipIfNotAvailable(t, "gopls")

	goRoot := filepath.Join(testCorpusDir, "go")
	mgr := NewManager(nil)
	mgr.SetRootURI("file://" + goRoot)
	defer mgr.StopAll()

	goFiles := collectFiles(t, goRoot, ".go")
	opened := openSampleFiles(t, mgr, goFiles, min(30, len(goFiles)))
	if len(opened) == 0 {
		t.Skip("no opened Go files")
	}
	time.Sleep(10 * time.Second)

	kindSet := make(map[SymbolKind]int)
	totalChildren := 0
	filesWithChildren := 0

	var walk func(nodes []DocumentSymbol) int
	walk = func(nodes []DocumentSymbol) int {
		childCount := 0
		for _, node := range nodes {
			kindSet[node.Kind]++
			if len(node.Children) > 0 {
				childCount += len(node.Children)
				childCount += walk(node.Children)
			}
		}
		return childCount
	}

	for _, filePath := range opened {
		symbols, err := mgr.DocumentSymbol(filePath)
		if err != nil {
			continue
		}
		children := walk(symbols)
		if children > 0 {
			filesWithChildren++
			totalChildren += children
		}
	}

	t.Log("=== SymbolKind 分布 ===")
	for kind, count := range kindSet {
		t.Logf("  %s (%d): %d", kind.String(), kind, count)
	}
	t.Logf("带 children 的文件: %d/%d", filesWithChildren, len(opened))
	t.Logf("总 children 数: %d", totalChildren)

	if len(kindSet) < 3 {
		t.Fatalf("symbol kind diversity too low: got %d, want >= 3", len(kindSet))
	}
}

func TestLSP_Stress_Completion(t *testing.T) {
	skipIfNotAvailable(t, "gopls")

	tmpDir := t.TempDir()
	must(t, os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte("module completiontest\ngo 1.22\n"), 0644))
	code := "package main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Prin\n}\n"
	goFile := filepath.Join(tmpDir, "main.go")
	must(t, os.WriteFile(goFile, []byte(code), 0644))

	mgr := NewManager(nil)
	mgr.SetRootURI("file://" + tmpDir)
	defer mgr.StopAll()

	if err := mgr.OpenFile(goFile, code); err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	time.Sleep(5 * time.Second)

	lines := strings.Split(code, "\n")
	line := 5
	col := strings.Index(lines[line], "Prin") + len("Prin")
	items, err := mgr.Completion(goFile, line, col)
	if err != nil {
		t.Fatalf("Completion: %v", err)
	}
	t.Logf("Completion: %d items at L%d:%d", len(items), line+1, col)
	for i, item := range items {
		if i >= 10 {
			t.Logf("  ... and %d more", len(items)-10)
			break
		}
		t.Logf("  [%d] %s (%s)", item.Kind, item.Label, item.Detail)
	}
	if len(items) == 0 {
		t.Fatalf("expected completion items, got 0")
	}
}

func TestLSP_Stress_DidChange(t *testing.T) {
	skipIfNotAvailable(t, "gopls")

	tmpDir := t.TempDir()
	must(t, os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte("module changetest\ngo 1.22\n"), 0644))
	oldCode := "package changetest\n\nfunc OldFunc() string { return \"old\" }\n"
	goFile := filepath.Join(tmpDir, "main.go")
	must(t, os.WriteFile(goFile, []byte(oldCode), 0644))

	mgr := NewManager(nil)
	mgr.SetRootURI("file://" + tmpDir)
	defer mgr.StopAll()

	if err := mgr.OpenFile(goFile, oldCode); err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	time.Sleep(3 * time.Second)

	beforeHover, _ := mgr.Hover(goFile, 2, 5)
	t.Logf("Before change hover available: %v", beforeHover != nil)

	newCode := "package changetest\n\nfunc NewFunc() int { return 42 }\n"
	if err := mgr.ChangeFile(goFile, 2, newCode); err != nil {
		t.Fatalf("ChangeFile: %v", err)
	}
	time.Sleep(3 * time.Second)

	var symbols []DocumentSymbol
	found := false
	for i := 0; i < 5; i++ {
		symbols, _ = mgr.DocumentSymbol(goFile)
		if containsSymbolName(symbols, "NewFunc") {
			found = true
			break
		}
		time.Sleep(1 * time.Second)
	}
	if !found {
		t.Fatalf("didChange not reflected in symbols: %+v", flattenSymbolNames(symbols))
	}

	afterHover, _ := mgr.Hover(goFile, 2, 5)
	if afterHover != nil {
		preview := afterHover.Contents.Value
		if len(preview) > 100 {
			preview = preview[:100]
		}
		t.Logf("After change hover: %s", preview)
	}
}

func TestLSP_Stress_ErrorRecovery(t *testing.T) {
	skipIfNotAvailable(t, "gopls")

	goRoot := filepath.Join(testCorpusDir, "go")
	mgr := NewManager(nil)
	mgr.SetRootURI("file://" + goRoot)
	defer mgr.StopAll()

	goFiles := collectFiles(t, goRoot, ".go")
	if len(goFiles) == 0 {
		t.Fatal("no Go files")
	}

	content, err := os.ReadFile(goFiles[0])
	if err != nil {
		t.Fatal(err)
	}
	_ = mgr.OpenFile(goFiles[0], string(content))
	time.Sleep(5 * time.Second)

	t.Run("UnknownFile", func(t *testing.T) {
		locs, err := mgr.Definition("/nonexistent/foo.go", 0, 0)
		if err != nil {
			t.Logf("error (acceptable): %v", err)
		}
		if locs != nil {
			t.Logf("locations for unknown file: %v", locs)
		}
	})

	t.Run("OutOfBounds", func(t *testing.T) {
		_, err := mgr.Definition(goFiles[0], 99999, 99999)
		t.Logf("OutOfBounds result: err=%v", err)
	})

	t.Run("BinaryFile", func(t *testing.T) {
		tmpBin := filepath.Join(t.TempDir(), "test.go")
		must(t, os.WriteFile(tmpBin, []byte{0x00, 0x01, 0x02}, 0644))
		err := mgr.OpenFile(tmpBin, string([]byte{0x00, 0x01, 0x02}))
		t.Logf("Binary open: err=%v", err)
	})

	t.Run("EmptyFile", func(t *testing.T) {
		tmpEmpty := filepath.Join(t.TempDir(), "empty.go")
		must(t, os.WriteFile(tmpEmpty, []byte(""), 0644))
		err := mgr.OpenFile(tmpEmpty, "")
		t.Logf("Empty open: err=%v", err)
		symbols, _ := mgr.DocumentSymbol(tmpEmpty)
		t.Logf("Empty symbols: %d", len(symbols))
	})

	t.Run("AfterStop", func(t *testing.T) {
		mgr2 := NewManager(nil)
		mgr2.SetRootURI("file://" + goRoot)
		defer mgr2.StopAll()

		content2, _ := os.ReadFile(goFiles[0])
		_ = mgr2.OpenFile(goFiles[0], string(content2))
		time.Sleep(2 * time.Second)
		mgr2.StopAll()

		_, err := mgr2.Definition(goFiles[0], 0, 0)
		t.Logf("After StopAll err=%v", err)
	})
}

func TestLSP_Stress_Lifecycle(t *testing.T) {
	skipIfNotAvailable(t, "gopls")

	goRoot := filepath.Join(testCorpusDir, "go")
	goFiles := collectFiles(t, goRoot, ".go")
	if len(goFiles) == 0 {
		t.Fatal("no Go files")
	}
	content, err := os.ReadFile(goFiles[0])
	if err != nil {
		t.Fatal(err)
	}

	mgr := NewManager(nil)
	mgr.SetRootURI("file://" + goRoot)
	defer mgr.StopAll()

	initial := runningByLanguage(mgr.Statuses())
	for lang, running := range initial {
		if running {
			t.Fatalf("expected initial %s running=false", lang)
		}
	}

	if err := mgr.OpenFile(goFiles[0], string(content)); err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	time.Sleep(3 * time.Second)
	if !runningByLanguage(mgr.Statuses())["go"] {
		t.Fatalf("expected go server running after OpenFile")
	}

	mgr.Reload()
	time.Sleep(500 * time.Millisecond)
	if runningByLanguage(mgr.Statuses())["go"] {
		t.Fatalf("expected go server stopped after Reload")
	}

	if err := mgr.OpenFile(goFiles[0], string(content)); err != nil {
		t.Fatalf("OpenFile after Reload: %v", err)
	}
	time.Sleep(3 * time.Second)
	if !runningByLanguage(mgr.Statuses())["go"] {
		t.Fatalf("expected go server running after reopen")
	}

	mgr.StopAll()
	time.Sleep(500 * time.Millisecond)
	if runningByLanguage(mgr.Statuses())["go"] {
		t.Fatalf("expected go server stopped after StopAll")
	}

	if err := mgr.OpenFile(goFiles[0], string(content)); err != nil {
		t.Fatalf("OpenFile after StopAll: %v", err)
	}
	time.Sleep(3 * time.Second)
	if !runningByLanguage(mgr.Statuses())["go"] {
		t.Fatalf("expected go server running after restart")
	}
}

func TestLSP_Stress_OpenCloseIdempotent(t *testing.T) {
	skipIfNotAvailable(t, "gopls")

	goRoot := filepath.Join(testCorpusDir, "go")
	goFiles := collectFiles(t, goRoot, ".go")
	if len(goFiles) == 0 {
		t.Fatal("no Go files")
	}
	content, err := os.ReadFile(goFiles[0])
	if err != nil {
		t.Fatal(err)
	}

	mgr := NewManager(nil)
	mgr.SetRootURI("file://" + goRoot)
	defer mgr.StopAll()

	for i := 0; i < 3; i++ {
		if err := mgr.OpenFile(goFiles[0], string(content)); err != nil {
			t.Fatalf("OpenFile attempt %d: %v", i+1, err)
		}
	}
	time.Sleep(3 * time.Second)

	for i := 0; i < 2; i++ {
		if err := mgr.CloseFile(goFiles[0]); err != nil {
			t.Fatalf("CloseFile attempt %d: %v", i+1, err)
		}
	}

	if err := mgr.OpenFile(goFiles[0], string(content)); err != nil {
		t.Fatalf("reopen after close: %v", err)
	}
	time.Sleep(2 * time.Second)

	symbols, err := mgr.DocumentSymbol(goFiles[0])
	if err != nil {
		t.Fatalf("DocumentSymbol after reopen: %v", err)
	}
	point := symbolPoint{line: 0, character: 0}
	if len(symbols) > 0 {
		point = symbolPoint{line: symbols[0].SelectionRange.Start.Line, character: symbols[0].SelectionRange.Start.Character}
	}
	if _, err := mgr.Hover(goFiles[0], point.line, point.character); err != nil {
		t.Fatalf("Hover after reopen: %v", err)
	}
}

func TestLSP_Stress_MissingServerBinary(t *testing.T) {
	tmpDir := t.TempDir()
	fooFile := filepath.Join(tmpDir, "sample.foo")
	must(t, os.WriteFile(fooFile, []byte("invalid language file"), 0644))
	must(t, os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte("module missingbinary\ngo 1.22\n"), 0644))
	goFile := filepath.Join(tmpDir, "main.go")
	goCode := "package main\n\nfunc main() {}\n"
	must(t, os.WriteFile(goFile, []byte(goCode), 0644))

	mgr := NewManager([]ServerConfig{
		{
			Language:   "broken",
			Command:    "__definitely_missing_lsp_binary__",
			Args:       nil,
			Extensions: []string{"foo"},
		},
		{
			Language:   "go",
			Command:    "gopls",
			Args:       nil,
			Extensions: []string{"go"},
		},
	})
	mgr.SetRootURI("file://" + tmpDir)
	defer mgr.StopAll()

	if err := mgr.OpenFile(fooFile, "broken"); err == nil {
		t.Fatalf("expected error for missing server binary")
	} else {
		t.Logf("missing binary returned expected error: %v", err)
	}

	if commandAvailable("gopls") {
		if err := mgr.OpenFile(goFile, goCode); err != nil {
			t.Fatalf("go open should remain available after missing binary error: %v", err)
		}
		time.Sleep(2 * time.Second)
		if _, err := mgr.DocumentSymbol(goFile); err != nil {
			t.Fatalf("go documentSymbol failed after missing binary error: %v", err)
		}
	} else {
		t.Log("gopls unavailable; skipped unaffected-language verification")
	}
}

func TestLSP_Stress_UnsupportedExtension(t *testing.T) {
	mgr := NewManager(nil)
	defer mgr.StopAll()

	tmpDir := t.TempDir()
	txtFile := filepath.Join(tmpDir, "note.txt")
	mdFile := filepath.Join(tmpDir, "note.md")

	if err := mgr.OpenFile(txtFile, "hello"); err != nil {
		t.Fatalf(".txt should be ignored without error: %v", err)
	}
	if err := mgr.OpenFile(mdFile, "# hello"); err != nil {
		t.Fatalf(".md should be ignored without error: %v", err)
	}
	if err := mgr.CloseFile(txtFile); err != nil {
		t.Fatalf("close .txt should be ignored without error: %v", err)
	}
}

func TestLSP_Stress_MultiLanguageIsolation(t *testing.T) {
	skipIfNotAvailable(t, "gopls")
	skipIfNotAvailable(t, "typescript-language-server")
	skipIfNotAvailable(t, "rust-analyzer")

	workRoot := t.TempDir()
	goFile := filepath.Join(workRoot, "main.go")
	tsFile := filepath.Join(workRoot, "index.ts")
	rsFile := filepath.Join(workRoot, "src", "main.rs")

	must(t, os.WriteFile(filepath.Join(workRoot, "go.mod"), []byte("module multiisol\n\ngo 1.22\n"), 0644))
	must(t, os.WriteFile(goFile, []byte("package main\n\nfunc add(a, b int) int { return a + b }\n\nfunc main() { _ = add(1, 2) }\n"), 0644))
	must(t, os.WriteFile(filepath.Join(workRoot, "tsconfig.json"), []byte("{\"compilerOptions\":{\"strict\":true},\"include\":[\"*.ts\"]}\n"), 0644))
	must(t, os.WriteFile(tsFile, []byte("function sum(a: number, b: number): number { return a + b; }\nconst value = sum(1, 2);\nconsole.log(value);\n"), 0644))
	must(t, os.MkdirAll(filepath.Dir(rsFile), 0755))
	must(t, os.WriteFile(filepath.Join(workRoot, "Cargo.toml"), []byte("[package]\nname = \"multiisol\"\nversion = \"0.1.0\"\nedition = \"2021\"\n"), 0644))
	must(t, os.WriteFile(rsFile, []byte("fn add(a: i32, b: i32) -> i32 { a + b }\nfn main() { let _v = add(1, 2); }\n"), 0644))

	mgr := NewManager(nil)
	mgr.SetRootURI("file://" + workRoot)
	defer mgr.StopAll()

	for _, filePath := range []string{goFile, tsFile, rsFile} {
		content, err := os.ReadFile(filePath)
		if err != nil {
			t.Fatalf("read %s: %v", filePath, err)
		}
		if err := mgr.OpenFile(filePath, string(content)); err != nil {
			t.Fatalf("OpenFile %s: %v", filePath, err)
		}
	}
	time.Sleep(15 * time.Second)

	allowedExts := map[string]map[string]bool{
		"go":         {".go": true},
		"typescript": {".ts": true, ".tsx": true, ".d.ts": true, ".js": true, ".jsx": true},
		"rust":       {".rs": true},
	}

	checkFile := func(filePath, language string) {
		t.Helper()
		point, ok := firstPointForFile(t, mgr, filePath)
		if !ok {
			t.Logf("skip query checks for %s (no point)", filepath.Base(filePath))
			return
		}

		if _, err := mgr.DocumentSymbol(filePath); err != nil {
			t.Errorf("%s documentSymbol: %v", filepath.Base(filePath), err)
		}
		locs, err := mgr.Definition(filePath, point.line, point.character)
		if err != nil {
			t.Errorf("%s definition: %v", filepath.Base(filePath), err)
		}
		if len(locs) > 0 {
			defExt := strings.ToLower(filepath.Ext(uriToPath(locs[0].URI)))
			if !allowedExts[language][defExt] {
				t.Errorf("%s definition crossed language boundary: got ext=%s language=%s", filepath.Base(filePath), defExt, language)
			}
		}
		if _, err := mgr.Hover(filePath, point.line, point.character); err != nil {
			t.Errorf("%s hover: %v", filepath.Base(filePath), err)
		}
	}

	checkFile(goFile, "go")
	checkFile(tsFile, "typescript")
	checkFile(rsFile, "rust")

	running := runningByLanguage(mgr.Statuses())
	for _, lang := range []string{"go", "typescript", "rust"} {
		if !running[lang] {
			t.Errorf("expected %s running=true", lang)
		}
	}
	for _, lang := range []string{"python", "c"} {
		if running[lang] {
			t.Errorf("expected %s running=false", lang)
		}
	}
}

func TestLSP_Stress_DiagnosticsQuality(t *testing.T) {
	tmpDir := t.TempDir()
	mgr := NewManager(nil)
	mgr.SetRootURI("file://" + tmpDir)
	defer mgr.StopAll()

	var mu sync.Mutex
	diagByLang := map[string]int{}
	severityCount := map[int]int{}
	totalDiags := 0
	invalidURI := 0
	invalidSeverity := 0

	mgr.SetDiagnosticHandler(func(uri string, diagnostics []Diagnostic) {
		mu.Lock()
		defer mu.Unlock()

		if uri == "" || uriToPath(uri) == "" {
			invalidURI++
		}
		path := uriToPath(uri)
		ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), ".")
		lang := ext
		switch ext {
		case "ts", "tsx", "js", "jsx":
			lang = "typescript"
		case "go":
			lang = "go"
		case "rs":
			lang = "rust"
		}

		for _, d := range diagnostics {
			totalDiags++
			diagByLang[lang]++
			sev := int(d.Severity)
			if sev < 0 || sev > 4 {
				invalidSeverity++
			}
			severityCount[sev]++
		}
	})

	opened := 0
	if commandAvailable("gopls") {
		goDir := filepath.Join(tmpDir, "go")
		must(t, os.MkdirAll(goDir, 0755))
		must(t, os.WriteFile(filepath.Join(goDir, "go.mod"), []byte("module diaggo\ngo 1.22\n"), 0644))
		goFile := filepath.Join(goDir, "bad.go")
		goCode := "package diaggo\n\nfunc bad() {\n\tvar x =\n}\n"
		must(t, os.WriteFile(goFile, []byte(goCode), 0644))
		if err := mgr.OpenFile(goFile, goCode); err == nil {
			opened++
		}
	}

	if commandAvailable("typescript-language-server") {
		tsDir := filepath.Join(tmpDir, "js")
		must(t, os.MkdirAll(tsDir, 0755))
		must(t, os.WriteFile(filepath.Join(tsDir, "tsconfig.json"), []byte(`{"compilerOptions":{"strict":true},"include":["*.ts"]}`), 0644))
		tsFile := filepath.Join(tsDir, "bad.ts")
		tsCode := "const value: number = \"oops\";\nconst fn = () => {\n"
		must(t, os.WriteFile(tsFile, []byte(tsCode), 0644))
		if err := mgr.OpenFile(tsFile, tsCode); err == nil {
			opened++
		}
	}

	if commandAvailable("rust-analyzer") {
		rsDir := filepath.Join(tmpDir, "rust")
		srcDir := filepath.Join(rsDir, "src")
		must(t, os.MkdirAll(srcDir, 0755))
		must(t, os.WriteFile(filepath.Join(rsDir, "Cargo.toml"), []byte("[package]\nname = \"diagrust\"\nversion = \"0.1.0\"\nedition = \"2021\"\n"), 0644))
		rsFile := filepath.Join(srcDir, "main.rs")
		rsCode := "fn main() {\n    let x: i32 = \"oops\";\n}\n"
		must(t, os.WriteFile(rsFile, []byte(rsCode), 0644))
		if err := mgr.OpenFile(rsFile, rsCode); err == nil {
			opened++
		}
	}

	if opened == 0 {
		t.Skip("no available language servers for diagnostics quality")
	}

	time.Sleep(12 * time.Second)

	mu.Lock()
	defer mu.Unlock()
	t.Logf("Diagnostics total: %d", totalDiags)
	t.Logf("Diagnostics by language: %+v", diagByLang)
	t.Logf("Diagnostics by severity: %+v", severityCount)

	if invalidURI > 0 {
		t.Fatalf("invalid diagnostics URI count: %d", invalidURI)
	}
	if invalidSeverity > 0 {
		t.Fatalf("invalid diagnostics severity count: %d", invalidSeverity)
	}
	if totalDiags == 0 {
		t.Fatalf("expected diagnostics callbacks, got 0")
	}
}

func TestLSP_Stress_UnicodePath(t *testing.T) {
	skipIfNotAvailable(t, "gopls")

	tmpDir := t.TempDir()
	workDir := filepath.Join(tmpDir, "工程 空间", "模块A")
	must(t, os.MkdirAll(workDir, 0755))
	must(t, os.WriteFile(filepath.Join(workDir, "go.mod"), []byte("module unicodepath\ngo 1.22\n"), 0644))

	code := "package unicodepath\n\nfunc NewUnicodeSymbol() string { return \"ok\" }\n"
	goFile := filepath.Join(workDir, "main.go")
	must(t, os.WriteFile(goFile, []byte(code), 0644))

	mgr := NewManager(nil)
	mgr.SetRootURI("file://" + workDir)
	defer mgr.StopAll()

	if err := mgr.OpenFile(goFile, code); err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	time.Sleep(4 * time.Second)

	symbols, err := mgr.DocumentSymbol(goFile)
	if err != nil {
		t.Fatalf("DocumentSymbol: %v", err)
	}
	if len(symbols) == 0 {
		t.Fatalf("expected symbols on unicode path file")
	}

	point := symbolPoint{line: symbols[0].SelectionRange.Start.Line, character: symbols[0].SelectionRange.Start.Character}
	_, _ = mgr.Hover(goFile, point.line, point.character)
	locs, _ := mgr.Definition(goFile, point.line, point.character)
	if len(locs) > 0 {
		gotPath := uriToPath(locs[0].URI)
		if gotPath == "" {
			t.Fatalf("definition URI is not reversible: %q", locs[0].URI)
		}
	}

	uri := pathToURI(goFile)
	roundtrip := uriToPath(uri)
	if normalizePath(roundtrip) != normalizePath(goFile) {
		t.Fatalf("uri roundtrip mismatch: got %q want %q", roundtrip, goFile)
	}
}

type goldenLanguageSummary struct {
	OpenRate       float64            `json:"open_rate"`
	SymbolRate     float64            `json:"symbol_rate"`
	DefinitionRate float64            `json:"definition_rate"`
	ReferenceRate  float64            `json:"reference_rate"`
	HoverRate      float64            `json:"hover_rate"`
	ErrorRate      float64            `json:"error_rate"`
	P95MS          map[string]float64 `json:"p95_ms,omitempty"`
}

type goldenBaseline struct {
	Version   int                              `json:"version"`
	Languages map[string]goldenLanguageSummary `json:"languages"`
}

func summaryFromResults(results []TestResult) goldenLanguageSummary {
	total := len(results)
	if total == 0 {
		return goldenLanguageSummary{}
	}
	var openOK, symbolOK, defOK, refOK, hoverOK, filesWithErrors int
	for _, r := range results {
		if r.OpenOK {
			openOK++
		}
		if r.SymbolCount > 0 {
			symbolOK++
		}
		if r.DefinitionOK {
			defOK++
		}
		if r.ReferencesOK {
			refOK++
		}
		if r.HoverOK {
			hoverOK++
		}
		if len(r.Errors) > 0 {
			filesWithErrors++
		}
	}
	toRate := func(v int) float64 { return float64(v) / float64(total) }
	return goldenLanguageSummary{
		OpenRate:       toRate(openOK),
		SymbolRate:     toRate(symbolOK),
		DefinitionRate: toRate(defOK),
		ReferenceRate:  toRate(refOK),
		HoverRate:      toRate(hoverOK),
		ErrorRate:      toRate(filesWithErrors),
	}
}

func loadResults(path string) ([]TestResult, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var results []TestResult
	if err := json.Unmarshal(data, &results); err != nil {
		return nil, err
	}
	return results, nil
}

func baselinePath() string {
	path := filepath.Join("testdata", "lsp_stress_baseline.json")
	if _, err := os.Stat(path); err == nil {
		return path
	}
	return filepath.Join("internal", "lsp", "testdata", "lsp_stress_baseline.json")
}

func languageServerCommand(language string) string {
	switch language {
	case "go":
		return "gopls"
	case "typescript":
		return "typescript-language-server"
	case "rust":
		return "rust-analyzer"
	default:
		return ""
	}
}

func readBaseline(path string) (goldenBaseline, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return goldenBaseline{}, err
	}
	var baseline goldenBaseline
	if err := json.Unmarshal(data, &baseline); err != nil {
		return goldenBaseline{}, err
	}
	if baseline.Languages == nil {
		baseline.Languages = map[string]goldenLanguageSummary{}
	}
	return baseline, nil
}

func writeBaseline(path string, baseline goldenBaseline) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(baseline, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func compareLanguageSummary(t *testing.T, language string, base, cur goldenLanguageSummary) {
	t.Helper()
	compareRate := func(metric string, baseVal, curVal float64) {
		if baseVal <= 0 {
			return
		}
		allow := baseVal - 0.05
		if curVal < allow {
			t.Errorf("%s %s regressed beyond 5pp: current=%.1f%% baseline=%.1f%%", language, metric, curVal*100, baseVal*100)
		}
	}
	compareRate("open_rate", base.OpenRate, cur.OpenRate)
	compareRate("symbol_rate", base.SymbolRate, cur.SymbolRate)
	compareRate("definition_rate", base.DefinitionRate, cur.DefinitionRate)
	compareRate("reference_rate", base.ReferenceRate, cur.ReferenceRate)
	compareRate("hover_rate", base.HoverRate, cur.HoverRate)
	if base.ErrorRate > 0 && cur.ErrorRate > base.ErrorRate+0.05 {
		t.Errorf("%s error_rate regressed beyond +5pp: current=%.1f%% baseline=%.1f%%", language, cur.ErrorRate*100, base.ErrorRate*100)
	}

	for metric, baseP95 := range base.P95MS {
		if baseP95 <= 0 {
			continue
		}
		curP95, ok := cur.P95MS[metric]
		if !ok {
			t.Errorf("%s missing p95 metric %s", language, metric)
			continue
		}
		limit := baseP95 * 1.30
		if curP95 > limit {
			t.Errorf("%s %s p95 regressed beyond 30%%: current=%.1fms baseline=%.1fms", language, metric, curP95, baseP95)
		}
	}
}

func TestLSP_Stress_GoldenBaseline(t *testing.T) {
	current := goldenBaseline{
		Version:   1,
		Languages: map[string]goldenLanguageSummary{},
	}

	if commandAvailable("gopls") {
		runStressGo(t)
		goResults, err := loadResults(filepath.Join(testCorpusDir, "go-results.json"))
		if err != nil {
			t.Fatalf("load go results: %v", err)
		}
		goSummary := summaryFromResults(goResults)

		goRoot := filepath.Join(testCorpusDir, "go")
		mgr := NewManager(nil)
		mgr.SetRootURI("file://" + goRoot)
		goFiles := collectFiles(t, goRoot, ".go")
		opened := openSampleFiles(t, mgr, goFiles, min(30, len(goFiles)))
		time.Sleep(10 * time.Second)
		stats := collectOperationLatencies(t, mgr, opened, len(opened))
		mgr.StopAll()
		goSummary.P95MS = collectP95Millis(stats)
		current.Languages["go"] = goSummary
	}

	if commandAvailable("typescript-language-server") {
		runStressJS(t)
		jsResults, err := loadResults(filepath.Join(testCorpusDir, "js-results.json"))
		if err != nil {
			t.Fatalf("load js results: %v", err)
		}
		current.Languages["typescript"] = summaryFromResults(jsResults)
	}

	if commandAvailable("rust-analyzer") {
		runStressRust(t)
		rustResults, err := loadResults(filepath.Join(testCorpusDir, "rust-results.json"))
		if err != nil {
			t.Fatalf("load rust results: %v", err)
		}
		current.Languages["rust"] = summaryFromResults(rustResults)
	}

	if len(current.Languages) == 0 {
		t.Skip("no language servers available for golden baseline")
	}

	path := baselinePath()
	baseline, err := readBaseline(path)
	if os.IsNotExist(err) {
		if writeErr := writeBaseline(path, current); writeErr != nil {
			t.Fatalf("write generated baseline: %v", writeErr)
		}
		t.Fatalf("baseline missing; generated %s, please review and re-run", path)
	}
	if err != nil {
		t.Fatalf("read baseline: %v", err)
	}

	for language, baseSummary := range baseline.Languages {
		curSummary, ok := current.Languages[language]
		if !ok {
			cmd := languageServerCommand(language)
			if cmd != "" && commandAvailable(cmd) {
				t.Errorf("language %s available via %s but missing in current baseline run", language, cmd)
				continue
			}
			t.Logf("language %s unavailable in current environment; skipping baseline compare", language)
			continue
		}
		compareLanguageSummary(t, language, baseSummary, curSummary)
	}
}
