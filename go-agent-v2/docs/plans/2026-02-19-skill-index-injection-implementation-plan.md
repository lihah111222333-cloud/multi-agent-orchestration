# Skill 注入索引化详细实施计划（V1）

> 基于 `docs/skill设计.md` 的落地计划。  
> 目标：把当前“命中后整份 `SKILL.md` 注入”改为“默认摘要注入 + 按需细节加载”，并支持模型重解析与人工审核版本管理。

---

## 0. 范围与约束

## 0.1 目标（本期必须达成）

1. 运行时默认注入 `SkillDigest`，不再默认注入全文。
2. 索引条目具备 `title + start_line + end_line + summary + source_path`。
3. 支持“模型重解析 → 人工审核 → 发布生效 → 回滚”闭环。
4. 任何失败可降级回 legacy 全量注入。

## 0.2 非目标（本期不做）

1. 不做向量库/embedding 检索。
2. 不引入外部索引服务。
3. 不一次改造全部 skill（先灰度高频 skill）。

## 0.3 已确认产品决策

1. **策略 A**：重解析不覆盖已审核，生成候选变更，人工确认后替换。
2. 支持版本管理（发布、历史、回滚）。
3. 前端工作台以人工整理效率为优先，必须双栏清晰可审。

---

## 1. 里程碑与节奏

| 里程碑 | 目标 | 预计输出 | 退出条件 |
|---|---|---|---|
| M0 | 基线与开关 | 指标埋点 + feature flags | 可观测 legacy token 基线 |
| M1 | 数据层落地 | 新 migration + store CRUD | 版本/条目可持久化 |
| M2 | 解析与版本流 | parse/review/publish/rollback 服务 | 版本流可跑通 |
| M3 | 注入主链路改造 | digest 注入 + fallback | turn/start 与 turn/steer 正常 |
| M4 | 审核工作台 | API + UI 交互闭环 | 可人工审核发布 |
| M5 | 验证与灰度 | 测试、回滚预案、灰度报告 | token 降幅达标且稳定 |

---

## 2. 分阶段任务拆解（可执行）

## 阶段 A：基线与兼容开关（M0）

### A1. 加开关与模式路由

- 新增配置：
  - `SKILL_INJECTION_MODE=legacy|digest|hybrid`
  - `SKILL_DIGEST_MAX_TOKENS`
  - `SKILL_TOTAL_MAX_TOKENS`
  - `SKILL_INDEX_REQUIRE_REVIEW`
- 预期改动：
  - `internal/config/*`（若已有统一读取入口）
  - `internal/apiserver/server.go`（初始化配置注入）
  - `internal/apiserver/methods.go`（注入分支）

### A2. 基线指标

- 记录每次 turn 的：
  - 注入模式（legacy/digest/fallback）
  - 注入 skill 数
  - 注入文本字符数（或估算 token）
  - 截断次数
- 预期改动：
  - `internal/apiserver/methods.go`
  - `internal/apiserver/notifications.go`（若需上报 UI）
  - `internal/uistate/*`（若需展示）

### A3. 退出条件

1. legacy 模式零行为变化。
2. 开关可热切（至少进程重启级切换）。
3. 基线日志可汇总“每 skill 注入成本”。

---

## 阶段 B：数据层（M1）

## B1. 数据库迁移

新增迁移（建议）：`migrations/0013_skill_index.sql`

建议表结构：

1. `skill_index_versions`
   - `id`, `version_id`, `skill_name`, `status`, `base_version_id`
   - `created_by_type`, `created_by`, `parser_version`, `change_note`
   - `created_at`, `published_at`
2. `skill_index_entries`
   - `id`, `entry_id`, `version_id`, `skill_name`
   - `source_path`, `heading_path`, `title`
   - `start_line`, `end_line`
   - `summary`, `review_status`, `origin`, `fingerprint`
   - `created_at`, `updated_at`
3. `skill_index_active_versions`
   - `skill_name` (PK), `active_version_id`, `updated_by`, `updated_at`
4. （可选）`skill_index_decisions`
   - 审核动作审计（actor/action/reason/timestamp）

索引建议：
- `idx_siv_skill_status_created` (`skill_name`, `status`, `created_at DESC`)
- `idx_sie_version_review` (`version_id`, `review_status`)
- `idx_sie_skill_source_line` (`skill_name`, `source_path`, `start_line`)

## B2. Store 层

新增文件建议：
- `internal/store/skill_index.go`
- `internal/store/skill_index_test.go`
- `internal/store/models.go`（追加结构体定义，或独立 `models_skill_index.go`）

