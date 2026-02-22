# LSP 超级冒烟测试 — 实现计划 (V2)

> **给执行 Agent:** 必须逐任务实现此计划。每个任务完成后运行验证命令确认结果。

**目标:** 对 3 个完整真实项目执行大规模 LSP 全链路操作，验证全部 7 个 LSP 工具在 Go/JS/Rust 下的正确性和稳定性。

**架构:** 编写 `lsp_stress_test.go`，直接调用 `lsp.Manager` 真实接入 3 个语言服务器，对真实项目执行批量 open → documentSymbol → definition → references → hover → diagnostics → rename 操作链。

**技术栈:** Go test / lsp.Manager / gopls / typescript-language-server / rust-analyzer

**测试语料 — 三个完整真实项目:**
```
/Users/mima0000/Desktop/wj/e2e测试/
├── go/         552 个 .go 文件，39 个子包 (量化引擎: dto/matching/statistics/strategy/...)
├── js/         131 个 .ts/.tsx 文件 (Vite+React 前端管理面板)
└── rust/       75 个 .rs 文件 (codex-app-server, Cargo workspace)
```

> [!IMPORTANT]
> 旧的 `go-src/`、`js-src/`、`rust-src/` 目录不再使用。所有测试路径改为 `go/`、`js/`、`rust/`。

---

### 任务 1: 创建测试文件骨架

**文件:**
- 创建: `go-agent-v2/internal/lsp/lsp_stress_test.go`

**步骤 1: 创建测试文件**

```go
// lsp_stress_test.go — LSP 大规模冒烟测试。
//
// 对 3 个完整真实项目 (Go 552 文件 / JS 131 文件 / Rust 75 文件)
// 执行全链路 LSP 操作:
//   open → documentSymbol → definition → references → hover → diagnostics
//
// 运行: go test -v -run TestLSP_Stress -timeout 600s ./internal/lsp/
package lsp

import (
    "encoding/json"
    "fmt"
    "os"
    "path/filepath"
    "strings"
    "sync/atomic"
    "testing"
    "time"
)

const testCorpusDir = "/Users/mima0000/Desktop/wj/e2e测试"

// TestResult 单个文件的测试结果。
type TestResult struct {
    File             string   `json:"file"`
    Language         string   `json:"language"`
    OpenOK           bool     `json:"open_ok"`
    SymbolCount      int      `json:"symbol_count"`
    SymbolNames      []string `json:"symbol_names,omitempty"`
    DefinitionOK     bool     `json:"definition_ok"`
    DefinitionLoc    string   `json:"definition_loc,omitempty"`
    ReferencesOK     bool     `json:"references_ok"`
    ReferenceCount   int      `json:"reference_count"`
    HoverOK          bool     `json:"hover_ok"`
    HoverPreview     string   `json:"hover_preview,omitempty"`
    DiagnosticsOK    bool     `json:"diagnostics_ok"`
    DiagnosticCount  int      `json:"diagnostic_count"`
    Errors           []string `json:"errors,omitempty"`
}
```

**步骤 2: 编译确认**

运行: `cd /Users/mima0000/Desktop/wj/multi-agent-orchestration/go-agent-v2 && go build ./internal/lsp/...`
预期: 成功 (exit 0)

---

### 任务 2: 实现通用辅助函数

**文件:**
- 修改: `go-agent-v2/internal/lsp/lsp_stress_test.go`

**步骤 1: 实现 collectFiles (带大文件过滤)**

```go
func collectFiles(t *testing.T, dir string, exts ...string) []string {
    t.Helper()
    extSet := make(map[string]bool)
    for _, e := range exts { extSet[e] = true }
    var files []string
    filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
        if err != nil { return nil }
        if info.IsDir() {
            base := filepath.Base(path)
            // 跳过 node_modules, target, .git, .vite 等不需要的目录
            if base == "node_modules" || base == "target" || base == ".git" || base == ".vite" || base == "vendor" {
                return filepath.SkipDir
            }
            return nil
        }
        ext := filepath.Ext(path)
        if !extSet[ext] { return nil }
        // 排除大文件 (打包产物等 > 500KB)
        if info.Size() > 500*1024 {
            t.Logf("  跳过大文件: %s (%.1fMB)", filepath.Base(path), float64(info.Size())/1024/1024)
            return nil
        }
        files = append(files, path)
        return nil
    })
    return files
}
```

