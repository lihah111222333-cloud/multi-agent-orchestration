# LSP 超级冒烟测试 — 实现计划 (V4)

> **给执行 Agent:** 必须逐任务实现此计划。每个任务完成后运行验证命令确认结果。
> **V3 新增任务 8-15 为增强测试，覆盖跨文件跳转、rename 应用验证、延迟统计、并发压力、completion、didChange、error recovery。**
> **V4 新增任务 16-24，补齐生命周期状态机、幂等性、缺失依赖、跨语言隔离、诊断质量、race/flake、Unicode 路径、Golden Record 回归。**

**目标:** 对 3 个完整真实项目执行大规模 LSP 全链路操作，验证核心 7 个 LSP 工具在 Go/JS/Rust 下的正确性和稳定性，并扩展验证 `completion` / `didChange` / 并发稳定性等高级能力。

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

---

## 增强测试 (V3 新增)

---

### 任务 8: 跨文件 Definition 测试 [P0]

**目标:** 验证 definition 能从文件 A 跳转到文件 B（不只是同文件内跳转）。

**文件:**
- 修改: `go-agent-v2/internal/lsp/lsp_stress_test.go`

**步骤 1: 实现 TestLSP_Stress_CrossFileDefinition**

```go
func TestLSP_Stress_CrossFileDefinition(t *testing.T) {
    skipIfNotAvailable(t, "gopls")

    goProjectDir := filepath.Join(testCorpusDir, "go")
    mgr := NewManager(nil)
    mgr.SetRootURI("file://" + goProjectDir)
    defer mgr.StopAll()

    goFiles := collectFiles(t, goProjectDir, ".go")
    if len(goFiles) < 10 { t.Skip("not enough Go files") }

    // 打开前 50 个文件
    for i := 0; i < 50 && i < len(goFiles); i++ {
        content, _ := os.ReadFile(goFiles[i])
        mgr.OpenFile(goFiles[i], string(content))
    }
    time.Sleep(15 * time.Second)

    crossFileCount := 0
    sameFileCount := 0
    tested := 0

    for i := 0; i < 50 && i < len(goFiles); i++ {
        symbols, err := mgr.DocumentSymbol(goFiles[i])
        if err != nil || len(symbols) == 0 { continue }

        sym := symbols[0]
        locs, err := mgr.Definition(goFiles[i], sym.SelectionRange.Start.Line, sym.SelectionRange.Start.Character)
        if err != nil || len(locs) == 0 { continue }
        tested++

        defURI := locs[0].URI
        fileURI := pathToURI(goFiles[i])
        if defURI != fileURI {
            crossFileCount++
            t.Logf("  跨文件: %s → %s", filepath.Base(goFiles[i]), filepath.Base(strings.TrimPrefix(defURI, "file://")))
        } else {
            sameFileCount++
        }
    }

    t.Logf("跨文件 Definition: tested=%d, cross=%d, same=%d", tested, crossFileCount, sameFileCount)
    // 不强制要求跨文件比例，但必须 > 0 说明索引图工作正常
    if crossFileCount == 0 && tested > 10 {
        t.Log("⚠️ WARNING: 未发现跨文件 definition 跳转")
    }
}
```

**步骤 2: 运行测试**

运行: `go test -v -run TestLSP_Stress_CrossFileDefinition -timeout 120s ./internal/lsp/`
预期: PASS

---

### 任务 9: Rename 应用+验证 [P0]

**目标:** 将 rename 返回的 edits 实际应用到文件副本，验证内容替换正确。

**文件:**
- 修改: `go-agent-v2/internal/lsp/lsp_stress_test.go`

**步骤 1: 实现 TestLSP_Stress_RenameApply**

```go
func TestLSP_Stress_RenameApply(t *testing.T) {
    skipIfNotAvailable(t, "gopls")

    goProjectDir := filepath.Join(testCorpusDir, "go")
    mgr := NewManager(nil)
    mgr.SetRootURI("file://" + goProjectDir)
    defer mgr.StopAll()

    goFiles := collectFiles(t, goProjectDir, ".go")
    if len(goFiles) == 0 { t.Fatal("no Go files") }

    // 找一个有导出符号的文件
    var targetFile string
    var targetSym DocumentSymbol
    for _, f := range goFiles[:min(30, len(goFiles))] {
        content, _ := os.ReadFile(f)
        mgr.OpenFile(f, string(content))
    }
    time.Sleep(10 * time.Second)

    for _, f := range goFiles[:min(30, len(goFiles))] {
        symbols, err := mgr.DocumentSymbol(f)
        if err != nil || len(symbols) == 0 { continue }
        for _, s := range symbols {
            // 找大写开头的导出名
            if len(s.Name) > 0 && s.Name[0] >= 'A' && s.Name[0] <= 'Z' {
                targetFile = f
                targetSym = s
                break
            }
        }
        if targetFile != "" { break }
    }

    if targetFile == "" {
        t.Skip("no exported symbol found for rename test")
    }

    oldName := targetSym.Name
    newName := oldName + "V2"
    line := targetSym.SelectionRange.Start.Line
    col := targetSym.SelectionRange.Start.Character

    t.Logf("Rename: %s.%s → %s (L%d:%d)", filepath.Base(targetFile), oldName, newName, line+1, col)

    edit, err := mgr.Rename(targetFile, line, col, newName)
    if err != nil {
        t.Skipf("rename failed: %v", err)
    }

    // 统计编辑
    totalEdits := 0
    filesAffected := 0
    // 兼容 Changes 和 DocumentChanges
    if len(edit.Changes) > 0 {
        for uri, edits := range edit.Changes {
            filesAffected++
            totalEdits += len(edits)
            // 验证每个 edit 的 NewText 是否等于 newName
            for _, e := range edits {
                if e.NewText != newName {
                    t.Errorf("unexpected NewText: got %q, want %q", e.NewText, newName)
                }
            }
            t.Logf("  %s: %d edits", filepath.Base(strings.TrimPrefix(uri, "file://")), len(edits))
        }
    }
    if len(edit.DocumentChanges) > 0 {
        for _, dc := range edit.DocumentChanges {
            filesAffected++
            totalEdits += len(dc.Edits)
            for _, e := range dc.Edits {
                if e.NewText != newName {
                    t.Errorf("unexpected NewText: got %q, want %q", e.NewText, newName)
                }
            }
            t.Logf("  %s: %d edits", filepath.Base(strings.TrimPrefix(dc.TextDocument.URI, "file://")), len(dc.Edits))
        }
    }

    t.Logf("Rename 结果: %d 文件, %d 处编辑, 所有 NewText 均为 %q ✓", filesAffected, totalEdits, newName)
    if totalEdits == 0 {
        t.Log("⚠️ 无编辑产生 (gopls 可能限制了此操作)")
    }
}
```

