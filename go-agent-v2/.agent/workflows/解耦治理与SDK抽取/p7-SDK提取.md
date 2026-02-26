---
description: P7 SDK 提取（优化版）— 分层迁移 + 子包保持 + 可回滚门禁
---

# P7: SDK 提取（优化版）

> **串行执行** — 等待 P6 集成验证通过

## 前置条件

- [ ] P6 集成验证全部通过
- [ ] tools 依赖边界校验通过
- [ ] difftracker 依赖边界校验通过
- [ ] lsp 依赖边界校验通过
- [ ] bus 依赖边界校验通过（`internal/bus` 无 `internal/codex`）

## 关键约束（必须遵守）

1. 不把 `agentcore` + `codex` 扁平合并到同一 package（会引发同名符号/自引用冲突）。
2. 迁移使用 `git mv` 保留历史，禁止 `cp + rm -rf` 大爆炸。
3. 每个子阶段完成后必须可编译、可测试，再进入下一子阶段。
4. 每次 `git mv` 后立刻完成对应 import 替换并验证；任何一步失败都可直接回滚当前子阶段。
5. 批量替换仅覆盖业务源码目录，排除 `vendor/.agent/.tmp`。
6. import 替换脚本仅替换 import 声明行，禁止盲替任意字符串字面量（避免破坏 guardrail 测试语义）。

## 执行步骤

// turbo-all

### Phase 2A: `pkg/codexsdk`（保持子包）

> [!WARNING]
> `agentcore/phase1_contract_test.go` 同时 import `internal/agentcore` 和 `internal/codex`。
> `git mv` 后此 test 文件会位于 `pkg/codexsdk/agentcore/`，但其对 `internal/codex` 的 import 也需要同步替换。
> 下方 import 替换脚本已覆盖此情况。

1. 迁移目录（保留原 package 名）
   ```bash
   mkdir -p pkg/codexsdk
   git mv internal/agentcore pkg/codexsdk/agentcore
   git mv internal/codex pkg/codexsdk/codex
   ```

2. 替换 import
   ```bash
   rg -l -0 'github.com/multi-agent/go-agent-v2/internal/agentcore' --glob '*.go' --glob '!vendor/**' --glob '!.agent/**' --glob '!.tmp/**' | \
     while IFS= read -r -d '' f; do
       perl -pi -e 's|^(\s*(?:import\s+)?(?:[A-Za-z_][A-Za-z0-9_]*\s+)?")github.com/multi-agent/go-agent-v2/internal/agentcore("(\s*//.*)?)$|$1github.com/multi-agent/go-agent-v2/pkg/codexsdk/agentcore$2|g' "$f"
     done
   rg -l -0 'github.com/multi-agent/go-agent-v2/internal/codex' --glob '*.go' --glob '!vendor/**' --glob '!.agent/**' --glob '!.tmp/**' | \
     while IFS= read -r -d '' f; do
       perl -pi -e 's|^(\s*(?:import\s+)?(?:[A-Za-z_][A-Za-z0-9_]*\s+)?")github.com/multi-agent/go-agent-v2/internal/codex("(\s*//.*)?)$|$1github.com/multi-agent/go-agent-v2/pkg/codexsdk/codex$2|g' "$f"
     done
   ```

3. 验证（特别关注迁移后的 test）
   ```bash
   # guardrail 测试需要人工复核（禁止语义漂移）
   rg -n 'internal/(agentcore|codex)' internal/apiserver --glob '*_test.go'

   go build ./...
   go test ./pkg/codexsdk/agentcore/... ./pkg/codexsdk/codex/...
   go test ./...
   ```

### Phase 2B: `pkg/toolsdk`（保持子包）

1. 迁移目录
   ```bash
   mkdir -p pkg/toolsdk
   git mv internal/lsp pkg/toolsdk/lsp
   git mv internal/tools pkg/toolsdk/tools
   git mv internal/tooladapter pkg/toolsdk/tooladapter
   ```

