---
description: P4 apiserver 顶层整理 — UI 逻辑归位、状态分组精简
---

# P4: apiserver 顶层整理

> **⚡ 可并行** — 与 P3、P5 同时执行（第二波）

## 前置条件

- [ ] P1.5 diff 独立已完成（diff 文件已缩减）

## 任务范围

### 需要修改的文件

- `internal/apiserver/methods_ui_code_open.go` (612行) → 业务逻辑默认移入 `internal/dashboard/`
- `internal/apiserver/methods_ui_state.go` (513行) → 同上
- `internal/apiserver/server_state_groups.go` (760行) → 精简，提取通用计算
- `internal/dashboard/` — 新建/扩展服务文件接收 UI 业务逻辑

### 契约边界（新增，必须满足）

- `internal/dashboard/*` 禁止 import `internal/apiserver`、`internal/uistate`。
- `apiserver` 通过接口/函数依赖注入调用 `dashboard`，不得把 `*Server` 直接传入 `dashboard`。
- `apiserver` 与 `dashboard` 之间仅传递 DTO/基础类型，避免双向耦合。
- `dashboard` 已有 3 个文件基于 gin 框架，新增文件保持 gin 风格，不引入其他 HTTP 框架。

### 禁止触碰的文件 ⚠️

- `internal/apiserver/codexadapter/*` (P3 负责)
- `internal/codex/*` (P5 负责)
- `internal/uistate/runtime_event_handlers.go` (P5 负责，P4 不修改)
- `internal/lsp/*` (P2 已完成)
- `internal/tools/*` (P1 已完成)

## 执行步骤

// turbo-all

### 步骤 1: methods_ui_code_open 移出

1. 在 `internal/dashboard/` 创建 `code_open_service.go`，定义服务入口和 DTO
2. 将 `methods_ui_code_open.go` 的核心业务逻辑移入
3. apiserver 保留薄 RPC handler 委托调用（通过接口注入）
4. 验证: `go build ./...`

### 步骤 2: methods_ui_state 移出

1. 在 `internal/dashboard/` 创建 `state_service.go`
2. 将 `methods_ui_state.go` 的状态管理逻辑移入
3. apiserver 保留委托调用（通过接口注入）
4. 验证: `go build ./...`

### 步骤 3: server_state_groups 精简

1. 提取状态分组的通用计算到 `internal/dashboard/`
2. apiserver 保留调度和结果组装
3. 验证

### 最终验证

```bash
go build ./...
go test ./internal/apiserver/...
go test ./internal/uistate/...
go test ./...
# dashboard 边界检查（必须 PASS）
if rg -n '"github.com/multi-agent/go-agent-v2/internal/(apiserver|uistate)"' internal/dashboard; then
  echo "FAIL dashboard boundary"
else
  echo "PASS dashboard boundary"
fi
```

## 风险控制

- 避免 `apiserver -> uistate -> apiserver` 循环依赖，P4 默认不改 `internal/uistate/*`。
- 若必须落在 `uistate`，仅允许新建纯数据/纯算法 helper，不得引用 `*apiserver.Server`。

## 完成标准

- [ ] `methods_ui_code_open.go` 有效行数从 561 缩减到 <=200（委托调用为主）
- [ ] `methods_ui_state.go` 有效行数从 471 缩减到 <=200
- [ ] `server_state_groups.go` 有效行数从 668 缩减到 <=520
- [ ] `internal/apiserver`（不含 `codexadapter`）有效行数从 ~7,338 降至 <=6,300
- [ ] `dashboard` 包无 `internal/apiserver`/`internal/uistate` import
- [ ] `go build ./...` 通过