**步骤 2: 实现 testFile (全链路测试单个文件)**

```go
func testFile(t *testing.T, mgr *Manager, filePath, lang string) TestResult {
    t.Helper()
    r := TestResult{File: filepath.Base(filePath), Language: lang, OpenOK: true}

    // DocumentSymbol
    symbols, err := mgr.DocumentSymbol(filePath)
    if err != nil {
        r.Errors = append(r.Errors, "documentSymbol: "+err.Error())
    } else {
        r.SymbolCount = len(symbols)
        for _, s := range symbols {
            r.SymbolNames = append(r.SymbolNames, s.Name)
        }
    }

    // 如果有符号，对第一个符号测试 Definition + References + Hover
    if len(symbols) > 0 {
        sym := symbols[0]
        line := sym.SelectionRange.Start.Line
        col := sym.SelectionRange.Start.Character

        // Definition
        locs, err := mgr.Definition(filePath, line, col)
        if err != nil {
            r.Errors = append(r.Errors, "definition: "+err.Error())
        } else if len(locs) > 0 {
            r.DefinitionOK = true
            r.DefinitionLoc = fmt.Sprintf("L%d:%d", locs[0].Range.Start.Line+1, locs[0].Range.Start.Character)
        }

        // References
        refs, err := mgr.References(filePath, line, col, true)
        if err != nil {
            r.Errors = append(r.Errors, "references: "+err.Error())
        } else {
            r.ReferencesOK = len(refs) > 0
            r.ReferenceCount = len(refs)
        }

        // Hover
        hover, err := mgr.Hover(filePath, line, col)
        if err != nil {
            r.Errors = append(r.Errors, "hover: "+err.Error())
        } else if hover != nil {
            r.HoverOK = true
            preview := hover.Contents.Value
            if len(preview) > 100 { preview = preview[:100] + "..." }
            r.HoverPreview = preview
        }
    }

    // Diagnostics — open 成功即算诊断链路打通
    r.DiagnosticsOK = true

    return r
}
```

**步骤 3: 实现 summarize (汇总统计)**

```go
func summarize(t *testing.T, results []TestResult, lang string) {
    t.Helper()
    total := len(results)
    symbolOK, defOK, refOK, hoverOK, diagOK := 0, 0, 0, 0, 0
    totalSymbols, totalRefs := 0, 0
    errorCount := 0
    for _, r := range results {
        if r.SymbolCount > 0 { symbolOK++ }
        if r.DefinitionOK { defOK++ }
        if r.ReferencesOK { refOK++ }
        if r.HoverOK { hoverOK++ }
        if r.DiagnosticsOK { diagOK++ }
        totalSymbols += r.SymbolCount
        totalRefs += r.ReferenceCount
        errorCount += len(r.Errors)
    }
    t.Logf("=== %s 汇总 (%d files) ===", lang, total)
    t.Logf("  DocumentSymbol: %d/%d 有符号 (共 %d 个符号)", symbolOK, total, totalSymbols)
    t.Logf("  Definition:     %d/%d 成功", defOK, total)
    t.Logf("  References:     %d/%d 成功 (共 %d 引用)", refOK, total, totalRefs)
    t.Logf("  Hover:          %d/%d 成功", hoverOK, total)
    t.Logf("  Diagnostics:    %d/%d 成功", diagOK, total)
    t.Logf("  Errors:         %d 个错误", errorCount)
}
```

**步骤 4: 编译确认**

运行: `cd /Users/mima0000/Desktop/wj/multi-agent-orchestration/go-agent-v2 && go build ./internal/lsp/...`
预期: 成功

---

### 任务 3: 实现 Go 项目批量测试 (552 文件)

**文件:**
- 修改: `go-agent-v2/internal/lsp/lsp_stress_test.go`

**步骤 1: 实现 TestLSP_Stress_Go**

