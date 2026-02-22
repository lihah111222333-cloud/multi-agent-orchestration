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
