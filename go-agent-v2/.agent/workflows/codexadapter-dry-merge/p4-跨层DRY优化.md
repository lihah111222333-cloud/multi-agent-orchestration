---
description: 跨层 DRY 优化 — 使用 P0 基础设施统一替换全包旧模式
---

# P4: 跨层 DRY 优化（串行）

## 前置条件

- [ ] P1/P2/P3 全部完成
- [ ] `go build` + `go test` 通过

## 执行步骤

// turbo-all

### 步骤 1: 全包 `threadLogFields` 替换

搜索所有 `logger.FieldAgentID, threadID, logger.FieldThreadID, threadID`（或 `id`），替换为 `threadLogFields(threadID)...`

```bash
grep -rn 'FieldAgentID.*FieldThreadID' internal/apiserver/codexadapter/*.go | grep -v _test
```

### 步骤 2: 全包 `requireThreadID` 替换

搜索所有 `id := strings.TrimSpace(threadID); if id == ""` 模式，替换为 `requireThreadID`。

### 步骤 3: 全包 accessor 替换

将所有 `if a == nil || a.ctx == nil || a.ctx.Store == nil` 替换为 `if a.store() == nil`，以此类推其余 4 个。

### 步骤 4: nil-guard 精准清理（禁止激进删除）

仅删除满足以下条件的 nil-guard：
- 已被 `normalizeDeps` 明确兜底；
- 且存在回归检查覆盖（`compatibility-checklist.md` 可验证）。

禁止一次性删除所有 `a == nil || a.ctx == nil || ...` 守卫；对运行时对象守卫保留最小必要集。

### 步骤 5: 空行/separator 注释瘦身

删除冗余 `// ========` 分隔注释、合并连续空行、移除不再需要的 section header。

### 步骤 6: 命名一致性检查

确保同层命名统一：
- thread 层：`thread_*.go`
- turn 层：`turn_*.go` + `slash_command.go`
- tracker 层：`turn_tracker*.go`

### 步骤 7: 编译测试

```bash
go build ./...
go test -v ./internal/apiserver/codexadapter/...
go test ./...
go vet ./...
```

### 步骤 8: 提交

```bash
git add -A internal/apiserver/codexadapter/
git commit -m "refactor(codexadapter): cross-layer DRY - accessors, threadLogFields, requireThreadID"
```

## 完成标准

- [ ] nil-guard 已按“最小必要守卫”原则清理（非一刀切）
- [ ] `threadLogFields` 替换 20+ 处
- [ ] `requireThreadID` 替换 15+ 处
- [ ] 同功能层命名风格一致
- [ ] 空行比例从 22% 降到 ~15%
- [ ] `go build` + `go test` + `go vet` 全部通过
