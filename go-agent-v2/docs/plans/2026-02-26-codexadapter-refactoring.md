# Codexadapter 代码精简实现计划

> **给 Claude:** 必须使用 @执行计划 逐任务实现此计划。

**目标:** 对 `codexadapter` 包进行代码精简 — 合并相同职责、拆分不同职责、消除 DRY 违规、提升代码优雅度。

**架构:** 当前包 26 个文件共 7127 行（含测试）。核心问题是：(1) 超大文件混合了不同职责；(2) "纯函数 + 适配器方法" 重复模式产生大量无价值胶水代码；(3) 职责边界不够清晰。重构策略是按职责重新组织文件，消除冗余包装层，同时保证所有公开 API 签名不变。

**技术栈:** Go 1.22+, 纯内部重构（无外部依赖变更）

---

## 现状分析

| 文件 | 行数 | 职责 | 问题 |
|:-----|-----:|:-----|:-----|
| `turn_runtime.go` | 991 | Turn 生命周期 + Interrupt + Resume + Skill/Prompt + LSP + FuzzySearch | **超大文件，混合 5+ 个不相关职责** |
| `turn_tracker_helpers.go` | 924 | TrackedTurn 纯函数工具集 | **与 `turn_tracker.go` 同职责但人为拆分** |
| `turn_tracker.go` | 604 | TrackedTurn 状态机 (Adapter 方法) | 同上 |
| `thread_archive.go` | 620 | 归档核心逻辑 | 合理 |
| `thread_messages.go` | 491 | 消息加载 + 分页 + Hydration | 较大但职责集中 |
| `thread_turn_entry.go` | 403 | Thread/Turn 入口方法 (Start/Resume/Fork/List) | 合理 |
| `turn_prepare.go` | 380 | Turn 提交准备 + Skill 注入 | 合理 |
| `thread_archive_utils.go` | 378 | 归档工具函数 | 合理 |
| `thread_usecases.go` | 318 | Thread 用例 (Messages/Archive) + 类型定义 | 类型定义应独立 |
| `thread_history.go` | 249 | 线程历史查找 | 合理 |
| `runtime_context.go` | 85 | Deps 委托方法 | **重复 nil-guard 模式** |
| `turn_skill_match.go` | 144 | Skill 匹配逻辑 | **纯函数+适配器包装冗余** |

## 问题清单

### P1: `turn_runtime.go` 超大文件（991行，5+职责混合）

该文件混合了以下不相关职责：
- **Turn生命周期** (`TurnStart`, `TurnSteer`, `startTurnSubmissionAndTrack`) — 应留在原文件
- **Interrupt流程** (`TurnInterrupt`, `TurnForceComplete`, `WaitInterruptOutcome`, `NormalizeInterruptState`...) — **应拆分为 `turn_interrupt.go`**
- **Resume/候选流程** (`BuildResumeCandidates`, `TryResumeCandidates`, `PreviewResumeCandidates`...) — **应拆分为 `turn_resume.go`**  
- **Skill/Prompt构建** (`BuildSelectedSkillPrompt`, `ResolveLSPUsagePromptHint`, `PrependLSPAvailabilityWarning`...) — **应拆分为 `turn_prompt.go`**
- **FuzzyFileSearch** — **应拆分到 `turn_prompt.go` 或独立文件**

### P2: `turn_tracker_helpers.go` (924行) + `turn_tracker.go` (604行) 人为拆分

两个文件都是 TrackedTurn 相关，按 "纯函数 vs Adapter方法" 拆分。但这不是正确的职责边界。应按功能子领域重新组织：
- **Turn状态机核心** → `turn_tracker.go`（Begin/Complete/Wait/Finalize + 对应纯函数）
- **Stall检测** → `turn_tracker_stall.go`（CheckStall/HandleGrace/AutoInterrupt + Heartbeat）
- **Summary缓存** → `turn_tracker_summary.go`（RememberSummary/Lookup/Inject/Capture）
- **Payload解析工具** → `turn_tracker_payload.go`（ExtractXxx/ThreadStatusTerminal/DiagKV）

### P3: "纯函数 + 适配器一行包装" 冗余模式

以下方法仅做 `return PureFn(a.xxx, ...)` 的委托，不增加任何逻辑价值：