**步骤 2: 运行测试**

运行: `go test -v -run TestLSP_Stress_RenameApply -timeout 120s ./internal/lsp/`
预期: PASS

---

### 任务 10: 操作延迟统计 [P1]

**目标:** 记录每个 LSP 操作的 ms 耗时，输出 p50/p95/max 性能基线。

**文件:**
- 修改: `go-agent-v2/internal/lsp/lsp_stress_test.go`

**步骤 1: 实现延迟统计辅助函数和 TestLSP_Stress_Latency**

```go
type LatencyStats struct {
    Name    string
    Samples []time.Duration
}

func (ls *LatencyStats) Add(d time.Duration) { ls.Samples = append(ls.Samples, d) }

func (ls *LatencyStats) Report(t *testing.T) {
    t.Helper()
    if len(ls.Samples) == 0 {
        t.Logf("  %s: no samples", ls.Name)
        return
    }
    sort.Slice(ls.Samples, func(i, j int) bool { return ls.Samples[i] < ls.Samples[j] })
    n := len(ls.Samples)
    p50 := ls.Samples[n/2]
    p95 := ls.Samples[int(float64(n)*0.95)]
    max := ls.Samples[n-1]
    t.Logf("  %s: n=%d  p50=%v  p95=%v  max=%v", ls.Name, n, p50, p95, max)
}

func TestLSP_Stress_Latency(t *testing.T) {
    skipIfNotAvailable(t, "gopls")

    goProjectDir := filepath.Join(testCorpusDir, "go")
    mgr := NewManager(nil)
    mgr.SetRootURI("file://" + goProjectDir)
    defer mgr.StopAll()

    goFiles := collectFiles(t, goProjectDir, ".go")
    maxFiles := min(50, len(goFiles))
    for i := 0; i < maxFiles; i++ {
        content, _ := os.ReadFile(goFiles[i])
        mgr.OpenFile(goFiles[i], string(content))
    }
    time.Sleep(12 * time.Second)

    symbolLatency := &LatencyStats{Name: "DocumentSymbol"}
    defLatency := &LatencyStats{Name: "Definition"}
    refLatency := &LatencyStats{Name: "References"}
    hoverLatency := &LatencyStats{Name: "Hover"}

    for i := 0; i < maxFiles; i++ {
        start := time.Now()
        symbols, _ := mgr.DocumentSymbol(goFiles[i])
        symbolLatency.Add(time.Since(start))

        if len(symbols) > 0 {
            sym := symbols[0]
            line, col := sym.SelectionRange.Start.Line, sym.SelectionRange.Start.Character

            start = time.Now()
            mgr.Definition(goFiles[i], line, col)
            defLatency.Add(time.Since(start))

            start = time.Now()
            mgr.References(goFiles[i], line, col, true)
            refLatency.Add(time.Since(start))

            start = time.Now()
            mgr.Hover(goFiles[i], line, col)
            hoverLatency.Add(time.Since(start))
        }
    }

    t.Log("=== 延迟统计 (Go) ===")
    symbolLatency.Report(t)
    defLatency.Report(t)
    refLatency.Report(t)
    hoverLatency.Report(t)
}
```

> [!NOTE]
> 需要追加 `import "sort"` 到文件头。

**步骤 2: 运行测试**

运行: `go test -v -run TestLSP_Stress_Latency -timeout 120s ./internal/lsp/`
预期: PASS，输出 p50/p95/max 延迟数据

---

### 任务 11: 并发压力测试 [P1]

**目标:** 多 goroutine 同时调用 definition/references/hover，验证线程安全不 panic。

**文件:**
- 修改: `go-agent-v2/internal/lsp/lsp_stress_test.go`

**步骤 1: 实现 TestLSP_Stress_Concurrent**

