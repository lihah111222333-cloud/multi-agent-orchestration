---
description: 增强 LSP 工具栈 — P0 双基线守卫 + P1 增能 + DRY 优化 + P2 合并（21→7，生产就绪，无向下兼容）
---

# 增强 LSP 工具栈 — P0 双基线守卫 + P1 增能 + P2 合并（生产就绪，无向下兼容）

> 目标：基于现有 19 个 LSP 工具，先执行 P0 基线检测与防回归守卫，再补齐 2 个搜索能力（19→21），最后按意图合并到 7 个工具（21→7），并在 P2 收口时删除老接口实现与老工具入口，不做向下兼容。

---

## 0. P0：双基线检测与防回归（必做，阻断后续阶段）

### 0.1 P0 目标

1. 改造前冻结当前行为基线（P0-pre）。
2. P2 完成后切换到新行为基线（P0-post）。
3. 通过切换门槛确保回归守卫从旧语义平滑迁移到新语义。
4. 任一阶段守卫失败都阻断后续流程。

### 0.2 P0 模式定义

1. `P0-pre`：改造前基线（19+ext，旧工具名语义）。
2. `P0-post`：改造后基线（7 工具，新 action 路由语义）。
3. 同一时间仅允许一个模式作为 CI 阻断门禁。

### 0.3 基线范围

1. 工具 schema 基线。
2. LSP 可用性 gating 基线。
3. 关键运行时语义基线。
4. 旧工具名残留基线（prompts/templates/hints）。
5. 发布期指标基线（错误率、延迟、超时、unknown-tool）。

### 0.4 P0 产物（强制）

1. `internal/tools/testdata/tool_schemas.golden.json`（按模式维护）。
2. `internal/tools/schema_compat_test.go`（失败信息可读可定位）。
3. 守卫测试文件：
   1. `internal/tooladapter/p0_pre_lsp_schema_guard_test.go`
   2. `internal/apiserver/p0_pre_dynamic_tool_runtime_guard_test.go`
   3. `internal/apiserver/p0_pre_prompt_toolname_guard_test.go`
   4. `internal/tooladapter/p0_post_lsp_schema_guard_test.go`
   5. `internal/apiserver/p0_post_dynamic_tool_runtime_guard_test.go`
   6. `internal/apiserver/p0_post_prompt_toolname_guard_test.go`
4. 基线报告：
   1. `.agent/workflows/baselines/lsp-p0-pre-baseline.md`
   2. `.agent/workflows/baselines/lsp-p0-post-baseline.md`

### 0.5 P0-pre 守卫断言（必须覆盖）

1. `hasAvailableServer=true` 时暴露当前 19 + ext。
2. `hasAvailableServer=false` 时行为与改造前一致。
3. `lsp_did_change` + `persist_to_disk=true` 触发 diff；否则不触发。
4. `tool_handlers_hints` 旧工具名提示文案快照一致。
5. `.agent/`、`internal/apiserver/commonadapter/skills.go`、`internal/store/prompt_template.go` 的旧工具名可扫描。

### 0.6 P0-post 守卫断言（必须覆盖）

1. 工具集合收敛为 7 个：`lsp_file`、`lsp_inspect`、`lsp_xref`、`lsp_grep`、`lsp_structure`、`lsp_edit`、`lsp_completion`。
2. `hasAvailableServer=false` 时 schema 仍暴露 `lsp_grep`。
3. `lsp_file action=change` 接管原 `lsp_did_change` diff 语义。
4. `lsp_grep` 不依赖 manager precheck，失败时不返回 `lsp manager unavailable`。
5. prompts/templates/hints 旧工具名残留为零。
6. 旧工具名调用返回 `UNKNOWN_TOOL`（不做 alias/shim）。
7. 发布期指标满足 SLO（见第 14 节）。

### 0.7 P2 切换门槛（Cutover Gate，强制）

1. P2 代码与测试已合并，P2 验收项全绿。
2. `lsp_did_change` 旧链路迁移到 `lsp_file action=change`。
3. 旧工具名扫描结果为零残留。
4. `P0-post` 守卫与 post baseline/golden 全绿。
5. CI 阻断套件切换为 `P0-post`，`P0-pre` 降级为历史回放（非阻断）。

### 0.8 P0 失败策略

1. 任一 P0 守卫失败即停止，不进入下一阶段。
2. `P0-pre` 失败：禁止进入 P1。
3. `P0-post` 失败：禁止宣告 P2 完成与发布。

---

## 1. 执行前结论

1. `lsp_grep` 独立降级闭环到 schema 暴露层。
2. `lsp_file` 条件 required 保留空 `file_path` 诊断能力。
3. P2 改名影响面覆盖 `tooladapter` / `apiserver` / tests / prompts / templates。
4. DRY-1 的 `hasFilePath` 可落地。
5. `registry` 改造路径唯一。
6. `rg/sg` 缺失行为唯一化：工具可见，调用返回固定依赖错误。
7. P2 收口时删除老接口实现和老工具入口，不做向下兼容。