2. 替换 import
   ```bash
   rg -l -0 'github.com/multi-agent/go-agent-v2/internal/lsp' --glob '*.go' --glob '!vendor/**' --glob '!.agent/**' --glob '!.tmp/**' | \
     while IFS= read -r -d '' f; do
       perl -pi -e 's|^(\s*(?:import\s+)?(?:[A-Za-z_][A-Za-z0-9_]*\s+)?")github.com/multi-agent/go-agent-v2/internal/lsp("(\s*//.*)?)$|$1github.com/multi-agent/go-agent-v2/pkg/toolsdk/lsp$2|g' "$f"
     done
   rg -l -0 'github.com/multi-agent/go-agent-v2/internal/tools' --glob '*.go' --glob '!vendor/**' --glob '!.agent/**' --glob '!.tmp/**' | \
     while IFS= read -r -d '' f; do
       perl -pi -e 's|^(\s*(?:import\s+)?(?:[A-Za-z_][A-Za-z0-9_]*\s+)?")github.com/multi-agent/go-agent-v2/internal/tools("(\s*//.*)?)$|$1github.com/multi-agent/go-agent-v2/pkg/toolsdk/tools$2|g' "$f"
     done
   rg -l -0 'github.com/multi-agent/go-agent-v2/internal/tooladapter' --glob '*.go' --glob '!vendor/**' --glob '!.agent/**' --glob '!.tmp/**' | \
     while IFS= read -r -d '' f; do
       perl -pi -e 's|^(\s*(?:import\s+)?(?:[A-Za-z_][A-Za-z0-9_]*\s+)?")github.com/multi-agent/go-agent-v2/internal/tooladapter("(\s*//.*)?)$|$1github.com/multi-agent/go-agent-v2/pkg/toolsdk/tooladapter$2|g' "$f"
     done
   ```

3. 验证
   ```bash
   go build ./...
   go test ./pkg/toolsdk/lsp/... ./pkg/toolsdk/tools/... ./pkg/toolsdk/tooladapter/...
   go test ./...
   ```

### Phase 2C: `pkg/diffsdk`（保持子包）

1. 迁移目录
   ```bash
   mkdir -p pkg/diffsdk
   git mv internal/difftracker pkg/diffsdk/difftracker
   ```

2. 替换 import
   ```bash
   rg -l -0 'github.com/multi-agent/go-agent-v2/internal/difftracker' --glob '*.go' --glob '!vendor/**' --glob '!.agent/**' --glob '!.tmp/**' | \
     while IFS= read -r -d '' f; do
       perl -pi -e 's|^(\s*(?:import\s+)?(?:[A-Za-z_][A-Za-z0-9_]*\s+)?")github.com/multi-agent/go-agent-v2/internal/difftracker("(\s*//.*)?)$|$1github.com/multi-agent/go-agent-v2/pkg/diffsdk/difftracker$2|g' "$f"
     done
   ```

3. 验证
   ```bash
   go build ./...
   go test ./pkg/diffsdk/difftracker/...
   go test ./...
   ```

### Phase 2D: 最终核验

```bash
# 确认旧路径已消失（与系统语言无关）
missing=0
for d in internal/agentcore internal/codex internal/lsp internal/tools internal/tooladapter internal/difftracker; do
  if [ -d "$d" ]; then
    echo "FAIL still exists: $d"
  else
    echo "PASS removed: $d"
    missing=$((missing + 1))
  fi
done
test "$missing" -eq 6

# 确认无残留旧 import
rg -n '^\s*(import\s+)?([A-Za-z_][A-Za-z0-9_]*\s+)?\"github.com/multi-agent/go-agent-v2/internal/(agentcore|codex|lsp|tools|tooladapter|difftracker)\"' --glob '*.go' --glob '!vendor/**' --glob '!.agent/**' --glob '!.tmp/**' .
# 期望无输出

# 最终编译测试
go build ./...
go test ./...
```

## 完成标准

- [ ] `pkg/codexsdk/agentcore` + `pkg/codexsdk/codex` 可编译
- [ ] `pkg/toolsdk/lsp` + `pkg/toolsdk/tools` + `pkg/toolsdk/tooladapter` 可编译
- [ ] `pkg/diffsdk/difftracker` 可编译
- [ ] 旧 `internal/*` 对应目录全部迁移完成
- [ ] `go build ./... && go test ./...` 全部通过
- [ ] `internal/` 有效行数 <= 22,000（Stretch: < 20,000）
