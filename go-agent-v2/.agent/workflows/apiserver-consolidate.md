---
description: apiserver 顶层小文件合并（可执行版）— 40→27，按职责域与功能命名分组，含可直接运行的 Runbook
---

# Apiserver 小文件合并工作流（可执行版）

## 1. 目标与通过标准

目标：在不改变外部行为的前提下，把 `internal/apiserver` 顶层生产文件从 `40` 收敛到 `27`，并保持职责边界清晰。

通过标准（全部满足才算完成）：

1. 顶层生产文件数（`*.go` 且非 `_test.go`）为 `27`。
2. 每组合并后均通过门禁：`go build`、`go test`、`go vet`、`git diff --check`。
3. 外部契约校验采用“快照或守卫”，至少完成一种并通过。
4. 组内拼接顺序按功能命名，不按文件名字典序。

## 2. 强约束（必须遵守）

1. 仅合并同职责域的 `server_*` / `methods_*` / `tool_*` 文件。
2. 采用两阶段：阶段 A（机械迁移）后必须先过门禁，再做阶段 B（结构整理）。
3. 阶段 A 必须“整块搬运”：仅允许拼接、import 归并、删除源文件，不改函数逻辑。
4. 不修改导出函数签名、RPC method 名、JSON tag、关键日志字段。
5. 不新增循环依赖，不扩大 package 可见面。
6. 测试文件不动。
7. 行数仅作参考，不作为阻断条件。

## 3. 一次性准备（必须先执行）

```bash
set -euo pipefail

if [ -f go.mod ] && [ -d internal/apiserver ]; then
  PROJECT_ROOT="$PWD"
else
  TOP="$(git rev-parse --show-toplevel)"
  if [ -f "${TOP}/go-agent-v2/go.mod" ] && [ -d "${TOP}/go-agent-v2/internal/apiserver" ]; then
    PROJECT_ROOT="${TOP}/go-agent-v2"
  else
    PROJECT_ROOT="${TOP}"
  fi
fi
cd "$PROJECT_ROOT"
mkdir -p .agent/tmp/apiserver-consolidate

# goimports 用于拼接后自动归并/修正 import（缺失则先安装）
if ! command -v goimports >/dev/null 2>&1; then
  go install golang.org/x/tools/cmd/goimports@latest
fi

# 契约快照基线（用于“快照方案”）
go doc ./internal/apiserver > .agent/tmp/apiserver-consolidate/exports.before.txt
rg -ho 'json:"[^"]+"' internal/apiserver/*.go | sort -u > .agent/tmp/apiserver-consolidate/json_tags.before.txt

# 起始文件数检查（应为 40）
find internal/apiserver -maxdepth 1 -name '*.go' -not -name '*_test.go' | wc -l
```

## 4. 可执行函数（复制后直接用）

```bash
set -euo pipefail

if [ -f go.mod ] && [ -d internal/apiserver ]; then
  PROJECT_ROOT="$PWD"
else
  TOP="$(git rev-parse --show-toplevel)"
  if [ -f "${TOP}/go-agent-v2/go.mod" ] && [ -d "${TOP}/go-agent-v2/internal/apiserver" ]; then
    PROJECT_ROOT="${TOP}/go-agent-v2"
  else
    PROJECT_ROOT="${TOP}"
  fi
fi
cd "$PROJECT_ROOT"

gate_group() {
  local name="$1"
  echo "== gate: ${name} =="
  go build ./internal/apiserver/...
  go test ./internal/apiserver/... -count=1
  go vet ./internal/apiserver/...
  git diff --check -- internal/apiserver
}

merge_go_group() {
  # 用法：merge_go_group <target> <src1> <src2> ...
  # 规则：保留首文件 package 行；各源文件按给定顺序整块拼接；最后 goimports 归并 import。
  local target="$1"
  shift
  local srcs=("$@")

  if [ "${#srcs[@]}" -eq 0 ]; then
    echo "FAIL: no source files for ${target}"; return 1
  fi

  local tmp
  tmp="$(mktemp /tmp/apiserver-merge-XXXXXX.go)"

  {
    sed -n '1{/^package[[:space:]]\+/p;}' "${srcs[0]}"
    echo
    for f in "${srcs[@]}"; do
      echo "// ---- from $(basename "$f") ----"
      sed '1{/^package[[:space:]]\+/d;}' "$f"
      echo
    done
  } > "$tmp"

  goimports -w "$tmp"
  mv "$tmp" "$target"
}

snapshot_after_and_diff() {
  go doc ./internal/apiserver > .agent/tmp/apiserver-consolidate/exports.after.txt
  diff -u \
    .agent/tmp/apiserver-consolidate/exports.before.txt \
    .agent/tmp/apiserver-consolidate/exports.after.txt

  rg -ho 'json:"[^"]+"' internal/apiserver/*.go | sort -u > .agent/tmp/apiserver-consolidate/json_tags.after.txt
  diff -u \
    .agent/tmp/apiserver-consolidate/json_tags.before.txt \
    .agent/tmp/apiserver-consolidate/json_tags.after.txt
}

run_contract_guard_tests() {
  go test ./internal/apiserver/... -run 'TestDashboardContractsStable|TestSkillsContractsStable|TestP2ServerPayloadSplitGuard|TestP2ServerGoMethodSurface|TestP2BootstrapFunctionBoundaries|TestP2GroupedStateFieldAccessBoundaries|TestServerStateGroupShapes' -count=1
}

final_count_check() {
  local n
  n="$(find internal/apiserver -maxdepth 1 -name '*.go' -not -name '*_test.go' | wc -l | tr -d ' ')"
  echo "top-level non-test go files: ${n}"
  test "$n" = "27"
}
```