---

## 2. 现状基线（19 个工具）

### 2.1 Base 9（`internal/tools/lsp_tools.go`）

1. `lsp_hover`
2. `lsp_open_file`
3. `lsp_diagnostics`
4. `lsp_definition`
5. `lsp_references`
6. `lsp_document_symbol`
7. `lsp_rename`
8. `lsp_completion`
9. `lsp_did_change`

### 2.2 Ext 10（4 个 ext 文件）

1. `lsp_code_action`
2. `lsp_signature_help`
3. `lsp_format`
4. `lsp_call_hierarchy`
5. `lsp_type_hierarchy`
6. `lsp_semantic_tokens`
7. `lsp_folding_range`
8. `lsp_workspace_symbol`
9. `lsp_implementation`
10. `lsp_type_definition`

---

## 3. P1：补齐能力（19→21）

### 3.1 新增工具

1. `lsp_text_search`（ripgrep）
2. `lsp_ast_search`（ast-grep）

### 3.2 接口变更

```go
type LSPHandlerProvider interface {
    // ... existing 19 methods ...
    TextSearch(json.RawMessage) string
    AstSearch(json.RawMessage) string
}
```

### 3.3 P1 文件变更（强制）

1. 新增 `internal/tools/lsp_ext_search.go`
2. 修改 `internal/tools/lsp_tools.go`（接口 + `LSPExtTools()` 追加搜索 ext）
3. 新增 `internal/lsp/tool_handlers_search.go`
4. 新增 `internal/lsp/tool_handlers_search_test.go`
5. 修改 `internal/apiserver/p2_lsp_migration_test.go`
6. 修改 `internal/tooladapter/tooladapter_test.go`（`fakeLSPProvider` 补齐 `TextSearch`/`AstSearch`）

### 3.4 P1 降级策略（唯一口径）

1. `rg` 不存在：`lsp_text_search` 仍注册，返回 `error: rg not found in PATH`。
2. `sg` 不存在：`lsp_ast_search` 仍注册，返回 `error: sg not found in PATH`。
3. 当前阶段仍沿用现有 LSP 总体 gating。

### 3.5 P1 安全约束

1. 路径限制：`Abs` + `EvalSymlinks` + `Rel` 保证不越根。
2. 超时：15s。
3. 单次结果硬上限：50。
4. 单条内容截断：500 字符。
5. 总 payload 上限：16KB。
6. 默认排除：`.git/`、`node_modules/`、`vendor/`、`dist/`。

---

## 4. P1 附加：DRY 重构（零行为变更）

### 4.1 DRY-1：precheck/decode/validate 收敛

```go
type hasFilePath interface { GetFilePath() string }
```

```go
func precheckAndDecode[T hasFilePath](h *ToolHandlers, toolName string, args json.RawMessage) (
    T, string, *lspToolCallLogger, error,
)
```

### 4.2 DRY-2/3/4/5

1. `Implementation` / `TypeDefinition` 收敛为 `locationLookup`。
2. `CallHierarchy` / `TypeHierarchy` 收敛为 hierarchy helper。
3. `SemanticTokens` / `FoldingRange` 收敛为 file-path-only helper。
4. `SignatureHelp` 复用统一参数/校验流程。

---

## 5. P2：工具合并（21→7）

### 5.1 目标工具

1. `lsp_file`
2. `lsp_inspect`
3. `lsp_xref`
4. `lsp_grep`
5. `lsp_structure`
6. `lsp_edit`
7. `lsp_completion`

### 5.2 关键设计

1. `lsp_xref` 受 LSP 可用性 gating。
2. `lsp_grep` 不依赖 LSP server，独立暴露。
3. `lsp_completion` 保持独立。
4. 不做向下兼容，旧工具名直接失效。

### 5.3 接口收口（删除老接口实现，强制）

P2 完成时，`LSPHandlerProvider` 仅保留合并后 7 个入口，删除旧 21 工具方法签名与实现。

建议收口后接口：

```go
type LSPHandlerProvider interface {
    AvailabilitySummary() map[string]any
    DiagnosticsQuery(filePath string) map[string]any

    LSPFile(json.RawMessage) string
    LSPInspect(json.RawMessage) string
    LSPXRef(json.RawMessage) string
    LSPGrep(json.RawMessage) string
    LSPStructure(json.RawMessage) string
    LSPEdit(json.RawMessage) string
    Completion(json.RawMessage) string
}
```

---

## 6. `lsp_grep` 独立降级（registry 唯一路径）

### 6.1 唯一实施方案