| 文件 | 适配器方法 | 纯函数 | 行为 |
|:-----|:-----------|:-------|:-----|
| `turn_skill_match.go` | `*Adapter.CollectAutoMatchedSkillMatches()` | `CollectAutoMatchedSkillMatches()` | **完全直传** |
| `turn_skill_match.go` | `*Adapter.RenderAutoMatchedSkillPrompt()` | `RenderAutoMatchedSkillPrompt()` | 仅绑定 deps |
| `turn_tracker_helpers.go` | `*Adapter.TrackedTurnSummaryFromPayload()` | `TrackedTurnSummaryFromPayload()` | **完全直传** |
| `turn_tracker_helpers.go` | `*Adapter.ExtractTrackedString()` | `ExtractTrackedString()` | **完全直传** |
| `turn_tracker_helpers.go` | `*Adapter.TrackedTurnTerminalFromEvent()` | `TrackedTurnTerminalFromEvent()` | **完全直传** |
| `turn_runtime.go` | `*Adapter.FuzzyFileSearch()` | `FuzzyFileSearch()` | **完全直传** |
| `turn_runtime.go` | `*Adapter.BuildSelectedSkillPrompt()` | `BuildSelectedSkillPrompt()` | 仅绑定 deps |
| `turn_runtime.go` | `*Adapter.ResolveLSPUsagePromptHint()` | `ResolveLSPUsagePromptHint()` | 仅绑定 deps |
| `turn_runtime.go` | `*Adapter.PrependLSPAvailabilityWarning()` | `PrependLSPAvailabilityWarning()` | 仅绑定 deps |
| `turn_runtime.go` | `*Adapter.WaitInterruptOutcome()` | `WaitInterruptOutcome()` | 仅绑定 deps |
| `turn_runtime.go` | `*Adapter.ReadThreadRuntimeState()` | `ReadThreadRuntimeState()` | 仅绑定 deps |

**策略：** 完全直传的适配器方法 → 直接删除，调用者直接使用纯函数。仅绑定 deps 的方法 → 保留但确认调用者确实需要简化签名。

### P4: `thread_list_helpers.go` 泛型反模式

`appendThreadItems[T]` 和 `toThreadSnapshots[T]` 使用泛型签名但内部 type-switch 回退到具体类型函数，这比直接调具体函数更复杂且无编译期安全：

```go
func appendThreadItems[T any](...) []ThreadListItem {
    switch src := any(items).(type) {  // ← 运行时类型断言
    case []store.AgentCodexBinding: return appendBindingThreadItems(...)
    case []store.AgentStatus: return appendAgentStatusThreadItems(...)
    // ...
    }
}
```

**策略：** 删除这两个泛型包装，调用者直接使用 `appendBindingThreadItems` / `toRunnerThreadSnapshots` 等具体函数。

### P5: `runtime_context.go` nil-guard 重复

8 个方法都重复 `if a == nil || a.ctx == nil || a.ctx.XxxField == nil` 模式。可以提取通用 `depsOr` 帮助函数。

---

## 提议的变更

### 任务 1: 拆分 `turn_runtime.go` — Interrupt 逻辑

**文件:**
- 创建: `internal/apiserver/codexadapter/turn_interrupt.go`
- 修改: `internal/apiserver/codexadapter/turn_runtime.go`

**迁移内容 (~270行):**
- `NormalizeInterruptState`
- `IsInterruptNoActiveTurnError`
- `IsInterruptActiveState`
- `InterruptSettleMode`
- `ReadThreadRuntimeState` (纯函数 + 适配器方法)
- `readRuntimeStatus`
- `WaitInterruptOutcome` (纯函数 + 适配器方法)
- `TurnInterrupt`
- `TurnForceComplete`
- `sendInterruptCommand`
- `notifyTurnCompleted`

**步骤 1:** 提取上述函数到新文件，保持函数签名不变
**步骤 2:** 运行 `go build ./...` 确认编译  
**步骤 3:** 运行 `go test ./internal/apiserver/codexadapter/...` 确认测试通过  
**步骤 4:** 提交

---

### 任务 2: 拆分 `turn_runtime.go` — Resume 逻辑

**文件:**
- 创建: `internal/apiserver/codexadapter/turn_resume.go`
- 修改: `internal/apiserver/codexadapter/turn_runtime.go`

