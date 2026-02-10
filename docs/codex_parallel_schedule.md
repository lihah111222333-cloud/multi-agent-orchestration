# Codex 并行执行计划（基于 `p0_p1_schedule.md` 适配版）

> 输入参考：`/Users/mima0000/.gemini/antigravity/brain/8762160e-962d-412a-8f06-9debe8ad7c6e/p0_p1_schedule.md`
> 目标：给当前项目一份“可立即执行”的并行文档，适配 **4 个子代理 + 1 个门禁职责** 的推进方式。

---

## 1. 现状评估（相对原计划的差异）

原计划方向正确，但和当前仓库状态有三点不匹配：

1. **认证/RBAC 不纳入当前周期**
   - 你已明确“测试期间不加认证”，因此原 Task 1.3（RBAC）从本周期移除。

2. **PostgreSQL 已落地，但迁移框架未成型**
   - 目前 `db/postgres.py` 仍以 `ensure_schema()` 直接建表为主，适合开发期，不利于版本化演进。

3. **并行执行需要“可拆分、低冲突”任务边界**
   - 原计划按阶段推进，但实际执行更适合按“泳道+波次”并行，减少相互等待。

---

## 2. 执行原则（本计划约束）

1. **不做向下兼容**：仅保留当前单一配置与单一路径。  
2. **每个子代理只改一个泳道**：减少交叉冲突。  
3. **门禁先于合并**：必须通过目标测试集再合并。  
4. **先可用再增强**：先打通最小闭环，再补体验。  
5. **接口先行**：先定义数据模型与 API，再做 UI。

---

## 3. 并行泳道（4 子代理）

### Lane A（子代理 A）— 数据层与迁移框架（P0）
**目标**：把 schema 变更从 `ensure_schema()` 迁移到版本化迁移。  
**核心产出**：
- 新增 `db/migrations/`（`001..00N`）
- 新增 `db/migrator.py`（`migrate_up()` / `migrate_down()` / `current_version()`）
- `run.py` 启动流程改为优先跑迁移
- 保留 `ensure_schema()` 仅作薄封装（内部调用迁移器）

**涉及文件（建议）**：
- `db/migrator.py`
- `db/migrations/*.sql`
- `db/postgres.py`
- `run.py`
- `tests/test_db_migrator.py`（新增）

---

### Lane B（子代理 B）— iTerm Agent 巡检与状态 API（P0）
**目标**：形成 Agent 运行状态闭环（采集→判定→落库→推送→展示）。  
**核心产出**：
- `agent_status` 表迁移脚本
- `agent_monitor.py`（巡检循环 + 状态分类）
- `/api/agent-status` 与 SSE 事件 `agent_status`
- Dashboard 顶部健康汇总 + Agent 状态芯片

**涉及文件（建议）**：
- `agent_monitor.py`（新增）
- `scripts/iterm_agent_io.py`
- `dashboard.py`
- `static/app.js`
- `static/style.css`
- `tests/test_agent_monitor.py`（新增）

---

### Lane C（子代理 C）— 任务追踪 + 提示词版本（P1）
**目标**：把“可观测链路”和“提示词回滚”做成可用版本。  
**核心产出**：
- `task_traces` 表 + 埋点 helper
- `run.py/master.py/gateway.py` 注入 trace span
- `prompt_versions` 表 + 保存与回滚接口
- Dashboard 增加 trace 列表/详情与 prompt 历史入口

**涉及文件（建议）**：
- `agent_ops_store.py`
- `master.py`
- `gateways/gateway.py`
- `run.py`
- `dashboard.py`
- `static/app.js`
- `tests/test_trace_store.py`（新增）
- `tests/test_prompt_versions.py`（新增）

---