## 5. 分组执行（功能命名顺序）

### 组 1：`server_context_*.go` → `server_context.go`

功能顺序：`runtime/context core -> conn accessors -> diagnostics accessors -> turn/ui runtime accessors`

```bash
merge_go_group internal/apiserver/server_context.go \
  internal/apiserver/server_context_codex.go \
  internal/apiserver/server_context_conn_accessors.go \
  internal/apiserver/server_context_diag_accessors.go \
  internal/apiserver/server_context_turn_ui_runtime.go

rm -f \
  internal/apiserver/server_context_accessors.go \
  internal/apiserver/server_context_codex.go \
  internal/apiserver/server_context_conn_accessors.go \
  internal/apiserver/server_context_diag_accessors.go \
  internal/apiserver/server_context_turn_ui_runtime.go

gate_group group1
```

### 组 2：`server_bootstrap_*.go` → `server_bootstrap.go`

功能顺序：`runtime wiring -> state stores -> skills wiring`

```bash
merge_go_group internal/apiserver/server_bootstrap.go \
  internal/apiserver/server_bootstrap_runtime.go \
  internal/apiserver/server_bootstrap_stores.go \
  internal/apiserver/server_bootstrap_skills.go

rm -f \
  internal/apiserver/server_bootstrap_runtime.go \
  internal/apiserver/server_bootstrap_stores.go \
  internal/apiserver/server_bootstrap_skills.go

gate_group group2
```

### 组 3：`server_payload.go` + `server_payload_merge.go` → `server_payload.go`

功能顺序：`payload core -> payload merge`（`filechange tracking` 保持独立）

```bash
merge_go_group internal/apiserver/server_payload.go \
  internal/apiserver/server_payload.go \
  internal/apiserver/server_payload_merge.go

rm -f internal/apiserver/server_payload_merge.go

gate_group group3
```

### 组 4：`methods_turn.go` + `methods_turn_debug.go` → `methods_turn.go`

功能顺序：`turn core -> turn debug`

```bash
merge_go_group internal/apiserver/methods_turn.go \
  internal/apiserver/methods_turn.go \
  internal/apiserver/methods_turn_debug.go

rm -f internal/apiserver/methods_turn_debug.go

gate_group group4
```

### 组 5：`tool_providers.go` + `tool_provider_approval.go` → `tool_providers.go`

功能顺序：`provider core -> approval provider`

```bash
merge_go_group internal/apiserver/tool_providers.go \
  internal/apiserver/tool_providers.go \
  internal/apiserver/tool_provider_approval.go

rm -f internal/apiserver/tool_provider_approval.go

gate_group group5
```

### 组 6：`methods.go` + 小 methods → `methods.go`

功能顺序：`account methods -> offline list methods -> common helpers`

```bash
merge_go_group internal/apiserver/methods.go \
  internal/apiserver/methods.go \
  internal/apiserver/methods_account.go \
  internal/apiserver/methods_offline52_list.go \
  internal/apiserver/methods_helpers.go

rm -f \
  internal/apiserver/methods_account.go \
  internal/apiserver/methods_offline52_list.go \
  internal/apiserver/methods_helpers.go

gate_group group6
```