**迁移内容 (~140行):**
- `BuildResumeCandidates`
- `TryResumeCandidates`
- `IsHistoricalResumeCandidateError`
- `IsCodexProcessCrashError`
- `PreviewResumeCandidates`

**步骤:** 同任务 1

---

### 任务 3: 拆分 `turn_runtime.go` — Prompt/Skill/LSP 逻辑

**文件:**
- 创建: `internal/apiserver/codexadapter/turn_prompt.go`
- 修改: `internal/apiserver/codexadapter/turn_runtime.go`

**迁移内容 (~200行):**
- `passthroughSkillInputText`
- `BuildSelectedSkillPrompt` (纯函数 + 适配器方法)
- `ResolveLSPUsagePromptHint` (纯函数 + 适配器方法)
- `CollectDynamicToolNames`
- `PrependLSPAvailabilityWarning` (纯函数 + 适配器方法)
- `FuzzyFileSearch` (纯函数 + 适配器方法)

**步骤:** 同任务 1

> **任务 1-3 完成后，`turn_runtime.go` 从 991 行降至 ~380 行，仅保留 Turn 生命周期核心逻辑。**

---

### 任务 4: 重组 `turn_tracker_helpers.go` — 按功能子领域拆分

**文件:**
- 创建: `internal/apiserver/codexadapter/turn_tracker_stall.go`
- 创建: `internal/apiserver/codexadapter/turn_tracker_summary.go`
- 创建: `internal/apiserver/codexadapter/turn_tracker_payload.go`
- 修改: `internal/apiserver/codexadapter/turn_tracker_helpers.go` → **删除**
- 修改: `internal/apiserver/codexadapter/turn_tracker.go` — 吸收 State 初始化/默认值部分

**子领域划分:**

| 新文件 | 迁移内容 | 预估行数 |
|:-------|:---------|--------:|
| `turn_tracker_stall.go` | `TouchTrackedTurnLastEvent`, `RescheduleStallCheck`, `HandleStallGracePeriod`, `StartApprovalStallHeartbeat`, `ApprovalStallHeartbeatInterval`, `PeekTrackedTurnMeta`, `MarkTrackedTurnStallHint`, `ShouldLogTrackedTurnStallHint` + 对应 Adapter 方法 | ~300 |
| `turn_tracker_summary.go` | `RememberTrackedTurnSummary`, `LookupTrackedTurnSummary`, `PruneTrackedTurnSummaryCacheLocked`, `InjectTrackedTurnSummary`, `CaptureAndInjectTurnSummary`, `TrackedTurnSummaryCacheKey`, `TrackedTurnSummaryCacheEntry`, noop/empty fallbacks + 对应 Adapter 方法 | ~300 |
| `turn_tracker_payload.go` | `ExtractTrackedTurnID/Status/Reason`, `ExtractTrackedString`, `TrackedTurnTerminalFromEvent`, `ThreadStatusTerminalFromPayload`, `ExtractTrackedRetryable`, `MergeTrackedTurnCompletionPayload`, `TrackedTurnPayloadDiagKV` | ~250 |
| `turn_tracker.go` (扩展) | 吸收 `TurnTrackerState` + `EnsureTurnTrackerStateLocked` + 常量定义 | +70 |

**步骤 1:** 创建三个新文件，逐一迁移  
**步骤 2:** 将 State/常量迁移到 `turn_tracker.go`  
**步骤 3:** 删除 `turn_tracker_helpers.go`  
**步骤 4:** 运行测试确认  
**步骤 5:** 提交

---

### 任务 5: 删除 "完全直传" 适配器包装方法

**文件:**
- 修改: `internal/apiserver/codexadapter/turn_skill_match.go`
- 修改: 新文件的相关位置（任务 1-4 创建的文件）

**删除以下方法（仅做直传，无额外逻辑）：**

```go
// turn_skill_match.go — 完全直传，删除
func (a *Adapter) CollectAutoMatchedSkillMatches(...) { return CollectAutoMatchedSkillMatches(...) }

// turn_tracker 相关 — 完全直传，删除
func (a *Adapter) TrackedTurnSummaryFromPayload(p) { return TrackedTurnSummaryFromPayload(p) }
func (a *Adapter) ExtractTrackedString(p, keys) { return ExtractTrackedString(p, keys) }
func (a *Adapter) TrackedTurnTerminalFromEvent(e,m,p) { return TrackedTurnTerminalFromEvent(e,m,p) }

// turn_runtime.go — 完全直传，删除
func (a *Adapter) FuzzyFileSearch(q, r, f) { return FuzzyFileSearch(q, r, f) }
```