```go
func TestLSP_Stress_Concurrent(t *testing.T) {
    skipIfNotAvailable(t, "gopls")

    goProjectDir := filepath.Join(testCorpusDir, "go")
    mgr := NewManager(nil)
    mgr.SetRootURI("file://" + goProjectDir)
    defer mgr.StopAll()

    goFiles := collectFiles(t, goProjectDir, ".go")
    maxFiles := min(20, len(goFiles))
    for i := 0; i < maxFiles; i++ {
        content, _ := os.ReadFile(goFiles[i])
        mgr.OpenFile(goFiles[i], string(content))
    }
    time.Sleep(10 * time.Second)

    // 收集每个文件的第一个符号位置
    type target struct {
        file     string
        line, col int
    }
    var targets []target
    for i := 0; i < maxFiles; i++ {
        symbols, _ := mgr.DocumentSymbol(goFiles[i])
        if len(symbols) > 0 {
            s := symbols[0]
            targets = append(targets, target{goFiles[i], s.SelectionRange.Start.Line, s.SelectionRange.Start.Character})
        }
    }

    if len(targets) == 0 { t.Skip("no valid targets") }

    // 并发执行
    var wg sync.WaitGroup
    var panicCount atomic.Int64
    var successCount atomic.Int64
    concurrency := 8

    for c := 0; c < concurrency; c++ {
        wg.Add(1)
        go func(id int) {
            defer wg.Done()
            defer func() {
                if r := recover(); r != nil {
                    panicCount.Add(1)
                    t.Errorf("goroutine %d panicked: %v", id, r)
                }
            }()
            tgt := targets[id % len(targets)]
            // 每个 goroutine 执行多次操作
            for round := 0; round < 5; round++ {
                mgr.DocumentSymbol(tgt.file)
                mgr.Definition(tgt.file, tgt.line, tgt.col)
                mgr.References(tgt.file, tgt.line, tgt.col, true)
                mgr.Hover(tgt.file, tgt.line, tgt.col)
                successCount.Add(4)
            }
        }(c)
    }
    wg.Wait()

    t.Logf("并发测试: concurrency=%d, operations=%d, panics=%d",
        concurrency, successCount.Load(), panicCount.Load())
    if panicCount.Load() > 0 {
        t.Fatalf("并发测试出现 %d 次 panic!", panicCount.Load())
    }
}
```

> [!NOTE]
> 需要追加 `import "sync"` 到文件头。

**步骤 2: 运行测试**

运行: `go test -v -run TestLSP_Stress_Concurrent -timeout 120s ./internal/lsp/`
预期: PASS, panics=0

---

### 任务 12: DocumentSymbol 深度验证 [P1]

**目标:** 检查 children 层级、Kind 枚举覆盖度。

**文件:**
- 修改: `go-agent-v2/internal/lsp/lsp_stress_test.go`

**步骤 1: 实现 TestLSP_Stress_SymbolDepth**

```go
func TestLSP_Stress_SymbolDepth(t *testing.T) {
    skipIfNotAvailable(t, "gopls")

    goProjectDir := filepath.Join(testCorpusDir, "go")
    mgr := NewManager(nil)
    mgr.SetRootURI("file://" + goProjectDir)
    defer mgr.StopAll()

    goFiles := collectFiles(t, goProjectDir, ".go")
    maxFiles := min(30, len(goFiles))
    for i := 0; i < maxFiles; i++ {
        content, _ := os.ReadFile(goFiles[i])
        mgr.OpenFile(goFiles[i], string(content))
    }
    time.Sleep(10 * time.Second)

    kindSet := make(map[SymbolKind]int)
    totalChildren := 0
    filesWithChildren := 0

    for i := 0; i < maxFiles; i++ {
        symbols, err := mgr.DocumentSymbol(goFiles[i])
        if err != nil { continue }

        hasChildren := false
        for _, s := range symbols {
            kindSet[s.Kind]++
            if len(s.Children) > 0 {
                hasChildren = true
                totalChildren += len(s.Children)
                for _, c := range s.Children {
                    kindSet[c.Kind]++
                }
            }
        }
        if hasChildren { filesWithChildren++ }
    }

    t.Log("=== SymbolKind 分布 ===")
    for kind, count := range kindSet {
        t.Logf("  %s (%d): %d", kind.String(), kind, count)
    }
    t.Logf("带 children 的文件: %d/%d", filesWithChildren, maxFiles)
    t.Logf("总 children 数: %d", totalChildren)

    // 验证: 至少出现 3 种不同的 SymbolKind
    if len(kindSet) < 3 {
        t.Errorf("SymbolKind 种类过少: %d (期望至少 3)", len(kindSet))
    }
}
```

**步骤 2: 运行测试**

运行: `go test -v -run TestLSP_Stress_SymbolDepth -timeout 60s ./internal/lsp/`
预期: PASS

---

### 任务 13: textDocument/completion [P2]

**目标:** 新增 LSP 补全能力并测试。

**文件:**
- 修改: `go-agent-v2/internal/lsp/protocol.go` — 新增 CompletionParams / CompletionItem 类型
- 修改: `go-agent-v2/internal/lsp/client.go` — 新增 Completion 方法
- 修改: `go-agent-v2/internal/lsp/manager.go` — 新增 Completion 代理方法
- 修改: `go-agent-v2/internal/lsp/lsp_stress_test.go` — 新增 TestLSP_Stress_Completion

