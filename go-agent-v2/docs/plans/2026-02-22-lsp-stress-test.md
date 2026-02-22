# LSP 压测计划（V5，19 工具）

> 日期：2026-02-22
> 适用仓库：`go-agent-v2`
> 当前基线：覆盖 19 个 LSP 工具，替代 V4 的 14 能力矩阵

## 1. 目标

- 在真实三语言语料（Go / JS / Rust）上验证 19 个 LSP 工具的正确性、稳定性和可恢复性。
- 把测试拆成可落地阶段（`P0-A` 到 `P3`），每阶段可独立验收。
- 提供 `Quick Gate`（开发快速回归）和 `Full Gate`（发布前全量门禁）。

## 2. 测试语料与目录约定

- 语料根目录：`/Users/mima0000/Desktop/wj/e2e测试`
- Go：`go/`（约 552 文件）
- JS/TS：`js/`（约 131 文件）
- Rust：`rust/`（约 75 文件）

### 2.1 App 目录自动发现（新增）

测试代码必须自动判断 app/project 目录，不允许硬编码单一路径。解析顺序如下：

1. 环境变量显式指定：`LSP_STRESS_APP_DIR`（最高优先级）
2. 语料推断：`<corpus>/js`、`<corpus>/go`、`<corpus>/rust`
3. 仓库内候选：
   - `cmd/agent-terminal/frontend/vue-app`
   - `cmd/agent-terminal/frontend`
4. 校验规则：目录存在，且命中至少一个标记文件（`package.json` / `go.mod` / `Cargo.toml` / `tsconfig.json`）

如果全部失败：测试直接 `t.Fatalf`，输出所有已探测路径与失败原因。

## 3. 19 工具覆盖矩阵

| # | 工具名 | 关键协议/能力 | 最小验收条件 |
|---|---|---|---|
| 1 | `lsp_open_file` | `textDocument/didOpen` | 可打开文件且无 panic |
| 2 | `lsp_document_symbol` | `textDocument/documentSymbol` | 返回符号列表（可空但不报错） |
| 3 | `lsp_hover` | `textDocument/hover` | 返回 hover 或明确空结果 |
| 4 | `lsp_diagnostics` | 诊断聚合 | 收到并读取诊断结果 |
| 5 | `lsp_definition` | `textDocument/definition` | 至少 1 个可解析定位（对可定义符号） |
| 6 | `lsp_references` | `textDocument/references` | 返回引用列表（可空但可解释） |
| 7 | `lsp_rename` | `textDocument/rename` | 返回 edits 且可应用/验证 |
| 8 | `lsp_completion` | `textDocument/completion` | completion 请求成功并有稳定结构 |
| 9 | `lsp_did_change` | `textDocument/didChange` | 变更后语义查询可见 |
| 10 | `lsp_workspace_symbol` | `workspace/symbol` | 跨文件关键字检索可返回结果 |
| 11 | `lsp_implementation` | `textDocument/implementation` | 对接口/trait/抽象点可返回实现 |
| 12 | `lsp_type_definition` | `textDocument/typeDefinition` | 可定位类型定义 |
| 13 | `lsp_call_hierarchy` | prepare/incoming/outgoing | 可得到调用方或被调方链路 |
| 14 | `lsp_type_hierarchy` | prepare/supertypes/subtypes | 可得到父子类型关系 |
| 15 | `lsp_code_action` | `textDocument/codeAction` | 返回 action 列表（可空但不报错） |
| 16 | `lsp_signature_help` | `textDocument/signatureHelp` | 调用点可返回签名提示 |
| 17 | `lsp_format_document` | `textDocument/formatting` | 格式化请求成功且文本稳定 |
| 18 | `lsp_semantic_tokens` | `textDocument/semanticTokens/full` | tokens 返回结构合法 |
| 19 | `lsp_folding_range` | `textDocument/foldingRange` | 折叠区间返回结构合法 |

## 4. 分阶段执行计划

### P0-A：基础设施与探测（阻塞阶段）

目标：把测试框架和目录探测打通。

- 任务 A1：实现 `discoverAppDirs()`，按 2.1 的顺序探测并输出日志。
- 任务 A2：统一 `open + bootstrap` 辅助函数，封装重试、超时和错误分级。
- 任务 A3：输出统一结果结构（JSON），包含：工具名、耗时、是否成功、错误码、错误文本。

验收：
- 三语言目录探测成功；失败时错误信息完整。
- 可跑通单文件 `open -> documentSymbol` 的 smoke。

### P0-B：Core 9 工具基线

目标：先把最常用 9 工具打成稳定基线。