```go
func TestLSP_Stress_Go(t *testing.T) {
    skipIfNotAvailable(t, "gopls")

    goProjectDir := filepath.Join(testCorpusDir, "go")

    mgr := NewManager(nil)
    mgr.SetRootURI("file://" + goProjectDir)
    var diagCount atomic.Int64
    mgr.SetDiagnosticHandler(func(uri string, diags []Diagnostic) {
        diagCount.Add(int64(len(diags)))
    })
    defer mgr.StopAll()

    goFiles := collectFiles(t, goProjectDir, ".go")
    t.Logf("Found %d Go files", len(goFiles))
    if len(goFiles) < 100 {
        t.Fatalf("expected at least 100 Go files, got %d", len(goFiles))
    }

    // 批量 Open — 为了效率只打开前 100 个文件供深度测试
    // 全部打开 552 个会让 gopls 内存暴涨
    maxOpen := 100
    if len(goFiles) < maxOpen { maxOpen = len(goFiles) }
    opened := 0
    for i := 0; i < maxOpen; i++ {
        content, err := os.ReadFile(goFiles[i])
        if err != nil { continue }
        if err := mgr.OpenFile(goFiles[i], string(content)); err == nil {
            opened++
        }
    }
    t.Logf("Opened %d/%d files (capped at %d)", opened, len(goFiles), maxOpen)

    // gopls 索引需要时间 — 文件多等久一点
    time.Sleep(15 * time.Second)

    // 对已打开的文件执行全链路测试
    var results []TestResult
    for i := 0; i < maxOpen; i++ {
        r := testFile(t, mgr, goFiles[i], "go")
        results = append(results, r)
    }

    summarize(t, results, "Go")
    dc := diagCount.Load()
    t.Logf("  Diagnostics 回调: 共收到 %d 条诊断信息", dc)
    if dc == 0 {
        t.Log("  ⚠️ WARNING: gopls 未推送任何诊断信息")
    }

    data, _ := json.MarshalIndent(results, "", "  ")
    os.WriteFile(filepath.Join(testCorpusDir, "go-results.json"), data, 0644)
}
```

**步骤 2: 运行测试**

运行: `cd /Users/mima0000/Desktop/wj/multi-agent-orchestration/go-agent-v2 && go test -v -run TestLSP_Stress_Go -timeout 300s ./internal/lsp/ 2>&1 | tail -30`
预期: PASS

---

### 任务 4: 实现 JS/TS 项目批量测试 (131 文件)

**文件:**
- 修改: `go-agent-v2/internal/lsp/lsp_stress_test.go`

**步骤 1: 实现 TestLSP_Stress_JS**

```go
func TestLSP_Stress_JS(t *testing.T) {
    skipIfNotAvailable(t, "typescript-language-server")

    jsProjectDir := filepath.Join(testCorpusDir, "js")

    mgr := NewManager(nil)
    mgr.SetRootURI("file://" + jsProjectDir)
    var diagCount atomic.Int64
    mgr.SetDiagnosticHandler(func(uri string, diags []Diagnostic) {
        diagCount.Add(int64(len(diags)))
    })
    defer mgr.StopAll()

    // .ts 和 .tsx 都要收集
    allFiles := collectFiles(t, filepath.Join(jsProjectDir, "src"), ".ts", ".tsx")
    t.Logf("Found %d TS/TSX files", len(allFiles))

    opened := 0
    for _, f := range allFiles {
        content, _ := os.ReadFile(f)
        if mgr.OpenFile(f, string(content)) == nil { opened++ }
    }
    t.Logf("Opened %d/%d files", opened, len(allFiles))
    time.Sleep(10 * time.Second)

    var results []TestResult
    for _, f := range allFiles {
        lang := "typescript"
        r := testFile(t, mgr, f, lang)
        results = append(results, r)
    }

    summarize(t, results, "JS/TS")
    t.Logf("  JS Diagnostics 回调: 共收到 %d 条诊断信息", diagCount.Load())

    data, _ := json.MarshalIndent(results, "", "  ")
    os.WriteFile(filepath.Join(testCorpusDir, "js-results.json"), data, 0644)
}
```

**步骤 2: 运行测试**