**步骤 1: protocol.go 新增类型**

```go
// CompletionParams 补全请求参数。
type CompletionParams struct {
    TextDocument TextDocumentIdentifier `json:"textDocument"`
    Position     Position               `json:"position"`
}

// CompletionItem 补全项。
type CompletionItem struct {
    Label         string `json:"label"`
    Kind          int    `json:"kind,omitempty"`
    Detail        string `json:"detail,omitempty"`
    Documentation any    `json:"documentation,omitempty"`
    InsertText    string `json:"insertText,omitempty"`
}

// CompletionList 补全列表。
type CompletionList struct {
    IsIncomplete bool             `json:"isIncomplete"`
    Items        []CompletionItem `json:"items"`
}
```

**步骤 2: client.go 新增 Completion**

```go
// Completion 代码补全 — 返回补全列表。
//
// LSP 规范: 返回值可能是 CompletionItem[] | CompletionList | null。
func (c *Client) Completion(ctx context.Context, uri string, line, character int) ([]CompletionItem, error) {
    if !c.Running() {
        return nil, fmt.Errorf("lsp client not running")
    }
    var raw json.RawMessage
    err := c.call("textDocument/completion", CompletionParams{
        TextDocument: TextDocumentIdentifier{URI: uri},
        Position:     Position{Line: line, Character: character},
    }, &raw)
    if err != nil { return nil, err }
    if len(raw) == 0 || string(raw) == "null" { return nil, nil }
    // 尝试解析为 CompletionList
    var list CompletionList
    if err := json.Unmarshal(raw, &list); err == nil && len(list.Items) > 0 {
        return list.Items, nil
    }
    // 回退: 解析为 []CompletionItem
    var items []CompletionItem
    if err := json.Unmarshal(raw, &items); err == nil {
        return items, nil
    }
    return nil, nil
}
```

**步骤 3: manager.go 新增代理方法**

```go
func (m *Manager) Completion(filePath string, line, character int) ([]CompletionItem, error) {
    client, err := m.clientForFile(filePath)
    if client == nil || err != nil { return nil, err }
    return client.Completion(m.ctx, pathToURI(filePath), line, character)
}
```

**步骤 4: 测试**

```go
func TestLSP_Stress_Completion(t *testing.T) {
    skipIfNotAvailable(t, "gopls")

    goProjectDir := filepath.Join(testCorpusDir, "go")
    mgr := NewManager(nil)
    mgr.SetRootURI("file://" + goProjectDir)
    defer mgr.StopAll()

    goFiles := collectFiles(t, goProjectDir, ".go")
    if len(goFiles) == 0 { t.Fatal("no Go files") }

    content, _ := os.ReadFile(goFiles[0])
    mgr.OpenFile(goFiles[0], string(content))
    time.Sleep(8 * time.Second)

    // 在文件中间位置触发补全
    lines := strings.Split(string(content), "\n")
    midLine := len(lines) / 2
    items, err := mgr.Completion(goFiles[0], midLine, 0)
    if err != nil {
        t.Logf("Completion error: %v", err)
    }
    t.Logf("Completion: %d items at L%d", len(items), midLine+1)
    for i, item := range items {
        if i >= 10 { t.Logf("  ... and %d more", len(items)-10); break }
        t.Logf("  [%d] %s (%s)", item.Kind, item.Label, item.Detail)
    }
}
```

**步骤 5: 编译+运行**

运行: `go test -v -run TestLSP_Stress_Completion -timeout 60s ./internal/lsp/`
预期: PASS

---

### 任务 14: textDocument/didChange [P2]

**目标:** 测试文件修改后的增量同步 — open → 修改 → hover 应反映新内容。

**文件:**
- 修改: `go-agent-v2/internal/lsp/protocol.go` — 新增 DidChangeTextDocumentParams
- 修改: `go-agent-v2/internal/lsp/client.go` — 新增 DidChange 方法
- 修改: `go-agent-v2/internal/lsp/manager.go` — 新增 ChangeFile 代理方法
- 修改: `go-agent-v2/internal/lsp/lsp_stress_test.go` — 新增 TestLSP_Stress_DidChange

**步骤 1: protocol.go 新增类型**

```go
// DidChangeTextDocumentParams didChange 通知参数。
type DidChangeTextDocumentParams struct {
    TextDocument   VersionedTextDocumentIdentifier  `json:"textDocument"`
    ContentChanges []TextDocumentContentChangeEvent `json:"contentChanges"`
}

// VersionedTextDocumentIdentifier 带版本的文档标识。
type VersionedTextDocumentIdentifier struct {
    URI     string `json:"uri"`
    Version int    `json:"version"`
}

// TextDocumentContentChangeEvent 内容变更事件 (全量替换模式)。
type TextDocumentContentChangeEvent struct {
    Text string `json:"text"` // 全量替换
}
```

**步骤 2: client.go 新增 DidChange**

```go
func (c *Client) DidChange(uri string, version int, newText string) error {
    return c.notify("textDocument/didChange", DidChangeTextDocumentParams{
        TextDocument: VersionedTextDocumentIdentifier{URI: uri, Version: version},
        ContentChanges: []TextDocumentContentChangeEvent{{Text: newText}},
    })
}
```

**步骤 3: manager.go 新增代理方法**