- 覆盖工具：1-9
- 每个语言至少 20 个文件样本（可配置）
- 单个工具失败不阻断整轮，最终统一汇总失败明细

验收：
- 工具 1-9 全部至少成功 1 次（每语言）
- `rename` 与 `didChange` 必须有“变更前后对比”断言
- 生成 `core9-summary.json`

### P1：跨文件与层级能力

目标：补齐语义跳转和层级能力。

- 覆盖工具：10-14
- 重点场景：
  - workspace symbol 全局检索
  - implementation/typeDefinition 在 Go + Rust 的可达性
  - call hierarchy 与 type hierarchy 的链路完整性

验收：
- 工具 10-14 在至少两种语言有非空结果
- 对空结果场景必须记录“为何为空”（不支持、无匹配、语料不足）
- 生成 `xref-hierarchy-summary.json`

### P2：编辑动作与展示能力

目标：补齐编辑辅助与展示工具。

- 覆盖工具：15-19
- 重点场景：
  - code action：诊断触发时返回可用 action
  - signature help：函数调用点返回签名
  - formatting：幂等（连续两次格式化 diff 为空）
  - semantic tokens/folding range：返回结构合法且可解析

验收：
- 工具 15-19 每个至少 1 个正样例
- formatting 幂等断言通过
- 生成 `actions-semantic-summary.json`

### P3：并发、稳定性、恢复

目标：验证全链路在压力下可用。

- 并发测试：N worker 并发混合请求（open/hover/definition/references/completion）
- 恢复测试：language server 重启后请求恢复
- soak：最少 20 轮（可通过环境变量调整）
- race：`go test -race`（针对 `internal/lsp`）

验收：
- 无 panic / 无死锁
- 失败率低于阈值（见第 5 节）
- 生成 `stability-summary.json`

## 5. 统一验收阈值

- 成功率阈值：
  - Core 9（1-9）：`>= 98%`
  - Extended 10（10-19）：`>= 95%`
- 延迟阈值（p95）：
  - `hover/definition/references/completion <= 1500ms`
  - `workspaceSymbol <= 2500ms`
  - `semanticTokens <= 3000ms`
- 稳定性阈值：
  - soak 20 轮累计失败率 `< 2%`
  - race 检查无 data race

> 若低于阈值，不允许标记“全量通过”，必须附带失败清单和修复计划。

## 6. 测试命令（建议）

### 6.1 Quick Gate（开发回归）

```bash
cd /Users/mima0000/Desktop/wj/multi-agent-orchestration/go-agent-v2

go test ./internal/lsp -run 'TestLSP_Stress_P0A|TestLSP_Stress_P0B_Core9|TestLSP_Stress_P1_Smoke|TestLSP_Stress_P2_Smoke' -count=1 -timeout 20m
```

### 6.2 Full Gate（发布前）

```bash
cd /Users/mima0000/Desktop/wj/multi-agent-orchestration/go-agent-v2

go test -tags lspstress ./internal/lsp -run 'TestLSP_Stress_' -count=1 -timeout 60m

go test -race ./internal/lsp -run 'TestLSP_Stress_' -count=1 -timeout 60m
```

### 6.3 产物输出

- 建议目录：`test-results/lsp-stress/2026-02-22/`
- 至少输出：
  - `core9-summary.json`
  - `xref-hierarchy-summary.json`
  - `actions-semantic-summary.json`
  - `stability-summary.json`
  - `final-report.md`

## 7. 失败分级与处理

- `P0` 阻塞：目录探测失败、open 失败率高、rename/didChange 不一致
- `P1` 重要：definition/references/workspaceSymbol 大面积失败
- `P2` 次要：单工具偶发超时或语义 token 不稳定

处理策略：
1. 先定位是 server 能力缺失还是 client 封装问题。
2. 对能力缺失场景加 capability gate，不计为功能回归。
3. 对超时场景记录重试次数与最终耗时，避免只看一次结果。

## 8. 执行顺序（给 Agent）

1. 先完成 `P0-A`。
2. 再执行 `P0-B` 并产出 `core9-summary.json`。
3. 按 `P1 -> P2 -> P3` 依次推进。
4. 每阶段结束必须更新 `final-report.md` 的“阶段结论 + 未解决问题 + 下一阶段入口条件”。

## 9. 当前完成定义（DoD）

满足以下条件才可标记“计划完成”：

- 19 个工具均有执行记录。
- Core 9 与 Extended 10 达到第 5 节阈值。
- Quick Gate 与 Full Gate 均可复现通过。
- 报告和 JSON 产物完整，且可追溯到具体失败样本。