**步骤 1:** `grep -rn` 查找所有调用点  
**步骤 2:** 将调用点从 `a.XxxMethod(...)` 改为 `codexadapter.XxxFunc(...)`  
**步骤 3:** 删除适配器方法  
**步骤 4:** 运行 `go build ./...` + `go test ./...`  
**步骤 5:** 提交

---

### 任务 6: 删除 `thread_list_helpers.go` 泛型反模式

**文件:**
- 修改: `internal/apiserver/codexadapter/thread_list_helpers.go`
- 修改: 调用者文件（如有使用泛型版本的）

**删除:**
```go
func appendThreadItems[T any](...) { switch ... }  // 泛型 type-switch
func toThreadSnapshots[T any](...) { switch ... }   // 泛型 type-switch
```

**步骤 1:** 查找调用者，确认是否有代码使用泛型版本  
**步骤 2:** 替换为具体函数调用  
**步骤 3:** 删除泛型函数  
**步骤 4:** 运行测试  
**步骤 5:** 提交

---

### 任务 7: 精简 `runtime_context.go` nil-guard 重复

**文件:**
- 修改: `internal/apiserver/codexadapter/runtime_context.go`

**方案:** 提取 `deps()` 无检查方法简化访问模式：

```go
// 当前重复模式 (x8):
func (a *Adapter) cancelCodeRuns(agentID string) int {
    if a == nil || a.ctx == nil || a.ctx.CancelCodeRuns == nil {
        return 0
    }
    return a.ctx.CancelCodeRuns(agentID)
}

// 优化后 — normalizeDeps 已保证非 nil 字段，因此只需保护 a==nil
func (a *Adapter) cancelCodeRuns(agentID string) int {
    if a == nil { return 0 }
    return a.ctx.CancelCodeRuns(agentID)
}
```

> 由于 `normalizeDeps()` 在 `New()` 中已经将所有 nil 函数字段初始化为默认值，后续方法中的 `a.ctx.XxxField == nil` 检查 **全部是死代码（dead code）**。可以安全移除。

**步骤 1:** 确认 `normalizeDeps` 覆盖所有字段  
**步骤 2:** 简化 8 个方法的 nil-guard  
**步骤 3:** 运行测试  
**步骤 4:** 提交

---

## 预期效果

| 指标 | 重构前 | 重构后 |
|:-----|-------:|-------:|
| `turn_runtime.go` | 991 行 | ~380 行 |
| `turn_tracker_helpers.go` | 924 行 | **删除** |
| `turn_tracker.go` | 604 行 | ~670 行 |
| 新增文件 | — | 6 个（每个 < 350 行） |
| 直传包装方法 | ~6 个 | 0 |
| 泛型反模式函数 | 2 个 | 0 |
| nil-guard 重复 | 8 处 | 简化为 1 行模式 |
| 总行数变化 | 5585（非测试） | ~5500（净减 ~80 行） |

> 重点不是减行数，而是 **每个文件只承担一个清晰的职责**，单文件不超过 ~700 行。

## 风险评估

| 风险 | 等级 | 缓解措施 |
|:-----|:-----|:---------|
| 外部调用者依赖被删除的适配器方法 | 中 | 任务 5 先搜索所有调用点，仅删除确认无外部使用的 |
| 文件移动后 import 路径变化 | 低 | 同包内移动，无 import 变化 |
| 测试覆盖不足 | 低 | 现有 30 个测试函数覆盖纯函数；移动不改变签名 |

## 验证计划

### 自动化测试

每个任务完成后执行：

```bash
# 1. 编译检查
go build ./...

# 2. codexadapter 包测试
go test -v ./internal/apiserver/codexadapter/...

# 3. 全量测试（防止跨包影响）
go test ./...

# 4. vet 静态检查
go vet ./...
```

### 回归验证

- 所有现有测试（30 个 Test 函数）必须全部通过
- 无新增编译错误
- 无新增 vet 警告