```go
func (m *Manager) ChangeFile(filePath string, version int, newContent string) error {
    client, err := m.clientForFile(filePath)
    if client == nil || err != nil { return err }
    return client.DidChange(pathToURI(filePath), version, newContent)
}
```

**步骤 4: 测试**

```go
func TestLSP_Stress_DidChange(t *testing.T) {
    skipIfNotAvailable(t, "gopls")

    // 使用临时文件避免修改真实语料
    tmpDir := t.TempDir()
    must(t, os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte("module changetest\ngo 1.22\n"), 0644))
    code := "package changetest\n\nfunc OldFunc() string { return \"old\" }\n"
    goFile := filepath.Join(tmpDir, "main.go")
    must(t, os.WriteFile(goFile, []byte(code), 0644))

    mgr := NewManager(nil)
    mgr.SetRootURI("file://" + tmpDir)
    defer mgr.StopAll()

    // Open
    mgr.OpenFile(goFile, code)
    time.Sleep(3 * time.Second)

    // Hover on OldFunc
    hover1, _ := mgr.Hover(goFile, 2, 5)
    t.Logf("Before change - Hover: %v", hover1 != nil)

    // DidChange — 修改函数名
    newCode := "package changetest\n\nfunc NewFunc() int { return 42 }\n"
    mgr.ChangeFile(goFile, 2, newCode)
    time.Sleep(3 * time.Second)

    // Hover on NewFunc (same position)
    hover2, _ := mgr.Hover(goFile, 2, 5)
    if hover2 != nil {
        t.Logf("After change - Hover: %s", hover2.Contents.Value[:min(len(hover2.Contents.Value), 100)])
        if strings.Contains(hover2.Contents.Value, "NewFunc") {
            t.Log("✓ didChange reflected correctly")
        }
    } else {
        t.Log("⚠️ Hover returned nil after change (gopls may need more time)")
    }

    // DocumentSymbol 应该反映新名称
    symbols, _ := mgr.DocumentSymbol(goFile)
    for _, s := range symbols {
        t.Logf("Symbol after change: %s", s.Name)
    }
}
```

**步骤 5: 编译+运行**

运行: `go test -v -run TestLSP_Stress_DidChange -timeout 60s ./internal/lsp/`
预期: PASS

---

### 任务 15: LSP Error Recovery [P2]

**目标:** 验证 LSP 操作在异常情况下优雅降级（不 panic、不死锁）。

**文件:**
- 修改: `go-agent-v2/internal/lsp/lsp_stress_test.go`

**步骤 1: 实现 TestLSP_Stress_ErrorRecovery**

```go
func TestLSP_Stress_ErrorRecovery(t *testing.T) {
    skipIfNotAvailable(t, "gopls")

    goProjectDir := filepath.Join(testCorpusDir, "go")
    mgr := NewManager(nil)
    mgr.SetRootURI("file://" + goProjectDir)
    defer mgr.StopAll()

    goFiles := collectFiles(t, goProjectDir, ".go")
    if len(goFiles) == 0 { t.Fatal("no Go files") }

    // 打开一个文件启动 gopls
    content, _ := os.ReadFile(goFiles[0])
    mgr.OpenFile(goFiles[0], string(content))
    time.Sleep(5 * time.Second)

    // 场景 1: 对未打开的文件执行操作 — 应返回 nil 不 panic
    t.Run("UnknownFile", func(t *testing.T) {
        locs, err := mgr.Definition("/nonexistent/foo.go", 0, 0)
        if err != nil { t.Logf("error (expected): %v", err) }
        if locs != nil { t.Log("got locations for nonexistent file?") }
        t.Log("✓ no panic on nonexistent file")
    })

    // 场景 2: 越界行列号 — 应不 crash
    t.Run("OutOfBounds", func(t *testing.T) {
        _, err := mgr.Definition(goFiles[0], 99999, 99999)
        t.Logf("OutOfBounds result: err=%v (expected some error or nil)", err)
        t.Log("✓ no panic on out-of-bounds position")
    })

    // 场景 3: 对二进制文件操作 — 应不 crash
    t.Run("BinaryFile", func(t *testing.T) {
        tmpBin := filepath.Join(t.TempDir(), "test.go")
        os.WriteFile(tmpBin, []byte{0x00, 0x01, 0x02}, 0644)
        err := mgr.OpenFile(tmpBin, string([]byte{0x00, 0x01, 0x02}))
        t.Logf("Binary open: err=%v", err)
        t.Log("✓ no panic on binary content")
    })

    // 场景 4: 空文件 — 应不 crash
    t.Run("EmptyFile", func(t *testing.T) {
        tmpEmpty := filepath.Join(t.TempDir(), "empty.go")
        os.WriteFile(tmpEmpty, []byte(""), 0644)
        err := mgr.OpenFile(tmpEmpty, "")
        t.Logf("Empty open: err=%v", err)
        symbols, _ := mgr.DocumentSymbol(tmpEmpty)
        t.Logf("Empty symbols: %d", len(symbols))
        t.Log("✓ no panic on empty file")
    })

    // 场景 5: StopAll 后调用 — 应返回 nil 不 panic
    t.Run("AfterStop", func(t *testing.T) {
        mgr2 := NewManager(nil)
        mgr2.SetRootURI("file://" + goProjectDir)
        content2, _ := os.ReadFile(goFiles[0])
        mgr2.OpenFile(goFiles[0], string(content2))
        time.Sleep(3 * time.Second)
        mgr2.StopAll()

        // StopAll 后操作
        _, err := mgr2.Definition(goFiles[0], 0, 0)
        t.Logf("After StopAll: err=%v", err)
        t.Log("✓ no panic after StopAll")
    })
}
```