核心方法：
- `CreateVersionDraft(...)`
- `UpsertEntries(...)`
- `ListVersions(skill, status, limit, cursor)`
- `GetVersion(versionID)`
- `ListEntries(versionID, filters...)`
- `UpdateReviewDecisions(versionID, updates...)`
- `PublishVersion(skill, versionID, actor)`
- `RollbackActiveVersion(skill, targetVersionID, actor)`
- `GetActiveVersion(skill)`

## B3. 数据层退出条件

1. 迁移可重复执行、幂等安全。
2. store 层可完成版本流转与条目更新。
3. 回滚只切指针，不复制数据。

---

## 阶段 C：解析与版本流服务（M2）

## C1. Markdown 分段解析器（确定性）

新增文件建议：
- `internal/service/skill_index_parser.go`
- `internal/service/skill_index_parser_test.go`

能力：
1. 解析 frontmatter 与标题树。
2. 计算段落行号区间（`start_line/end_line`）。
3. 生成 `heading_path` 与 `fingerprint`。
4. 支持子文件映射（从“子文件按需加载”表格抽取路径）。

## C2. 模型摘要生成器（可替换）

新增接口建议：
- `type SkillDigestGenerator interface { Generate(ctx, chunk) (summary string, err error) }`

实现建议：
1. `HeuristicDigestGenerator`（测试/降级）
2. `ModelDigestGenerator`（生产）

要求：
- 每条摘要长度限制（字符/估算 token）
- 失败时保留条目并标记 `summary_pending`

## C3. 重解析 diff 分类器

新增文件建议：
- `internal/service/skill_index_diff.go`
- `internal/service/skill_index_diff_test.go`

分类：
- `unchanged / changed / new / removed / conflict`

匹配优先级：
1. `fingerprint`
2. `heading_path + source_path`
3. `title + line_overlap`

## C4. 版本流服务

新增文件建议：
- `internal/service/skill_index_workflow.go`

方法：
- `ParseDraft(skill, baseVersionID, actor)`
- `ApplyReviewUpdates(versionID, updates, actor)`
- `Publish(skill, versionID, actor)`
- `Rollback(skill, targetVersionID, actor)`

## C5. 阶段退出条件

1. 从 skill 文件可稳定生成 draft 条目。
2. 重解析能产出可审的候选变更。
3. 审核发布与回滚流程在 service 层可闭环。

---

## 阶段 D：API 契约与后端接线（M3/M4 前置）

## D1. 新增方法注册

在 `internal/apiserver/methods.go` 注册：
- `skills/index/parse`
- `skills/index/read`
- `skills/index/review/update`
- `skills/index/publish`
- `skills/index/versions`
- `skills/index/rollback`

## D2. Handler 落地

新增文件建议（若执行 god-file 拆分，可放新 methods_skill_index.go）：
- `internal/apiserver/methods_skill_index.go`
- `internal/apiserver/methods_skill_index_test.go`

校验要求：
1. 参数校验（skill/version/entry/summary 长度）
2. 并发控制（同 skill parse/publish 串行）
3. 错误码一致（沿用 `pkg/errors` 风格）

## D3. API 退出条件

1. 六个 API 可完整跑通。
2. 返回结构满足前端工作台展示需求（树、diff、统计、状态）。

---

## 阶段 E：运行时注入改造（M3）

## E1. 注入链路切换

当前关键注入点（保持顺序）：
- `buildConfiguredSkillPrompt`
- `buildSelectedSkillPrompt`
- `buildAutoMatchedSkillPrompt`

改造目标：
1. 在 digest/hybrid 模式优先读 active 版本摘要。
2. 构建 `SkillDigest` 注入文本（标题 + 行号 + 摘要）。
3. 达到预算上限时截断并附 `...N more entries`。
4. 无索引时 fallback legacy 全文注入。

建议改动文件：
- `internal/apiserver/methods.go`
- `internal/service/skills.go`（扩展读取摘要入口，保留 `ReadSkillContent`）
- （可选）`internal/service/skill_runtime_digest.go`

## E2. `turn/start` 与 `turn/steer` 一致性

两条链路必须一致执行 mode 逻辑，避免行为漂移。

## E3. 退出条件

1. digest 模式下不再默认全文注入。
2. fallback 路径可用且有日志可观测。
3. 现有 skills tests 通过，新增 digest tests 通过。

---

## 阶段 F：前端审核工作台（M4）

> 说明：本仓库前端代码尚未入树时，先完成 API + 状态契约与交互文档；若 UI 在同仓库则按以下实施。

