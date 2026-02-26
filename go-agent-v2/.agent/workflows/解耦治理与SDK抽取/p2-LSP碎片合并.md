---
description: P2 LSP 碎片文件合并 — 38 个文件按职责域合并为 ~24 个
---

# P2: LSP 碎片文件合并

> **⚡ 可并行** — 与 P1、P1.5 同时执行

## 前置条件

- [ ] P0 准备阶段已完成

## 任务范围

### 需要修改的文件 (同包合并)

全部在 `internal/lsp/` 内操作。同包合并不影响外部 import。

### 禁止触碰的文件 ⚠️

- `internal/tools/*` (P1 负责)
- `internal/tooladapter/*` (P1 负责)
- `internal/difftracker/*` (P1.5 负责)
- `internal/apiserver/*` (P4 负责)
- `internal/lsp/tool_handlers_merged.go`（已承担聚合路由，避免重复改造）

## 执行步骤

// turbo-all

### 合并组 1: client 工具方法 → `client_tools.go`

1. 将以下文件内容合入 `client_hierarchy_tools.go`（最大的那个，175 行）
   - `client_actions_tools.go` (83 行)
   - `client_semantic_tools.go` (79 行)
   - `client_xref_tools.go` (41 行)

2. 重命名 `client_hierarchy_tools.go` → `client_tools.go`

3. 删除原始文件: `client_actions_tools.go`, `client_semantic_tools.go`, `client_xref_tools.go`

4. 验证: `go build ./internal/lsp/...`

### 合并组 2: manager 工具方法 → `manager_tools.go`

1. 合并以下文件为 `manager_tools.go`
   - `manager_actions_tools.go` (95 行)
   - `manager_semantic_tools.go` (29 行)
   - `manager_xref_tools.go` (29 行)
   - `manager_hierarchy_tools.go` (71 行)

2. 删除原始 4 文件

3. 验证: `go build ./internal/lsp/...`

### 合并组 3: protocol 扩展 → 合入 `protocol_ext_common.go`

1. 将以下文件内容追加到 `protocol_ext_common.go`
   - `protocol_ext_hierarchy.go` (59 行)
   - `protocol_ext_xref.go` (4 行)
   - `protocol_ext_actions.go` (185 行)
   - `protocol_ext_semantic.go` (186 行)

2. 删除原始 4 文件

3. 验证: `go build ./internal/lsp/...`

### 合并组 4: handler 碎片 → `tool_handlers_misc.go`

1. 合并以下文件为 `tool_handlers_misc.go`
   - `tool_handlers.go` (33 行)
   - `tool_handlers_bind.go` (9 行)
   - `tool_handlers_aux.go` (92 行)
   - `tool_handlers_hints.go` (79 行)
   - `tool_handlers_hierarchy.go` (84 行)
   - `tool_handlers_semantic.go` (83 行)

2. 删除原始 6 文件

3. 验证: `go build ./internal/lsp/...`

### 最终验证

```bash
go build ./internal/lsp/...
go test ./internal/lsp/...
go build ./...
```

## 完成标准

- [ ] `internal/lsp/` 文件数从 38 减至 ~24
- [ ] 无外部 import 变化
- [ ] `go build ./...` 通过
- [ ] `go test ./internal/lsp/...` 通过
- [ ] 不修改其他 Agent 负责的文件
