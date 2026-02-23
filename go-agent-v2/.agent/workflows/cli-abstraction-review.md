# cli-abstraction.md 深度审查报告 (第二轮)

> **审查时间**: 2026-02-23 · **版本**: v2 (311 行，新增 Runbook)  
> **方法**: 逐行交叉验证源码 · **范围**: `internal/codex/`, `runner/`, `apiserver/`

---

## 一、未修复的第一轮问题（3 项）

### 🔴 R1-1. `EventHandler` 源文件标注仍有误

§1.1 写「从 `events.go` 搬出 `Event` struct + `EventHandler` type」，但 `EventHandler` 实际定义在 [client.go:38-39](file:///Users/mima0000/Desktop/wj/multi-agent-orchestration/go-agent-v2/internal/codex/client.go#L38-L39)，不在 `events.go`。

```go
// client.go:38-39
type EventHandler func(event Event)
```

**风险**: 执行者若只处理 `events.go`，Phase 1 编译就会失败。

**修复**: 在 §1.1 明确标注 `EventHandler` 来源为 `client.go`，或统一在 §1.1 列出「迁移来源文件清单」。

---

### 🔴 R1-2. `DynamicToolCallResponse` / `DynamicToolContentItem` 不应入 `agent`

§边界定义 Line 30 仍将这两个类型列入 `agent`。全项目 `rg` 确认：

```
rg 'codex\.(DynamicToolCallResponse|DynamicToolContentItem)' internal/ → 0 hits（仅 codex 包内部使用）
```

这两个类型的 JSON tag (`contentItems`, `inputText`) 是 **Codex Rust 协议专属**格式。其他 CLI 的工具响应格式必然不同。

**修复**: 从 §边界定义 的 `agent` 列表中移除这两个类型，保留在 `codex`。

---

### 🟡 R1-3. `agent.Client` 接口耦合未标注技术债

§1.2 说「与 `CodexClient` 方法签名完全一致，仅改名」。`SpawnAndConnect` 的参数假设了「spawn 进程 → 端口监听 → 创建 thread」，这是 Codex 独有流程。

**建议**: 在 §非目标 中补充：

> `agent.Client` 接口签名在本阶段 1:1 复制 `CodexClient`，接口拆分（Spawner / Communicator）留到多 CLI 适配阶段。

---

## 二、新发现的深层问题（5 项）

### 🔴 D-1. `runner/manager.go` handleEvent 存在常量双路径

[manager.go:309-348](file:///Users/mima0000/Desktop/wj/multi-agent-orchestration/go-agent-v2/internal/runner/manager.go#L309-L348) 的状态转换逻辑有**两条路径**：

```go
// 路径 A: uistate.NormalizeEvent → switch normalized.UIType (L314-343)
normalized := uistate.NormalizeEvent(event.Type, "", event.Data)
switch normalized.UIType { ... }

// 路径 B: 直接用 codex.Event* 常量 (L336-341, L347)
case codex.EventCollabAgentSpawnBegin, codex.EventCollabAgentInteractionBegin, ...
if event.Type == codex.EventShutdownComplete { ... }
```

工作流 §2.1 说「所有 `codex.EventXxx` 常量引用迁移为 `agent.EventXxx`」，但 **路径 A 完全不用常量**（基于 `uistate.UIType` 枚举），只有路径 B 用了 7 个 `codex.Event*` 常量。

**风险**: 如果只做 `codex.Event*` → `agent.Event*` 文本替换，路径 A 的 `uistate.NormalizeEvent` 内部仍然使用 `event.Type` 字符串匹配（跟 `codex` 无类型依赖），但 **行为上 `uistate` 内藏了一份完整的事件类型字符串表**。后续新增 CLI 事件时需要同步更新 `uistate`。

**建议**: 在工作流中标注 `uistate` 包作为 **隐式依赖**。建议阶段性产出：

> Phase 2 完成后，runner 的 `codex.*` 编译依赖清零，但 `uistate.NormalizeEvent` 对事件字符串的隐式依赖需在多 CLI 阶段处理。

---

### 🔴 D-2. `server_payload.go` AgentEventHandler 是 Phase 3 最复杂迁移点

[server_payload.go:494-585](file:///Users/mima0000/Desktop/wj/multi-agent-orchestration/go-agent-v2/internal/apiserver/server_payload.go#L494-L585) 是 **Phase 3 的核心难点**，但工作流中没有特别标注：

```go
// L498: 返回类型是 codex.EventHandler
func (s *Server) AgentEventHandler(agentID string) codex.EventHandler {
    return func(event codex.Event) {     // L499: 参数类型
        ...
        codex.EventStreamError           // L534: 常量
        codex.EventDynamicToolCall       // L577: 常量
    }
}
```

此函数涉及 4 个 `codex.` 引用（1 返回类型 + 1 参数类型 + 2 常量），且**闭包内部使用 `event.Data`、`event.Type`、`event.DenyFunc`、`event.RespondFunc`** 四个字段 — 这些字段如果 `Event` 结构体从 `codex` 迁到 `agent`，JSON 序列化行为不变，但闭包签名需要跟着改。

**建议**: 在 Phase 3 文件列表中将 `server_payload.go` 标记为 **⚠️ high-risk**。

---

### 🟡 D-3. `server_approval.go` 函数签名中的 `codex.Event`

[server_approval.go:56](file:///Users/mima0000/Desktop/wj/multi-agent-orchestration/go-agent-v2/internal/apiserver/server_approval.go#L56):

```go
func (s *Server) handleApprovalRequest(agentID, method string, payload map[string]any, event codex.Event) {
```

此函数签名中的 `codex.Event` 需要迁移。但它还使用了 `event.DenyFunc` (L150-151, L161-162)，这是 `Event` struct 的**闭包字段**。

Phase 1 alias `codex.Event = agent.Event` 后，此处编译不会断，但工作流的验证正则（§最终验证 L302）没有检查 `codex.Event` **作为函数参数类型**的场景 — `codex.Event` 在 alias 存在时会被 `rg` 跳过（因为源码中已经写成 `agent.Event`）。

**结论**: 这不是 bug，alias 策略已覆盖。确认无问题。

---

### 🟡 D-4. apiserver 测试文件遗漏未在迁移范围内

Phase 3 §Step 3 的验证正则显式排除 `*_test.go`：

```bash
--glob '!**/*_test.go'
```

但以下 5 个测试文件也引用 `codex.DynamicTool` / `codex.Event*`：

| 文件 | 引用数量 |
|---|---|
| `server_dynamic_tools_xref_test.go` | 3 |
| `server_dynamic_tools_ext_registry_test.go` | 2 |
| `methods_lsp_prompt_test.go` | 3 |
| `server_helpers_test.go` | 3 |
| `server_approval_test.go` | 未统计 |

当 Phase 1 的 alias 被清除（未来彻底移除兼容层时），这些测试会编译失败。

**建议**: Phase 3 即同步迁移测试文件中的 `codex.` 引用，或在工作流中显式标注「Phase 3 测试文件依赖 Phase 1 alias，清除 alias 时需二次迁移」。

---

### 🟡 D-5. `codex/client.go` 内的 `EventHandler` 类型由两个实现使用

`EventHandler` 在 `codex` 包内被两个实现使用：

| 实现 | 文件 | 行 |
|---|---|---|
| `Client.SetEventHandler` | [client.go:76](file:///Users/mima0000/Desktop/wj/multi-agent-orchestration/go-agent-v2/internal/codex/client.go#L76) | `handler EventHandler` 字段 |
| `AppServerClient.SetEventHandler` | [client_appserver.go:192](file:///Users/mima0000/Desktop/wj/multi-agent-orchestration/go-agent-v2/internal/codex/client_appserver.go#L192) | `handler EventHandler` 字段 |

Phase 1 alias `type EventHandler = agent.EventHandler` 后，两个实现的字段类型会自动跟着改变（alias 是透明的），不需要修改实现文件。

**结论**: alias 策略覆盖正确，**无需额外改动**。✅

---

## 三、Runbook 步骤审查（3 项）

### 🟡 RB-1. Step 1 的 `gofmt -w` 对尚未创建的文件生效

```bash
gofmt -w internal/agent/types.go internal/agent/client.go internal/codex/events.go internal/codex/interface.go
```

如果执行者先运行 `gofmt` 再创建文件，会报 `no such file`。建议改为在创建文件后运行，或拆为两步。

---

### 🟡 RB-2. Step 2 的 rg 验证不够严格

```bash
rg -n "func \\(m \\*AgentManager\\) SetClientFactories\\(" internal/runner/manager.go
```

此检查只验证方法存在，但不验证签名中使用的是 `agent.ClientFactory` 而非 `codex.CodexClient`。

**建议**: 补充签名验证：

```bash
rg -n "func \\(m \\*AgentManager\\) SetClientFactories\\(.*agent\\.ClientFactory" internal/runner/manager.go
```

---

### ✅ RB-3. Step 4 全量验收脚本质量好

`violations` + `test -z` + `runner_import_count` + `agent_codex_imports` 四段检查覆盖了关键不变量。无问题。

---

## 四、总结与建议优先级

| 级别 | ID | 摘要 | 建议 |
|---|---|---|---|
| 🔴 必须修 | R1-1 | `EventHandler` 源标错 | 标注来自 `client.go` |
| 🔴 必须修 | R1-2 | Response 类型不应入 `agent` | 从边界定义移除 |
| 🔴 必须修 | D-1 | `uistate` 隐式事件依赖 | 标注为已知技术债 |
| 🔴 需关注 | D-2 | `AgentEventHandler` 高风险 | 标记为 Phase 3 重点 |
| 🟡 建议 | R1-3 | 接口耦合技术债 | 补充非目标说明 |
| 🟡 建议 | D-4 | 测试文件未迁移 | 同步迁移或标注 |
| 🟡 建议 | RB-1 | gofmt 时序 | 调整步骤顺序 |
| 🟡 建议 | RB-2 | rg 签名验证不足 | 补充正则 |
| ✅ 无问题 | D-3 | approval 函数签名 | alias 覆盖 |
| ✅ 无问题 | D-5 | EventHandler 两实现 | alias 覆盖 |
| ✅ 无问题 | RB-3 | Step 4 验收脚本 | 质量好 |

---

## 五、设计整体评价

**新增 Runbook 的价值很大** — 将文档从「描述型计划」升级为「可执行运维手册」，每步有 bash 验证、通过标准明确。相比 v1 的 206 行纯描述，v2 的可执行性显著提升。

**核心风险点**:
1. Phase 3 的 `server_payload.go:AgentEventHandler` 是影响面最大的单点，涉及事件分发、审批、工具调用三条链路
2. `uistate` 包作为隐式事件路由器，未来多 CLI 适配时需要同步扩展