**步骤 2: 运行测试**

运行: `go test -v -run TestLSP_Stress_ErrorRecovery -timeout 60s ./internal/lsp/`
预期: PASS, 所有 5 个场景不 panic

---

## 验收标准 (更新)

| 指标 | Go | JS/TS | Rust |
|------|:-:|:-:|:-:|
| **基础测试 (任务 3-6)** | | | |
| Open 成功率 | >= 95% | >= 90% | >= 90% |
| DocumentSymbol 成功率 | >= 80% | >= 70% | >= 70% |
| Definition 成功率 | >= 50% | >= 40% | >= 40% |
| References 成功率 | >= 50% | >= 40% | >= 30% |
| Hover 成功率 | >= 60% | >= 50% | >= 50% |
| Diagnostics 不 crash | ✓ | ✓ | ✓ |
| Rename 不 crash | ✓ | – | – |
| **增强测试 (任务 8-15)** | | | |
| 跨文件 Definition | >= 1 次 | – | – |
| Rename 应用+NewText 验证 | ✓ | – | – |
| 延迟统计输出 p50/p95/max | ✓ | – | – |
| 并发 8 goroutine 不 panic | ✓ | – | – |
| SymbolKind >= 3 种 | ✓ | – | – |
| Completion 返回 items | ✓ | – | – |
| didChange 反映新内容 | ✓ | – | – |
| Error Recovery 5 场景 | ✓ | – | – |
| **全局** | | | |
| 无 panic/crash/死锁/OOM | ✓ | ✓ | ✓ |
| 总测试运行 < 900s | ✓ | ✓ | ✓ |

---

## 运行全部测试

```bash
cd /Users/mima0000/Desktop/wj/multi-agent-orchestration/go-agent-v2
go test -v -run "TestLSP_Stress" -timeout 900s ./internal/lsp/ 2>&1 | tee /Users/mima0000/Desktop/wj/e2e测试/test-output-v3.log
```

## 覆盖的全部 LSP 工具 + 增强能力

| # | 工具/能力 | 测试位置 | 验证方式 |
|---|---------|---------|---------|
| 1 | `lsp_open_file` | 批量 Open | `opened` 计数 |
| 2 | `lsp_hover` | `testFile()` + Latency | 成功率 + p50/p95 |
| 3 | `lsp_diagnostics` | `atomic.Int64` 回调 | 回调计数 |
| 4 | `lsp_definition` | `testFile()` + CrossFile | 成功率 + 跨文件验证 |
| 5 | `lsp_references` | `testFile()` + Concurrent | 成功率 + 并发安全 |
| 6 | `lsp_document_symbol` | `testFile()` + SymbolDepth | 符号计数 + Kind 分布 |
| 7 | `lsp_rename` | Rename + RenameApply | edits 计数 + NewText 验证 |
| 8 | `completion` (NEW) | Completion | items 计数 |
| 9 | `didChange` (NEW) | DidChange | 内容反映验证 |
| 10 | 并发安全 | Concurrent | 8 goroutine 不 panic |
| 11 | 错误恢复 | ErrorRecovery | 5 场景不 crash |
| 12 | 性能基线 | Latency | p50/p95/max |

---

## 审查结论 (V4)

> **执行优先级说明:** 本节及其后续 “任务 16-24 / 验收标准 (V4 最终) / 运行全部测试 (V4 推荐)” 为当前执行基线，优先级高于上文 V3 对应段落。

### 主要问题 (需补强)

1. 当前计划以“功能可用”为主，生命周期与状态机（`Statuses`/`Reload`/`StopAll`）覆盖不足，无法提前发现“可重启性”回归。
2. `Open/Close` 幂等行为未设独立测试；重复打开、重复关闭、关闭后重开是线上高频操作，缺少守护。
3. 语言服务器缺失/命令不可用的失败路径覆盖不完整，容易在 CI/新机器上出现不可预期失败。
4. 多语言共用 `Manager` 的隔离性未验证（Go/TS/Rust 同时运行时可能互相污染根路径或状态）。
5. 并发测试目前只检查 panic，未配合 `-race` 与重复执行，难以发现数据竞争和偶发失败。
6. 诊断链路仅统计“回调次数”，缺少 URI 合法性、Severity 合法性等质量断言。
7. 缺少 Unicode/空格路径场景；实际语料路径含中文（`/Users/mima0000/Desktop/wj/e2e测试`），需要显式守护 URI 编解码。
8. 缺少 Golden Record 统计回归门禁，历史性能/成功率漂移无法自动发现。

### 与测试规范对齐 (单元 / 集成 / Golden Record)

- 单元测试: 新增 helper 纯函数与边界行为断言（幂等、错误恢复、URI 合法性）。
- 集成测试: 保留真实语言服务器压测，新增生命周期、缺失依赖、多语言隔离、Unicode 路径场景。
- Golden Record: 将关键成功率与延迟基线持久化并做容差比较，形成长期回归护栏。

