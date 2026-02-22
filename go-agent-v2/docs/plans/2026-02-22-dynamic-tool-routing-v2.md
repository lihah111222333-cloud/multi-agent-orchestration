# Dynamic Tool Routing V2 落地计划

> 目标: 在当前 `30+` 动态工具基础上，把“一次性全注入”升级为“核心工具常驻 + 搜索发现 + 显式启用 + 策略门禁 + 自适应重排”，在不牺牲安全性的前提下降低上下文成本并提升工具调用命中率。

---

## 1. 现状基线（与代码对齐）

当前实现已经具备完整动态工具链路，但属于 **全量注入模型**:

1. `thread/start` 会构建并注入所有动态工具（LSP + 编排 + 资源 + code_run）  
   见 `internal/apiserver/methods_thread.go` 的 `threadStartTyped` 调用 `buildAllDynamicTools()`。
2. `buildAllDynamicTools()` 按模块拼接完整工具列表  
   见 `internal/apiserver/orchestration_tools.go`。
3. 运行时调用通过 `handleDynamicToolCall` 分发到 `s.dynTools`  
   见 `internal/apiserver/server_dynamic_tools.go`。
4. 目前 `registerDynamicTools()` 是“统一注册 + 统一可调用”，尚无“启用态/权限态”区分  
   见 `internal/apiserver/server_dynamic_tools.go`。

问题不是“功能不可用”，而是“规模上来后决策与成本退化”:

1. 工具数继续增长会拉高提示与选择噪声。
2. 隐藏 schema 但允许直接调用在部分协议下不可依赖。
3. 缺少“按会话启用”与“策略门禁”导致高风险工具隔离不足。
4. 缺少路由反馈闭环，`AlwaysLoad` 难以数据驱动调整。

---

## 2. V2 设计目标

## 2.1 本期必须达成

1. 引入 `tool_search` 与 `tool_enable`，建立 **发现 -> 启用 -> 调用** 闭环。
2. 增加 `policy gate`，每次调用前统一校验风险与启用态。
3. 将工具注入分层为:
   - 常驻核心（Always Injected）
   - 可发现但默认不可直接调用（Discoverable）
4. 增加基础观测:
   - 路由命中率
   - 二次搜索率
   - 工具调用失败率
   - 启用后未使用率

## 2.2 非目标（本期不做）

1. 不做复杂多模型编排平台（先保持单路由器）。
2. 不做向量数据库强依赖（先用关键词 + 规则 + 可选小模型重排）。
3. 不自动放开高风险工具权限（必须显式启用且过策略门禁）。

---

## 3. V2 总体架构

## 3.1 组件

1. `ToolRegistry`
   - 工具元数据中心（schema、handler、分类、风险、关键词、是否常驻）。
2. `ToolRouter`
   - 接收自然语言需求，输出候选工具 `topK`（规则优先，可选小模型重排）。
3. `ToolEnableState`
   - 维护会话级“已启用工具集”（支持 TTL turn）。
4. `ToolPolicyGate`
   - 每次执行前检查:
     - 是否已启用
     - 风险级别是否允许
     - 前置条件是否满足
5. `ToolTelemetry`
   - 记录路由、启用、调用、失败与回退事件。

## 3.2 执行链路

1. `thread/start` 仅注入核心工具 + 元工具（`tool_search`, `tool_enable`）。
2. Agent 需要额外能力时调用 `tool_search`。
3. Agent 选择候选后调用 `tool_enable`（可带 `ttl_turns`）。
4. 执行具体工具调用时由 `ToolPolicyGate` 做统一校验。
5. 校验通过才进入原 handler，失败返回结构化错误与建议动作。

---

## 4. 工具分层与风险模型

## 4.1 推荐常驻工具（最小核心）

1. `lsp_open_file`
2. `lsp_document_symbol`
3. `lsp_hover`
4. `lsp_definition`
5. `lsp_references`
6. `lsp_diagnostics`
7. `tool_search`
8. `tool_enable`

> 说明: `lsp_open_file` 必须常驻，否则会与“先 open 再分析”的链路约束冲突。

## 4.2 风险分级（示例）