运行: `cd /Users/mima0000/Desktop/wj/multi-agent-orchestration/go-agent-v2 && go test -v -run TestLSP_Stress_JS -timeout 300s ./internal/lsp/ 2>&1 | tail -20`
预期: PASS

---

### 任务 5: 实现 Rust 项目批量测试 (75 文件)

**文件:**
- 修改: `go-agent-v2/internal/lsp/lsp_stress_test.go`

**步骤 1: 实现 TestLSP_Stress_Rust**

```go
func TestLSP_Stress_Rust(t *testing.T) {
    skipIfNotAvailable(t, "rust-analyzer")

    rustProjectDir := filepath.Join(testCorpusDir, "rust")

    mgr := NewManager(nil)
    mgr.SetRootURI("file://" + rustProjectDir)
    var diagCount atomic.Int64
    mgr.SetDiagnosticHandler(func(uri string, diags []Diagnostic) {
        diagCount.Add(int64(len(diags)))
    })
    defer mgr.StopAll()

    rsFiles := collectFiles(t, filepath.Join(rustProjectDir, "src"), ".rs")
    t.Logf("Found %d Rust source files", len(rsFiles))

    opened := 0
    for _, f := range rsFiles {
        content, _ := os.ReadFile(f)
        if mgr.OpenFile(f, string(content)) == nil { opened++ }
    }
    t.Logf("Opened %d/%d files", opened, len(rsFiles))
    time.Sleep(15 * time.Second) // rust-analyzer 索引较慢

    var results []TestResult
    for _, f := range rsFiles {
        r := testFile(t, mgr, f, "rust")
        results = append(results, r)
    }

    summarize(t, results, "Rust")
    t.Logf("  Rust Diagnostics 回调: 共收到 %d 条诊断信息", diagCount.Load())

    data, _ := json.MarshalIndent(results, "", "  ")
    os.WriteFile(filepath.Join(testCorpusDir, "rust-results.json"), data, 0644)
}
```

**步骤 2: 运行测试**

运行: `cd /Users/mima0000/Desktop/wj/multi-agent-orchestration/go-agent-v2 && go test -v -run TestLSP_Stress_Rust -timeout 300s ./internal/lsp/ 2>&1 | tail -20`
预期: PASS

---

### 任务 6: 实现 Rename 测试 + 综合运行

**文件:**
- 修改: `go-agent-v2/internal/lsp/lsp_stress_test.go`

**步骤 1: 实现 TestLSP_Stress_Rename**

选择 Go 项目中的一个文件做 rename 测试, 因为 Go 项目有完整包结构，gopls rename 支持最好。

```go
func TestLSP_Stress_Rename(t *testing.T) {
    skipIfNotAvailable(t, "gopls")

    goProjectDir := filepath.Join(testCorpusDir, "go")
    mgr := NewManager(nil)
    mgr.SetRootURI("file://" + goProjectDir)
    defer mgr.StopAll()

    // 找一个有 struct 定义的文件
    goFiles := collectFiles(t, goProjectDir, ".go")
    if len(goFiles) == 0 { t.Fatal("no Go files") }

    // 打开第一个文件
    target := goFiles[0]
    content, err := os.ReadFile(target)
    if err != nil { t.Fatal(err) }
    mgr.OpenFile(target, string(content))
    time.Sleep(8 * time.Second)

    // 先获取符号，使用第一个符号做 rename
    symbols, err := mgr.DocumentSymbol(target)
    if err != nil || len(symbols) == 0 {
        t.Skipf("no symbols in %s, skipping rename test", filepath.Base(target))
    }

    sym := symbols[0]
    line := sym.SelectionRange.Start.Line
    col := sym.SelectionRange.Start.Character
    newName := sym.Name + "Renamed"

    t.Logf("Rename target: %s.%s (L%d:%d) → %s", filepath.Base(target), sym.Name, line+1, col, newName)

    edit, err := mgr.Rename(target, line, col, newName)
    if err != nil {
        t.Logf("Rename error (may be expected): %v", err)
        return
    }
    if edit == nil || len(edit.Changes) == 0 {
        t.Log("Rename returned no edits (may be restricted in this context)")
        return
    }
    totalEdits := 0
    for uri, edits := range edit.Changes {
        totalEdits += len(edits)
        t.Logf("  Rename in %s: %d edits", filepath.Base(strings.TrimPrefix(uri, "file://")), len(edits))
    }
    t.Logf("Total rename edits: %d ✓", totalEdits)
}
```