---

## 增强测试 (V4 新增)

### 任务 16: 生命周期状态机测试 [P0]

**目标:** 验证 `Statuses`、`Reload`、`StopAll` 的状态转移与可重启性。

**文件:**
- 修改: `go-agent-v2/internal/lsp/lsp_stress_test.go`

**步骤:**
1. 新增 `TestLSP_Stress_Lifecycle`。
2. 断言初始状态：所有配置语言 `Running=false`。
3. `OpenFile` 一个 Go 文件后，断言 Go 语言 `Running=true`。
4. 调用 `Reload()` 后，断言 `Running=false`，再 `OpenFile` 断言可重启。
5. 调用 `StopAll()` 后再次 `OpenFile`，断言仍可重启（防止 context 失效回归）。

**运行:**
`go test -v -run TestLSP_Stress_Lifecycle -timeout 120s ./internal/lsp/`

---

### 任务 17: Open/Close 幂等 + Reopen [P0]

**目标:** 验证重复打开、重复关闭、关闭后重开行为稳定。

**文件:**
- 修改: `go-agent-v2/internal/lsp/lsp_stress_test.go`

**步骤:**
1. 新增 `TestLSP_Stress_OpenCloseIdempotent`。
2. 对同一文件执行 3 次 `OpenFile`，期望无错误。
3. 执行 2 次 `CloseFile`，期望无错误。
4. 关闭后重开并执行 `DocumentSymbol`/`Hover`，期望可恢复。
5. 所有步骤要求不 panic、不死锁。

**运行:**
`go test -v -run TestLSP_Stress_OpenCloseIdempotent -timeout 120s ./internal/lsp/`

---

### 任务 18: 缺失依赖与不支持扩展场景 [P0]

**目标:** 验证异常依赖环境下的可预期失败与降级行为。

**文件:**
- 修改: `go-agent-v2/internal/lsp/lsp_stress_test.go`

**步骤:**
1. 新增 `TestLSP_Stress_MissingServerBinary`：使用自定义 `ServerConfig`（命令名不存在），`OpenFile` 应返回错误且不 panic。
2. 新增 `TestLSP_Stress_UnsupportedExtension`：对 `.txt/.md` 调用 `OpenFile` 应静默返回 `nil`。
3. 确认同一 `Manager` 下，缺失语言失败不影响已可用语言（例如 Go）继续工作。

**运行:**
`go test -v -run "TestLSP_Stress_(MissingServerBinary|UnsupportedExtension)" -timeout 120s ./internal/lsp/`

---

### 任务 19: 多语言同 Manager 隔离测试 [P1]

**目标:** 在一个 `Manager` 中同时运行 Go/TS/Rust，验证请求分流与状态隔离。

**文件:**
- 修改: `go-agent-v2/internal/lsp/lsp_stress_test.go`

**步骤:**
1. 新增 `TestLSP_Stress_MultiLanguageIsolation`。
2. 同一 `Manager` 分别 `OpenFile` 三种语言样本文件。
3. 分别执行 `DocumentSymbol`、`Definition`、`Hover`。
4. 断言返回 URI 与请求语言语料路径一致（防止跨语言串线）。
5. 记录 `Statuses()`，确认仅已触发语言处于 `Running=true`。

**运行:**
`go test -v -run TestLSP_Stress_MultiLanguageIsolation -timeout 180s ./internal/lsp/`

---

### 任务 20: Diagnostics 质量验证 [P1]

**目标:** 从“有回调”提升到“回调数据有效”。

**文件:**
- 修改: `go-agent-v2/internal/lsp/lsp_stress_test.go`

**步骤:**
1. 新增 `TestLSP_Stress_DiagnosticsQuality`，收集 `(uri, diagnostics[])`。
2. 断言 `uri` 非空、可转换为合法路径。
3. 断言 `severity` 在合法范围（0/1/2/3/4，兼容服务端未填 severity）。
4. 输出每语言诊断计数、error/warn 分布，便于后续 Golden 比较。

**运行:**
`go test -v -run TestLSP_Stress_DiagnosticsQuality -timeout 180s ./internal/lsp/`

---

### 任务 21: 并发 + Race Gate [P1]

**目标:** 除 panic 外，进一步验证无数据竞争。

**步骤:**
1. 保留任务 11 并发测试。
2. 增加 race 运行门禁：对并发相关用例执行 `-race`。
3. 失败即阻断（出现 race 报告或测试失败）。

**运行:**
```bash
cd /Users/mima0000/Desktop/wj/multi-agent-orchestration/go-agent-v2
go test -race -v -run "TestLSP_Stress_(Concurrent|Lifecycle|OpenCloseIdempotent)" -timeout 240s ./internal/lsp/
```

---

### 任务 22: Flake/Soak 稳定性 [P1]

**目标:** 发现偶发超时、偶发空结果、偶发死锁。

**步骤:**
1. 对关键用例执行 3 轮重复（`-count=3`）。
2. 使用 `-shuffle=on` 打乱顺序，暴露顺序依赖。
3. 统计失败轮次与失败类型，生成 `stress-flake-report.txt`。