1. 构建完整 LSP 工具集合 `all := buildLSPTools(provider)`。
2. `hasAvailableServer=true`：暴露全部。
3. `hasAvailableServer=false`：仅暴露 `lsp_grep`。
4. runtime handler 注册保持原行为，schema 层分流。

### 6.2 硬约束

1. `lsp_grep` handler 禁止 `managerUnavailable` precheck。
2. 失败时不得返回 `lsp manager unavailable`。
3. 缺 `rg/sg` 不隐藏工具，返回固定依赖错误。

### 6.3 必改文件

1. `internal/tooladapter/registry.go`
2. `internal/tooladapter/tooladapter_test.go`

---

## 7. 合并工具 Schema：AI 可用性强化（必须）

### 7.1 `lsp_file` 条件 required

使用 `oneOf` 按 action 约束（保留 `diagnostics` 空 `file_path`）。

### 7.2 `lsp_inspect` 条件 required

必填统一：`action,file_path,line,column`。

### 7.3 `lsp_xref` 条件 required（oneOf）

1. `references`：`action,file_path,line,column`
2. `implementations`：`action,file_path,line,column`
3. `symbols`：`action,query` + exactly one of `file_path|language`

### 7.4 `lsp_grep` 条件 required（oneOf）

1. `text`：`action,query`
2. `ast_pattern`：`action,pattern,language`

### 7.5 `lsp_structure` 条件 required（oneOf）

1. `outline`：`action,file_path`
2. `call_hierarchy`：`action,file_path,line,column,direction(incoming|outgoing|both)`
3. `type_hierarchy`：`action,file_path,line,column,direction(supertypes|subtypes|both)`

### 7.6 `lsp_edit` 条件 required（oneOf）

1. `rename`：`action,file_path,line,column,new_name`
2. `code_action`：`action,file_path,line,column`
3. `format`：`action,file_path`

### 7.7 `lsp_completion`

必填：`file_path,line,column`。

---

## 8. 结构化错误契约（生产必做）

### 8.1 统一返回模型

```json
{
  "ok": false,
  "error": {
    "code": "DEPENDENCY_MISSING",
    "message": "sg not found in PATH",
    "category": "dependency",
    "retryable": false,
    "hint": "install ast-grep or use action=text",
    "details": {"binary": "sg"}
  },
  "meta": {"tool": "lsp_grep", "action": "ast_pattern"}
}
```

### 8.2 错误码集合（必须）

1. `INVALID_ACTION`
2. `INVALID_ARGUMENT`
3. `DEPENDENCY_MISSING`
4. `TOOL_TIMEOUT`
5. `PATH_OUT_OF_ROOT`
6. `LSP_UNAVAILABLE`
7. `NOT_FOUND`
8. `INTERNAL_ERROR`
9. `UNKNOWN_TOOL`

### 8.3 测试要求

1. 错误路径断言 `error.code`。
2. 缺依赖断言 `DEPENDENCY_MISSING`。
3. 缺参数断言 `INVALID_ARGUMENT`。
4. 旧工具名断言 `UNKNOWN_TOOL`。

---

## 9. P2 路由与文件改动清单（完整）

### 9.1 核心路由

新增 `internal/tools/lsp_merged.go`：

1. `routeFile`
2. `routeInspect`
3. `routeXRef`
4. `routeGrep`
5. `routeStructure`
6. `routeEdit`
7. `lsp_completion` 直连

### 9.2 工具层改动

1. 修改 `internal/tools/lsp_tools.go`：`LSPTools()` 返回 7 schema。
2. 修改 `internal/tools/lsp_tools.go`：`RegisterLSPHandlers()` 注册 7 handlers。
3. 修改 `internal/tools/lsp_tools.go`：`LSPExtTools()` 返回空。
4. 删除 `internal/tools/lsp_ext_actions.go`
5. 删除 `internal/tools/lsp_ext_hierarchy.go`
6. 删除 `internal/tools/lsp_ext_semantic.go`
7. 删除 `internal/tools/lsp_ext_xref.go`
8. 删除 `internal/tools/lsp_ext_search.go`

### 9.3 删除老接口实现与旧入口（强制）

1. 删除 `LSPHandlerProvider` 中旧 21 方法签名。
2. 删除 `ToolHandlers` 对旧工具名方法实现（或迁移后删除废弃文件）。
3. 删除 runtime 注册中的旧工具名绑定。
4. 删除所有 alias/shim 代码与测试，不保留兼容层。

### 9.4 其它必改

1. `internal/tooladapter/registry.go`
2. `internal/tooladapter/tooladapter_test.go`
3. `internal/apiserver/server_dynamic_tools.go`
4. `internal/apiserver/server_dynamic_tools_threadid_test.go`
5. `internal/lsp/tool_handlers_hints.go`
6. `internal/lsp/tool_handlers_hints_test.go`
7. `internal/apiserver/p2_lsp_migration_test.go`
8. `internal/tools/schema_compat_test.go`
9. `internal/tools/testdata/tool_schemas.golden.json`
10. `.agent/` 下 workflow/prompt 文档
11. `internal/apiserver/commonadapter/skills.go`
12. `internal/store/prompt_template.go`