## F1. 页面功能

1. 左栏索引树：来源文件分组、标题层级、状态标记、冲突角标。
2. 右栏审核区：原文片段、模型摘要、当前摘要、差异视图、编辑框。
3. 顶栏操作：重解析、批量采纳低风险、发布、回滚。

## F2. 交互细节

1. 筛选：未审核/冲突/已采纳/已拒绝。
2. 搜索：标题/摘要/来源文件。
3. 定位：点击条目跳转原文行号。
4. 保存：自动草稿保存 + 手动保存。
5. 冲突：必须显式决策后才可发布。

## F3. UI 退出条件

1. 从“重解析”到“发布生效”全流程可闭环。
2. 审核效率满足大文档场景（支持批处理 + 快速定位）。

---

## 阶段 G：测试矩阵与质量门禁（M5）

## G1. 后端单测

新增/扩展：
- `internal/service/skill_index_parser_test.go`
- `internal/service/skill_index_diff_test.go`
- `internal/store/skill_index_test.go`
- `internal/apiserver/methods_skill_index_test.go`
- `internal/apiserver/methods_skills_test.go`（扩展 digest/fallback 用例）

## G2. 迁移测试

新增：
- `internal/store/skill_index_migration_test.go`

检查点：
- 新迁移文件存在
- 包含必要表与关键列
- 主键/唯一约束满足 `ON CONFLICT` 使用

## G3. 回归测试命令（建议）

```bash
go test ./internal/service/... -count=1
go test ./internal/store/... -count=1
go test ./internal/apiserver/... -count=1
go test ./... -count=1
```

## G4. 验收指标

1. 默认注入 token 降幅 `>=50%`（以基线日志对比）。
2. 解析失败场景 100% 可回退 legacy。
3. 发布/回滚路径无数据不一致问题。
4. 人工审核后注入内容可追溯到原文行号区间。

---

## 3. 提交批次（建议原子提交）

1. `feat(db): add skill index version tables and constraints`
2. `feat(store): add skill index stores and models`
3. `feat(service): add markdown parser and reparse diff classifier`
4. `feat(apiserver): add skills/index parse-review-publish APIs`
5. `feat(apiserver): add digest injection mode with fallback`
6. `feat(ui): add skill index review workspace`（若 UI 在同仓库）
7. `test: add parser/store/apiserver migration coverage`
8. `docs: add rollout playbook and operational runbook`

---

## 4. 风险与对策

| 风险 | 影响 | 对策 |
|---|---|---|
| 模型摘要质量波动 | 审核负担上升 | 强制人工发布，低质量标记冲突 |
| 行号漂移导致定位不准 | 审核效率下降 | 使用 `heading_path + fingerprint` 双锚点 |
| 并发重解析与发布冲突 | 数据不一致 | skill 级互斥锁 + 事务切换 active |
| digest 截断过度 | 模型上下文不足 | 可配置预算 + 关键条目优先级 |
| 灰度期间行为差异 | 线上不可控 | `hybrid` 默认 + 快速回切开关 |

---

## 5. 运行手册（上线前必备）

1. 开关策略：
   - 默认 `legacy`
   - 单 skill 灰度启用 `hybrid`
   - 稳定后切 `digest`
2. 监控看板：
   - 模式分布（legacy/digest/fallback）
   - 每 turn 注入成本
   - parse 成功率/冲突率
3. 故障处理：
   - 解析服务异常：切回 `legacy`
   - 索引数据异常：回滚 active 版本
4. 回滚策略：
   - 代码回滚 + 配置开关回退双保险

---

## 6. Definition of Done（DoD）

全部满足才算完成：

1. API、store、service、注入链路均有自动化测试覆盖。
2. 至少 1 个真实 skill（`后端`）完成 parse→review→publish→inject→rollback 全链路验收。
3. token 成本在灰度数据中达到目标降幅。
4. 文档与 runbook 完整，值班同学可按文档执行回滚。

---

## 7. 立即执行清单（Next 10 Tasks）

1. 建立 migration 草案与表结构评审。
2. 补 store 结构体与 CRUD 接口草案。
3. 完成 markdown 标题+行号解析器。
4. 完成 `fingerprint` 与 diff 分类器。
5. 接入 `skills/index/parse` 与 `skills/index/read`。
6. 接入 `review/update` 与 `publish/rollback`。
7. 在 `turn/start` 引入 digest 模式分支。
8. 在 `turn/steer` 对齐相同分支。
9. 增加 fallback 日志与指标。
10. 对 `后端` skill 做端到端灰度验收。