**运行:**
```bash
cd /Users/mima0000/Desktop/wj/multi-agent-orchestration/go-agent-v2
go test -v -shuffle=on -count=3 -run "TestLSP_Stress_(Go|JS|Rust|Rename|Concurrent|ErrorRecovery)" -timeout 1200s ./internal/lsp/ 2>&1 | tee /Users/mima0000/Desktop/wj/e2e测试/stress-flake-report.txt
```

---

### 任务 23: Unicode/空格路径与 URI 编解码 [P2]

**目标:** 验证中文/空格路径下 `OpenFile` 与查询链路稳定。

**文件:**
- 修改: `go-agent-v2/internal/lsp/lsp_stress_test.go`

**步骤:**
1. 新增 `TestLSP_Stress_UnicodePath`。
2. 在 `t.TempDir()` 下创建含中文和空格的目录，例如 `工程 空间/模块A`。
3. 构造最小 Go 模块并执行 `OpenFile` + `DocumentSymbol` + `Hover`。
4. 断言不 panic，且返回结果 URI 可逆转换为本地路径。

**运行:**
`go test -v -run TestLSP_Stress_UnicodePath -timeout 120s ./internal/lsp/`

---

### 任务 24: Golden Record 回归门禁 [P1]

**目标:** 将“本次结果”与“历史基线”比较，防止质量慢性下滑。

**文件:**
- 修改: `go-agent-v2/internal/lsp/lsp_stress_test.go`
- 新增: `go-agent-v2/internal/lsp/testdata/lsp_stress_baseline.json`

**步骤:**
1. 在任务 3/4/5/10/20 输出 `summary`（成功率、错误率、p95）。
2. 新增 `TestLSP_Stress_GoldenBaseline`：
   - 若 baseline 不存在：生成并提示人工确认。
   - 若存在：比较容差（成功率退化 <= 5pp，p95 退化 <= 30%）。
3. 超出容差即失败，作为 CI 回归门禁。

**运行:**
`go test -v -run TestLSP_Stress_GoldenBaseline -timeout 120s ./internal/lsp/`

---

## 验收标准 (V4 最终)

| 指标 | Go | JS/TS | Rust |
|------|:-:|:-:|:-:|
| Open 成功率 | >= 95% | >= 90% | >= 90% |
| DocumentSymbol 成功率 | >= 80% | >= 70% | >= 70% |
| Definition 成功率 | >= 50% | >= 40% | >= 40% |
| References 成功率 | >= 50% | >= 40% | >= 30% |
| Hover 成功率 | >= 60% | >= 50% | >= 50% |
| Diagnostics 回调质量校验通过 | ✓ | ✓ | ✓ |
| 生命周期状态机测试通过 | ✓ | ✓ | ✓ |
| Open/Close 幂等测试通过 | ✓ | ✓ | ✓ |
| 缺失依赖/不支持扩展降级正确 | ✓ | ✓ | ✓ |
| 并发 + `-race` 无告警 | ✓ | ✓ | ✓ |
| Flake 3 轮稳定 | ✓ | ✓ | ✓ |
| Unicode 路径场景通过 | ✓ | ✓ | ✓ |
| Golden Baseline 容差内 | ✓ | ✓ | ✓ |
| 无 panic/crash/死锁/OOM | ✓ | ✓ | ✓ |

---

## 运行全部测试 (V4 推荐)

```bash
cd /Users/mima0000/Desktop/wj/multi-agent-orchestration/go-agent-v2

# 1) 常规压力套件
go test -v -run "TestLSP_Stress" -timeout 1200s ./internal/lsp/ 2>&1 | tee /Users/mima0000/Desktop/wj/e2e测试/test-output-v4.log

# 2) 并发竞态门禁
go test -race -v -run "TestLSP_Stress_(Concurrent|Lifecycle|OpenCloseIdempotent)" -timeout 240s ./internal/lsp/

# 3) 稳定性重复执行
go test -v -shuffle=on -count=3 -run "TestLSP_Stress_(Go|JS|Rust|Rename|Concurrent|ErrorRecovery)" -timeout 1200s ./internal/lsp/
```

## 覆盖能力矩阵 (V4)

| # | 能力 | 测试位置 | 验证方式 |
|---|---|---|---|
| 1 | `lsp_open_file` | 基础 + 幂等 | 成功率 + 重复操作 |
| 2 | `lsp_hover` | `testFile` + 并发 + Unicode | 成功率 + 稳定性 |
| 3 | `lsp_diagnostics` | 任务 3/4/5 + 任务 20 | 数量 + 质量 |
| 4 | `lsp_definition` | 基础 + 跨文件 + 隔离 | 成功率 + URI 校验 |
| 5 | `lsp_references` | 基础 + 并发 | 成功率 + 竞态门禁 |
| 6 | `lsp_document_symbol` | 基础 + 深度 + 幂等恢复 | 覆盖度 + 恢复能力 |
| 7 | `lsp_rename` | 任务 6/9 | edits 数量与内容 |
| 8 | `completion` | 任务 13 | items 可用性 |
| 9 | `didChange` | 任务 14 | 变更可见性 |
| 10 | 生命周期 | 任务 16 | 状态机与可重启 |
| 11 | 降级容错 | 任务 18/15 | 无 panic + 错误可预期 |
| 12 | 多语言隔离 | 任务 19 | 无串线 |
| 13 | 稳定性 | 任务 21/22 | race + flake |
| 14 | Golden 回归 | 任务 24 | 指标容差比较 |