---

## 10. 实施步骤

### 10.0 P0（先做，预计 0.5 天）

1. 生成并提交 `P0-pre` baseline 与守卫。
2. 将 `P0-pre` 接入 CI 阻断链路。
3. `P0-pre` 全绿后才允许开始 P1。

### 10.1 P1（预计 1.5 天）

1. 新增 search ext + handlers。
2. 完成 `LSPHandlerProvider` 接口增量。
3. 同步补齐所有实现体/测试桩（与步骤 2 同 commit）。
4. 实施 DRY 收敛。

### 10.2 P2（预计 1.5 天）

1. 建立 7 合并工具与 action 路由。
2. 引入结构化错误契约。
3. 下线 ext 文件并迁移 schema。
4. 删除旧工具入口和老接口实现（不做兼容）。
5. 完成旧工具名扫描守卫与全量替换。
6. 更新守卫测试与 golden。

### 10.3 Cutover（P2 收口后，预计 0.5 天）

1. 满足 0.7 切换门槛。
2. 生成并提交 `P0-post` baseline 与守卫。
3. CI 阻断从 `P0-pre` 切换为 `P0-post`。
4. `P0-pre` 降级为历史回放。

---

## 11. 测试矩阵（执行闭环）

### 11.0 P0-pre

1. 改造前 schema/gating/旧语义稳定。

### 11.1 P0-post

1. 7 工具 schema 稳定。
2. `hasAvailableServer=false` 时 `lsp_grep` 暴露稳定。
3. `lsp_file action=change` 新语义稳定。
4. 旧工具名残留为零且调用返回 `UNKNOWN_TOOL`。

### 11.2 包级与契约

1. `internal/lsp` 行为与错误码回归。
2. `internal/tooladapter` 注册/gating 回归。
3. `internal/apiserver` diff 语义迁移回归。
4. 结构化错误码断言回归。
5. 文本扫描门禁回归。

### 11.3 验收场景

1. LSP 可用：7 工具可见。
2. LSP 不可用：`lsp_grep` 仍可见。
3. `rg/sg` 缺失：返回 `DEPENDENCY_MISSING`。
4. `lsp_grep` 失败不出现 `lsp manager unavailable`。
5. 旧工具名调用返回 `UNKNOWN_TOOL`。

---

## 12. 验收标准

### 12.0 P0-pre

1. `P0-pre` 全绿并阻断链路生效。

### 12.1 P1

1. 新搜索能力可用。
2. 缺依赖时返回固定/结构化错误。
3. 路径、超时、截断与上限生效。
4. 接口增量后实现体/桩编译通过。

### 12.2 P2

1. 工具收敛到 7。
2. action 级条件 schema 生效。
3. `lsp_grep` 与 LSP 可用性解耦。
4. `diagnostics` 空路径能力保留。
5. 结构化错误契约生效。
6. prompts/templates 无旧工具名残留。
7. 老接口实现、旧绑定、兼容层代码全部删除。

### 12.3 Cutover / P0-post

1. `P0-post` 全绿并接管 CI 阻断。
2. `P0-pre` 降级为非阻断。
3. P2 后回归默认执行 `P0-post`。

---

## 13. 执行门禁（Definition of Ready）

1. `registry` 改造路径（第 6 节）评审通过。
2. 合并工具 action 条件 schema（第 7 节）评审通过。
3. 结构化错误契约（第 8 节）评审通过。
4. 删除兼容层与旧接口实现方案（第 9.3 节）评审通过。
5. P2 影响面清单确认。
6. `P0-pre` 守卫全绿。
7. Cutover 负责人与切换窗口确认。

---

## 14. 发布与 SLO 门禁（生产必做）

### 14.1 灰度策略

1. Phase A：5%，观察 30 分钟。
2. Phase B：25%，观察 2 小时。
3. Phase C：100%，观察 24 小时。

### 14.2 每阶段 SLO 门槛

1. `tool_error_rate` < 1.0%。
2. `lsp_grep_timeout_rate` < 0.5%。
3. `dynamic_tool_p95_latency` < 2000ms。
4. `unknown_tool_rate` < 0.1%（仅允许外部陈旧请求）。

### 14.3 自动回滚触发

1. 任一关键 SLO 连续 5 分钟超阈值。
2. `INTERNAL_ERROR` 突增（> 基线 3 倍）。

### 14.4 回滚剧本（强制）

1. 立即切回 `P0-pre` 阻断套件与旧路由版本。
2. 暂停新 schema 下发。
3. 回滚后 30 分钟完成指标复核。
4. RCA 完成后再进入灰度。