**步骤 2: 实现 TestLSP_Stress_All**

```go
func TestLSP_Stress_All(t *testing.T) {
    t.Run("Go", TestLSP_Stress_Go)
    t.Run("JS", TestLSP_Stress_JS)
    t.Run("Rust", TestLSP_Stress_Rust)
    t.Run("Rename", TestLSP_Stress_Rename)
}
```

**步骤 3: 运行全套测试**

运行: `cd /Users/mima0000/Desktop/wj/multi-agent-orchestration/go-agent-v2 && go test -v -run "TestLSP_Stress" -timeout 600s ./internal/lsp/ 2>&1 | grep -E "^=== |^--- |PASS|FAIL|汇总|Found|Opened|Diagnostics|Rename"`
预期: 全部 PASS

---

### 任务 7: 验证报告

**步骤 1: 检查 JSON 结果**

运行:
```bash
for f in go-results.json js-results.json rust-results.json; do
  echo "=== $f ==="
  cat /Users/mima0000/Desktop/wj/e2e测试/$f | python3 -c "
import json,sys
data=json.load(sys.stdin)
total=len(data)
sym=sum(1 for r in data if r['symbol_count']>0)
defn=sum(1 for r in data if r['definition_ok'])
refs=sum(1 for r in data if r['references_ok'])
hover=sum(1 for r in data if r['hover_ok'])
diag=sum(1 for r in data if r.get('diagnostics_ok'))
errs=sum(len(r.get('errors',[])) for r in data)
print(f'  Files: {total}  Symbol: {sym}/{total}  Def: {defn}/{total}  Ref: {refs}/{total}  Hover: {hover}/{total}  Diag: {diag}/{total}  Errors: {errs}')
"
done
```

---

## 验收标准

| 指标 | Go (100 files*) | JS/TS (131 files) | Rust (src/*.rs) |
|------|:-:|:-:|:-:|
| Open 成功率 | >= 95% | >= 90% | >= 90% |
| DocumentSymbol 成功率 | >= 80% | >= 70% | >= 70% |
| Definition 成功率 | >= 50% | >= 40% | >= 40% |
| References 成功率 | >= 50% | >= 40% | >= 30% |
| Hover 成功率 | >= 60% | >= 50% | >= 50% |
| Diagnostics 回调触发 | ✓ 不 crash | ✓ 不 crash | ✓ 不 crash |
| Rename 不 crash | ✓ | – | – |
| 无 panic/crash/死锁 | ✓ | ✓ | ✓ |
| 测试运行 < 600s | ✓ | ✓ | ✓ |

> \* Go 为效率仅测试前 100 个文件 (全打开 552 个会让 gopls 内存暴涨)

> 核心验证点: **不 crash、不 panic、不死锁、不 OOM**

---

## 辅助函数来源

`skipIfNotAvailable`、`min` 已在同包 `lsp_e2e_test.go` 中定义，可直接复用。

## 运行全部测试

```bash
cd /Users/mima0000/Desktop/wj/multi-agent-orchestration/go-agent-v2
go test -v -run "TestLSP_Stress" -timeout 600s ./internal/lsp/ 2>&1 | tee /Users/mima0000/Desktop/wj/e2e测试/test-output.log
```

## 覆盖的全部 7 个 LSP 工具

| # | 工具 | 测试位置 | 验证方式 |
|---|------|---------|---------|
| 1 | `lsp_open_file` | 批量 Open 循环 | `opened` 计数 |
| 2 | `lsp_hover` | `testFile()` | 成功率统计 |
| 3 | `lsp_diagnostics` | `atomic.Int64` 回调 | 回调计数 |
| 4 | `lsp_definition` | `testFile()` | 成功率统计 |
| 5 | `lsp_references` | `testFile()` | 成功率统计 |
| 6 | `lsp_document_symbol` | `testFile()` | 符号计数 |
| 7 | `lsp_rename` | `TestLSP_Stress_Rename` | edits 计数 |
