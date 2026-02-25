---
description: Codexadapter 重构功能等价/无回归检查清单（每批次必跑）
---

# Compatibility Checklist

> 目标：确保重构仅改变代码组织，不改变外部可观测行为。

## 使用方式

1. 每一批重构（每次“下刀”）完成后执行一次。
2. 合并前（P5）再执行一次完整检查。
3. 任何一项失败，先修复再继续下一批。

## A. 构建与测试基线

- [ ] `go build ./...` 通过
- [ ] `go test ./internal/apiserver/codexadapter/... -count=1` 通过
- [ ] `go test ./internal/apiserver -count=1` 通过
- [ ] `go test ./... -count=1` 通过
- [ ] `go vet ./...` 通过

## A.1 契约基线快照（P0 创建）

- [ ] 已创建目录：`.agent/workflows/codexadapter-dry-merge/baseline`
- [ ] 已生成：`baseline/adapter_methods.txt`
- [ ] 已生成：`baseline/events.txt`
- [ ] 已生成：`baseline/payload_keys.txt`

建议命令：

```bash
BASE=.agent/workflows/codexadapter-dry-merge/baseline
mkdir -p "$BASE"

grep '^func (a \*Adapter)' internal/apiserver/codexadapter/*.go | grep -v _test.go | sort > "$BASE/adapter_methods.txt"
grep -R --line-number '"turn/completed"\|"thread/messages/page"' internal/apiserver/codexadapter/*.go | sort > "$BASE/events.txt"
grep -R --line-number '"threadId"\|"turnId"\|"status"\|"reason"\|"lastAgentMessage"\|"summary"' internal/apiserver/codexadapter/*.go | sort > "$BASE/payload_keys.txt"
```

## B. 对外 API 兼容

- [ ] `Adapter` 对外可调用方法签名未被破坏（调用点可编译）
- [ ] `apiserver` 到 `codexadapter` 的调用入口保持兼容
- [ ] 未引入未计划的导出符号变化（导出预算受控）

建议命令：

```bash
grep '^func (a \*Adapter)' internal/apiserver/codexadapter/*.go | grep -v _test.go
```

## C. 事件名兼容

- [ ] 事件名保持不变（至少覆盖以下关键事件）
- [ ] `turn/completed`
- [ ] `thread/messages/page`

建议命令：

```bash
grep -R --line-number '"turn/completed"\|"thread/messages/page"' internal/apiserver/codexadapter/*.go
```

## D. Payload Key 兼容

- [ ] 关键字段名保持兼容：`threadId`、`turnId`、`status`、`reason`
- [ ] summary 相关字段兼容：`lastAgentMessage`、`summary`
- [ ] 未无说明删除关键字段

建议命令：

```bash
grep -R --line-number '"threadId"\|"turnId"\|"status"\|"reason"\|"lastAgentMessage"\|"summary"' internal/apiserver/codexadapter/*.go
```

## E. Slash 命令兼容

- [ ] `SendSlashCommandFromRawParams` 可用
- [ ] `SendSlashCommandWithArgs(params, command, argKey)` 兼容入口可用
- [ ] `ThreadSkillsList()` 兼容入口可用

## F. 错误语义兼容

- [ ] 无说明变更错误 caller 名称（如 `Server.xxx`）
- [ ] 无说明变更关键错误文案语义（required/not found/not available）
- [ ] 若为 bugfix，已在本批次变更说明中记录

## G. Tracker/Thread 特殊 guardrail

- [ ] `thread_archive_utils_guardrail_test.go` 通过
- [ ] `tracked_turn_shape_guardrail_test.go` 通过
- [ ] 若删除 `thread_archive_utils.go`，已先迁移并更新 guardrail

## H. 全覆盖归属（必须为 0 漏项）

- [ ] `codexadapter/*.go` 文件均已归属到 P0/P1/P2/P3/P5
- [ ] 未归属文件数 = 0

可直接复用 `p5-集成验证.md` 的“步骤 5.1 全覆盖零遗漏检查”脚本。

## H.1 契约对比结果（每批次）

- [ ] 已生成目录：`.agent/workflows/codexadapter-dry-merge/current`
- [ ] `adapter_methods.txt` 与 baseline 无差异（或已说明）
- [ ] `events.txt` 与 baseline 无差异（或已说明）
- [ ] `payload_keys.txt` 与 baseline 无差异（或已说明）

## I. 结果记录

### 本批次信息

- 批次标识：`P5-集成验证`
- 执行人：`codex`
- 日期：`2026-02-26`

### 检查结论

- [ ] 通过（允许进入下一批）
- [x] 不通过（先修复）

### 备注（必填：若有行为变更）

`build/test/vet 与关键事件/payload 兼容检查通过；baseline 文本 diff 非零，且 max-file/effective-LOC/export-budget 未达目标。详见 summary.md。`