1. `low`: 只读查询（如大部分 LSP 查询工具）。
2. `medium`: 产生建议但不直接落盘（如 rename 生成 edits）。
3. `high`: 可能执行命令、写文件、变更系统状态（如 `code_run` 的 `project_cmd`）。

策略:

1. `low` 可在启用后直接调用。
2. `medium/high` 必须 `tool_enable` + `policy gate` 双重通过。
3. `high` 需保留现有审批链（不因已启用而跳过审批）。

---

## 5. 新增工具协议（外部可见）

## 5.1 `tool_search`

用途: 根据任务描述返回候选工具与调用提示。

入参:

```json
{
  "query": "分析某函数调用链",
  "top_k": 5
}
```

出参:

```json
{
  "query": "分析某函数调用链",
  "matches": [
    {
      "name": "lsp_call_hierarchy",
      "category": "analysis",
      "risk": "low",
      "description": "分析调用者与被调用者",
      "enabled": false,
      "why_matched": ["keyword: 调用链", "alias: caller/callee"]
    }
  ],
  "fallback": {
    "suggestion": "如果目标符号未知，先调用 lsp_document_symbol"
  }
}
```

## 5.2 `tool_enable`

用途: 显式启用候选工具，进入当前会话可调用集合。

入参:

```json
{
  "names": ["lsp_call_hierarchy"],
  "ttl_turns": 3
}
```

出参:

```json
{
  "enabled": [
    {"name": "lsp_call_hierarchy", "expires_after_turns": 3}
  ],
  "rejected": []
}
```

---

## 6. 小模型路由器（可选增强）

## 6.1 定位

小模型只做“候选排序器”，不是最终执行器:

1. 输入: 用户意图 + 上下文摘要 + 工具候选集。
2. 输出: `intent`, `top_k_tools`, `confidence`, `need_clarification`。
3. 最终执行仍由 `tool_enable + policy gate` 决定。

## 6.2 路由回退

1. `confidence < 阈值` 时回退关键词路由。
2. 候选为空时给出追问建议，不直接猜测调用。
3. 小模型异常/超时时不阻断流程，自动降级到规则路由。

---

## 7. 基于使用习惯的自适应优化

## 7.1 可自动优化项

1. 工具排序权重（个人/项目/全局三层）。
2. 同义词与习惯表达映射（例如“看调用链” -> `lsp_call_hierarchy`）。
3. `AlwaysLoad` 候选建议（需人工或策略阈值批准后生效）。

## 7.2 不自动化项

1. 不自动放开高风险工具权限。
2. 不自动修改审批策略与风险等级。
3. 不自动变更工具 schema。

## 7.3 学习信号（可观测指标）

1. `route_top1_hit`: top1 是否最终被调用并成功。
2. `route_top3_hit`: top3 是否命中。
3. `search_retry_count`: 同一任务是否重复搜索。
4. `enable_unused_rate`: 启用后未调用比例。
5. `tool_call_error_rate`: 工具错误率（按工具维度）。

---

## 8. 代码改造方案（对应当前目录）

## 8.1 新增文件

1. `internal/apiserver/tool_registry.go`
   - `ToolEntry`、`ToolRegistry`、元数据索引。
2. `internal/apiserver/tool_router.go`
   - 关键词召回 + 可选小模型重排。
3. `internal/apiserver/tool_policy.go`
   - 统一调用前门禁逻辑。
4. `internal/apiserver/tool_enable_state.go`
   - 会话启用态存储（内存版 + TTL）。
5. `internal/apiserver/tool_meta_tools.go`
   - `tool_search`、`tool_enable` handler 与 schema。

## 8.2 修改文件

1. `internal/apiserver/server.go`
   - `Server` 增加 registry/router/policy/enableState/telemetry 字段并初始化。
2. `internal/apiserver/server_dynamic_tools.go`
   - `registerDynamicTools()` 改为“注册所有 handler + 注册元工具”。
   - `handleDynamicToolCall()` 增加 policy gate 检查。
3. `internal/apiserver/orchestration_tools.go`
   - `buildAllDynamicTools()` 改为从 registry 获取 `AlwaysLoad` + 元工具。