### Lane D（子代理 D）— 插件化 Agent 骨架 + 门禁职责（P1）
**目标**：先做插件机制骨架，并承担统一门禁。  
**核心产出**：
- `plugins/` 目录规范与 loader
- `AgentSpec` 支持插件声明（非硬编码扩展）
- 至少 2 个示例插件（例如 `http_fetch`、`db_query`）
- 每轮汇总跑门禁并出报告（D 兼任 Gatekeeper）

**涉及文件（建议）**：
- `agents/factory.py`
- `agents/specs.py`
- `plugins/__init__.py`（新增）
- `plugins/http_fetch/plugin.py`（新增）
- `plugins/db_query/plugin.py`（新增）
- `tests/test_plugin_loader.py`（新增）

---

## 4. 波次计划（并行执行顺序）

## Wave 0（0.5 天）— 对齐与脚手架
- A：迁移器接口与目录脚手架
- B：巡检状态枚举与判定函数签名
- C：trace/prompt version 数据模型草案
- D：插件 manifest 协议草案 + 门禁清单
- 门禁：统一命名、迁移编号、测试命名规范

**验收**：接口冻结（函数签名 + 表结构 + API 字段）

---

## Wave 1（1-2 天）— P0 主闭环
- A：迁移器可执行，`001_initial.sql` 可落库
- B：巡检任务可跑，`/api/agent-status` 可读
- D（门禁）：验证 A/B 迁移、状态 API、单测稳定

**验收**：
- 新实例启动后可自动迁移到最新 schema
- Dashboard 能看到 Agent 状态（至少 unknown/running/error）

---

## Wave 2（2-3 天）— P1 可观测闭环
- C：trace + prompt version 后端落地
- B：SSE 状态事件与前端实时刷新联动
- D（门禁）：跨模块回归（dashboard/master/gateway）

**验收**：
- 可按 `trace_id` 查询完整链路
- 提示词可查看历史并回滚

---

## Wave 3（2-3 天）— 插件化最小可用
- D：插件 loader + 2 个真实插件
- C：trace 与插件调用打通（span 标注 plugin）
- A：迁移补齐插件相关表（如需）

**验收**：
- 配置中启用插件后可被网关调用
- 插件调用失败可追踪、有审计记录

---

## 5. 门禁标准（每轮必须满足）

1. `python3 -m unittest` 全通过。  
2. 新增模块必须有对应测试文件。  
3. 不引入兼容别名配置（如旧 env key fallback）。  
4. PR/提交描述必须含：变更点、风险点、回滚点。  
5. 若改动 `dashboard.py`，必须附前端回归清单。

建议最小门禁命令：
- `python3 -m unittest tests.test_db_postgres_pool tests.test_gateway tests.test_master`
- `python3 -m unittest tests.test_dashboard_config tests.test_dashboard_events tests.test_command_card_executor`
- `python3 -m unittest`（合并前全量）

---

## 6. 本周期排除项（明确不做）

1. RBAC/登录认证（按你的要求暂不纳入）。  
2. 外部告警平台深度接入（仅保留事件能力，不做完整通知中心）。  
3. 大规模前端重构（保持单页结构，先完成功能闭环）。

---

## 7. 风险与应对

1. **迁移切换风险**：`ensure_schema()` 与迁移器并存期可能重复建表。  
   - 应对：以 `schema_version` 为唯一真相，`ensure_schema()` 只代理迁移器。  
2. **巡检误判风险**：iTerm 输出短期噪音导致状态跳变。  
   - 应对：加入最短稳定窗口（如连续 N 次才切状态）。  
3. **并行冲突风险**：多泳道同时改 `dashboard.py`。  
   - 应对：B 负责状态页块，C 负责 trace/prompt 页块，按页面区块拆分并约定 merge 顺序。  
4. **插件安全风险**：插件执行能力过强。  
   - 应对：首批插件仅白名单函数，不开放任意 shell。

---

## 8. 建议执行口径（给协作群）

`[codex-parallel] mode=4-lanes wave=0 start=ready scope=P0-first no-backward-compat auth=disabled`