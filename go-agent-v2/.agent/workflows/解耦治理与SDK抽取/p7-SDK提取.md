---
description: P7 SDK 提取（收尾执行版）— 断点续跑 + 幂等迁移
---

# P7: SDK 提取（收尾版）

> 串行执行。当前仓库已部分迁移到 `pkg/`，本阶段必须按“探测现状 -> 条件迁移”执行。

## 前置条件（必须）

- [ ] P3 已完成并通过门禁
- [ ] P6-lite 回归已通过（`go build ./... && go vet ./... && go test ./...`）
- [ ] 在独立 worktree 执行
- [ ] 工作树干净（必须）：
  ```bash
  test -z "$(git status --porcelain)" || { echo "FAIL: dirty worktree"; exit 1; }
  ```

## 关键约束

1. 仅用 `git mv`，禁止 `cp + rm -rf`。
2. 每次迁移后立刻替换 import 并验证。
3. 若某目录已迁移（source 不存在），必须 `SKIP` 而不是失败。
4. 若 source 和 destination 同时存在，直接 `FAIL`（先人工处理冲突）。
5. import 批量替换只替换 import 语句，排除 `vendor/.agent/.tmp`。

## 执行步骤

// turbo-all

### 步骤 0: 现状探测

```bash
for d in \
  internal/agentcore internal/codex internal/lsp internal/tools internal/tooladapter internal/difftracker \
  pkg/codexsdk/agentcore pkg/codexsdk/codex pkg/toolsdk/lsp pkg/toolsdk/tools pkg/toolsdk/tooladapter pkg/diffsdk/difftracker
do
  if [ -d "$d" ]; then
    echo "EXISTS  $d"
  else
    echo "MISSING $d"
  fi
done
```

### 步骤 1: 条件迁移（幂等）

```bash
move_if_exists() {
  src="$1"
  dst="$2"
  if [ -d "$src" ] && [ -d "$dst" ]; then
    echo "FAIL both exist: $src and $dst"
    return 1
  fi
  if [ -d "$src" ]; then
    mkdir -p "$(dirname "$dst")"
    git mv "$src" "$dst"
    echo "MOVED $src -> $dst"
  else
    echo "SKIP  $src (already migrated)"
  fi
}

move_if_exists internal/agentcore   pkg/codexsdk/agentcore
move_if_exists internal/codex       pkg/codexsdk/codex
move_if_exists internal/lsp         pkg/toolsdk/lsp
move_if_exists internal/tools       pkg/toolsdk/tools
move_if_exists internal/tooladapter pkg/toolsdk/tooladapter
move_if_exists internal/difftracker pkg/diffsdk/difftracker
```

### 步骤 2: import 替换（仅替换 import 行）

```bash
replace_import_path() {
  from="$1"
  to="$2"
  rg -l -0 "$from" --glob '*.go' --glob '!vendor/**' --glob '!.agent/**' --glob '!.tmp/**' | \
    while IFS= read -r -d '' f; do
      FROM="$from" TO="$to" perl -pi -e '
        BEGIN {
          $from = $ENV{"FROM"};
          $to = $ENV{"TO"};
        }
        s|^(\s*(?:import\s+)?(?:[A-Za-z_][A-Za-z0-9_]*\s+)?")\Q$from\E("(\s*//.*)?)$|$1$to$2|g
      ' "$f"
    done
}

replace_import_path 'github.com/multi-agent/go-agent-v2/internal/agentcore'   'github.com/multi-agent/go-agent-v2/pkg/codexsdk/agentcore'
replace_import_path 'github.com/multi-agent/go-agent-v2/internal/codex'       'github.com/multi-agent/go-agent-v2/pkg/codexsdk/codex'
replace_import_path 'github.com/multi-agent/go-agent-v2/internal/lsp'         'github.com/multi-agent/go-agent-v2/pkg/toolsdk/lsp'
replace_import_path 'github.com/multi-agent/go-agent-v2/internal/tools'       'github.com/multi-agent/go-agent-v2/pkg/toolsdk/tools'
replace_import_path 'github.com/multi-agent/go-agent-v2/internal/tooladapter' 'github.com/multi-agent/go-agent-v2/pkg/toolsdk/tooladapter'
replace_import_path 'github.com/multi-agent/go-agent-v2/internal/difftracker' 'github.com/multi-agent/go-agent-v2/pkg/diffsdk/difftracker'
```

### 步骤 3: 分段验证

```bash
go build ./...
go test ./pkg/codexsdk/agentcore/... ./pkg/codexsdk/codex/...
go test ./pkg/toolsdk/lsp/... ./pkg/toolsdk/tools/... ./pkg/toolsdk/tooladapter/...
go test ./pkg/diffsdk/difftracker/...
go test ./...
```

### 步骤 4: 最终核验

```bash
# 旧目录应全部消失
for d in internal/agentcore internal/codex internal/lsp internal/tools internal/tooladapter internal/difftracker; do
  [ ! -d "$d" ] || { echo "FAIL still exists: $d"; exit 1; }
done

# 不应残留旧 import
rg -n '^\s*(import\s+)?([A-Za-z_][A-Za-z0-9_]*\s+)?"github.com/multi-agent/go-agent-v2/internal/(agentcore|codex|lsp|tools|tooladapter|difftracker)"' \
  --glob '*.go' --glob '!vendor/**' --glob '!.agent/**' --glob '!.tmp/**' . && \
  { echo "FAIL old imports still exist"; exit 1; } || true

go build ./...
go test ./...
```

## 完成标准

- [ ] `pkg/codexsdk/agentcore` 与 `pkg/codexsdk/codex` 可编译可测试
- [ ] `pkg/toolsdk/*` 与 `pkg/diffsdk/difftracker` 可编译可测试
- [ ] `internal/agentcore|codex|lsp|tools|tooladapter|difftracker` 目录全部不存在
- [ ] 无残留旧 import `internal/(agentcore|codex|lsp|tools|tooladapter|difftracker)`
- [ ] `go build ./... && go test ./...` 全部通过

## 回滚建议

```bash
git switch -c recover-p7 <checkpoint-commit>
git cherry-pick <good-commits>
```