4. `internal/apiserver/methods_thread.go`
   - `threadStartTyped()` 保持注入入口，但使用新分层工具列表。

## 8.3 测试文件

1. `internal/apiserver/tool_registry_test.go`
2. `internal/apiserver/tool_router_test.go`
3. `internal/apiserver/tool_policy_test.go`
4. `internal/apiserver/tool_enable_state_test.go`
5. `internal/apiserver/server_dynamic_tools_v2_test.go`

---

## 9. 分阶段实施计划

| 阶段 | 目标 | 退出条件 |
|---|---|---|
| P0 | 引入 registry，不改行为 | 全量工具注入与现状一致，测试通过 |
| P1 | 上线 `tool_search`（只读） | 可返回稳定 topK，且无调用回归 |
| P2 | 上线 `tool_enable` + policy gate | 未启用工具默认拒绝，高风险路径可控 |
| P3 | 接入小模型重排（可降级） | 低置信度回退稳定，P95 时延达标 |
| P4 | 自适应排序灰度 | top1/top3 命中率提升，误调用率不升 |

---

## 10. 验收标准（必须量化）

1. 功能:
   - 未启用工具调用返回结构化拒绝结果。
   - 启用后工具可调用，TTL 到期后自动失效。
2. 安全:
   - `high` 风险工具即使启用，仍需审批链通过。
3. 性能:
   - `tool_search` P95 < 120ms（纯规则路由）。
4. 质量:
   - 路由 `top3` 命中率 >= 95%（基于离线样本）。
5. 回滚:
   - 开关可退回“全量注入模式”（兼容现网）。

---

## 11. 配置与开关建议

新增配置项（建议）:

1. `DYN_TOOL_ROUTING_MODE=legacy|v2`
2. `DYN_TOOL_ROUTER_MODEL=`（为空表示禁用小模型，仅规则路由）
3. `DYN_TOOL_ROUTER_PROVIDER=openai_compatible|...`
4. `DYN_TOOL_ROUTER_BASE_URL=...`（可指向本地网关）
5. `DYN_TOOL_ROUTER_API_KEY=...`（可为空，视本地网关策略）
6. `DYN_TOOL_ROUTER_TIMEOUT_SEC=8`
7. `DYN_TOOL_ENABLE_DEFAULT_TTL_TURNS=3`
8. `DYN_TOOL_ROUTER_CONFIDENCE_THRESHOLD=0.65`
9. `DYN_TOOL_POLICY_STRICT=true`

本地小模型示例（Ollama OpenAI 兼容口）:

1. `DYN_TOOL_ROUTER_MODEL=qwen2.5:7b-instruct`
2. `DYN_TOOL_ROUTER_PROVIDER=openai_compatible`
3. `DYN_TOOL_ROUTER_BASE_URL=http://127.0.0.1:11434/v1`

---

## 12. 风险与回退预案

1. 风险: 路由误判导致多一步搜索  
   预案: 低置信度回退 + 追问模板。
2. 风险: 启用态与线程生命周期不一致  
   预案: 启用态绑定 `threadID`，线程结束即清理。
3. 风险: 小模型不可用  
   预案: 全量降级规则路由，不影响工具可用性。
4. 风险: 策略误拒导致任务失败  
   预案: 返回明确拒绝原因与下一步动作（先 enable/补参数/请求审批）。

---

## 13. 里程碑建议（两周版）

1. 第 1-2 天: P0（registry 抽象，行为等价）。
2. 第 3-4 天: P1（`tool_search` + 测试）。
3. 第 5-7 天: P2（`tool_enable` + gate + 回归）。
4. 第 8-10 天: P3（小模型可选接入 + 降级）。
5. 第 11-14 天: P4（灰度与指标评估）。

---

## 14. 结论

V2 的关键不是“再加一个搜索工具”，而是把调用链从“可见即可调”改成“可发现、可启用、可审计、可回退”。  
对你们当前 `30+` 工具规模，优先级应为:

1. `tool_enable + policy gate`（先把安全与协议闭环补齐）。
2. 再做小模型路由重排与使用习惯优化（提升命中率与成本效率）。