### 组 7：`methods_command.go` + `methods_skills_entry.go` → `methods_command.go`

功能顺序：`command core -> skills command entry`

```bash
merge_go_group internal/apiserver/methods_command.go \
  internal/apiserver/methods_command.go \
  internal/apiserver/methods_skills_entry.go

rm -f internal/apiserver/methods_skills_entry.go

gate_group group7
```

## 6. 最终验收（必须执行）

```bash
# 1) 再跑一遍统一门禁
gate_group final

# 2) 外部契约校验：二选一（建议先快照，失败再守卫）
# 2A: 快照方案
snapshot_after_and_diff

# 2B: 守卫方案
# run_contract_guard_tests

# 3) 文件数必须为 27
final_count_check

# 4) 信息记录（非阻断）
wc -l \
  internal/apiserver/server_context.go \
  internal/apiserver/server_bootstrap.go \
  internal/apiserver/server_payload.go \
  internal/apiserver/methods_turn.go \
  internal/apiserver/tool_providers.go \
  internal/apiserver/methods.go \
  internal/apiserver/methods_command.go
```

## 7. 一键执行（按组串行）

```bash
set -euo pipefail

if [ -f go.mod ] && [ -d internal/apiserver ]; then
  PROJECT_ROOT="$PWD"
else
  TOP="$(git rev-parse --show-toplevel)"
  if [ -f "${TOP}/go-agent-v2/go.mod" ] && [ -d "${TOP}/go-agent-v2/internal/apiserver" ]; then
    PROJECT_ROOT="${TOP}/go-agent-v2"
  else
    PROJECT_ROOT="${TOP}"
  fi
fi
cd "$PROJECT_ROOT"

# 假设你已经执行了“第 3 节准备”和“第 4 节函数定义”

# group1
merge_go_group internal/apiserver/server_context.go \
  internal/apiserver/server_context_codex.go \
  internal/apiserver/server_context_conn_accessors.go \
  internal/apiserver/server_context_diag_accessors.go \
  internal/apiserver/server_context_turn_ui_runtime.go
rm -f internal/apiserver/server_context_accessors.go internal/apiserver/server_context_codex.go internal/apiserver/server_context_conn_accessors.go internal/apiserver/server_context_diag_accessors.go internal/apiserver/server_context_turn_ui_runtime.go
gate_group group1

# group2
merge_go_group internal/apiserver/server_bootstrap.go \
  internal/apiserver/server_bootstrap_runtime.go \
  internal/apiserver/server_bootstrap_stores.go \
  internal/apiserver/server_bootstrap_skills.go
rm -f internal/apiserver/server_bootstrap_runtime.go internal/apiserver/server_bootstrap_stores.go internal/apiserver/server_bootstrap_skills.go
gate_group group2

# group3
merge_go_group internal/apiserver/server_payload.go internal/apiserver/server_payload.go internal/apiserver/server_payload_merge.go
rm -f internal/apiserver/server_payload_merge.go
gate_group group3

# group4
merge_go_group internal/apiserver/methods_turn.go internal/apiserver/methods_turn.go internal/apiserver/methods_turn_debug.go
rm -f internal/apiserver/methods_turn_debug.go
gate_group group4

# group5
merge_go_group internal/apiserver/tool_providers.go internal/apiserver/tool_providers.go internal/apiserver/tool_provider_approval.go
rm -f internal/apiserver/tool_provider_approval.go
gate_group group5

# group6
merge_go_group internal/apiserver/methods.go internal/apiserver/methods.go internal/apiserver/methods_account.go internal/apiserver/methods_offline52_list.go internal/apiserver/methods_helpers.go
rm -f internal/apiserver/methods_account.go internal/apiserver/methods_offline52_list.go internal/apiserver/methods_helpers.go
gate_group group6

# group7
merge_go_group internal/apiserver/methods_command.go internal/apiserver/methods_command.go internal/apiserver/methods_skills_entry.go
rm -f internal/apiserver/methods_skills_entry.go
gate_group group7

# final
gate_group final
snapshot_after_and_diff || run_contract_guard_tests
final_count_check
```

## 8. 失败处理（可执行）

```bash
# 当前组失败时：只回退未提交改动
git restore --worktree --staged internal/apiserver

# 若已按组提交：回退最近一组提交（示例）
# git revert --no-edit HEAD
```
