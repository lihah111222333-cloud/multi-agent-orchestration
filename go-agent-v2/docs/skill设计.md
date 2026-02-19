# Skill 注入索引化设计（精细版）

## 文档信息

- 文档状态：`Draft (for review)`
- 最后更新：`2026-02-19`
- 适用范围：Go 后端 skill 注入链路 + 前端审核工作台
- 目标读者：后端、前端、产品、运维

---

## 1. 背景与问题定义

当前注入逻辑在命中 skill 后，会读取完整 `SKILL.md` 并拼接到本次 prompt，导致：

1. **token 成本高**：非当前任务所需段落也被注入。
2. **上下文噪音大**：模型在长文中定位重点成本高。
3. **人工维护弱**：缺乏可审核、可版本化的摘要层。

本设计将“全文注入”升级为“索引摘要注入 + 按需细节加载”。

---

## 2. 目标与非目标

## 2.1 目标

1. 默认注入摘要，不注入全文。
2. 摘要条目必须可定位原文（标题 + 行号区间）。
3. 支持“模型重新解析 → 人工审核 → 发布版本”闭环。
4. 支持版本管理、回滚、审计。
5. 在不破坏现有能力的前提下灰度上线。

## 2.2 非目标（V1 不做）

1. 不做 embedding 向量检索。
2. 不引入外部搜索服务。
3. 不要求一次性改造全部 skill，仅支持渐进接入。

---

## 3. 总体方案

## 3.1 核心策略

- **默认注入**：`SkillDigest`（摘要卡片）
- **按需加载**：根据条目 `source_path + line range` 拉细节
- **fallback**：
  - 显式 full 模式（如 `[skill:xxx#full]`）
  - 无可用索引时降级旧逻辑（完整 `SKILL.md`）

## 3.2 两层结构

1. **索引层（可审核）**
   - 条目：标题、行号区间、摘要、来源文件、状态
2. **运行时注入层（可控 token）**
   - 读取“当前生效版本”的摘要条目注入

---

## 4. 索引模型（条目级）

每个索引条目结构：

- `entry_id`：唯一 ID（建议 ULID）
- `skill_name`
- `source_path`：`SKILL.md` 或子文件路径
- `heading_path`：完整标题路径（如 `Go后端开发规范/核心强制规则/错误处理`）
- `title`：当前段标题
- `start_line`：起始行（1-based）
- `end_line`：结束行（1-based，必填）
- `summary`：30~120 字可注入摘要
- `review_status`：`draft | accepted | rejected | edited | conflict`
- `origin`：`model | human | carry_forward`
- `fingerprint`：用于重解析匹配（标题+正文哈希）

### 4.1 行号规则

1. 标题行作为 `start_line`。
2. 下一个同级或更高层标题前一行作为 `end_line`。
3. 文末段落 `end_line` 为文件最后一行。
4. 重复标题依赖 `heading_path + start_line` 保证唯一性。

---

## 5. 版本管理（已选策略 A）

> 结论：重新解析不自动覆盖已审核内容，先生成候选变更，由人工确认。

## 5.1 版本头

- `version_id`
- `skill_name`
- `status`：`draft | reviewed | archived`
- `base_version_id`
- `created_by_type`：`model | human | system`
- `created_by`
- `created_at`
- `published_at`
- `change_note`
- `parser_version`

## 5.2 当前生效指针

单独维护：

- `skill_name`
- `active_version_id`
- `updated_at`
- `updated_by`

## 5.3 状态流转

1. `parse` 生成新 `draft`
2. 人工审核条目（采纳/拒绝/编辑）
3. `publish` 后标记为 `reviewed`，并切换 active 指针
4. `rollback` 可将历史 `reviewed` 重新设为 active

---

## 6. 重解析与冲突策略

## 6.1 匹配键

优先级：
1. `fingerprint` 完全一致
2. `heading_path + source_path` 一致
3. `title` 相同且行号区间高度重叠（容忍轻微偏移）

## 6.2 结果分类

- `unchanged`：自动 carry-forward（保持已审核）
- `changed`：生成候选更新，待人工确认
- `new`：新条目待审核
- `removed`：旧条目标记候选删除
- `conflict`：无法可靠匹配，必须人工处理

## 6.3 合并原则

- 已审核条目不被静默覆盖。
- 批量采纳只允许低风险（`unchanged/changed-low-risk`）。
- 高风险与冲突必须逐条决策。

---

## 7. 前端工作台设计（人工整理优先）

## 7.1 页面布局（双栏）

- 左栏：索引树（`source_path > heading`）
  - 显示：标题、行号区间、状态、差异标记
- 右栏：审核面板
  - 上：原文片段（带行号）
  - 中：模型摘要 vs 当前摘要 diff
  - 下：编辑区 + 操作按钮

## 7.2 核心操作

1. `重新解析`（创建新 `draft`）
2. `逐条采纳`
3. `逐条拒绝`
4. `编辑后采纳`
5. `批量采纳低风险`
6. `发布版本`
7. `回滚版本`

## 7.3 可用性要求

1. 支持筛选：`未审核/冲突/已采纳/已拒绝`
2. 支持全文搜索（标题、摘要、来源）
3. 支持跳转原文（按行号）
4. 支持草稿自动保存（本地 + 服务端）
5. 支持“仅看有变更”

## 7.4 前端状态约定

- 条目状态：灰（draft）、绿（accepted）、黄（edited）、红（conflict）、淡灰（rejected）
- 版本状态：草稿、已发布、历史

---

## 8. 后端 API 契约（V1）

> 采用现有 methods 风格：`skills/index/*`

## 8.1 `skills/index/parse`

请求：

```json
{
  "skill_name": "后端",
  "base_version_id": "optional",
  "reason": "manual_reparse"
}
```

响应：

```json
{
  "ok": true,
  "version_id": "v_01...",
  "stats": {
    "total_entries": 86,
    "new": 5,
    "changed": 11,
    "unchanged": 68,
    "conflict": 2
  }
}
```

## 8.2 `skills/index/read`

请求：

```json
{
  "skill_name": "后端",
  "version_id": "optional",
  "active_only": false
}
```

响应包含版本头 + 条目列表 + 统计。

## 8.3 `skills/index/review/update`

请求（支持批量）：

```json
{
  "version_id": "v_01...",
  "updates": [
    { "entry_id": "e1", "action": "accept" },
    { "entry_id": "e2", "action": "reject", "reason": "noise" },
    { "entry_id": "e3", "action": "edit_accept", "summary": "人工修订摘要" }
  ]
}
```

## 8.4 `skills/index/publish`

请求：

```json
{
  "skill_name": "后端",
  "version_id": "v_01...",
  "change_note": "审核完成"
}
```

行为：事务内切换 active 指针。

## 8.5 `skills/index/versions`

查询版本历史（支持分页、状态过滤）。

## 8.6 `skills/index/rollback`

请求：

```json
{
  "skill_name": "后端",
  "target_version_id": "v_00..."
}
```

---

## 9. 运行时注入算法（token 控制）

## 9.1 注入顺序（保持现有）

1. 配置技能
2. 手动选择技能
3. 自动匹配技能

## 9.2 每个 skill 的注入策略

1. 若有 active 索引：
   - 生成 `SkillDigest`，按条目注入 `title + line-range + summary`
2. 若请求 full 或无索引：
   - 降级全量 `SKILL.md`

## 9.3 digest 预算（建议默认）

- 单 skill 摘要上限：`<= 700 tokens`
- 单条摘要上限：`<= 80 tokens`
- 单 turn 所有 skill 摘要总上限：`<= 1600 tokens`
- 超限策略：按优先级截断并标记 `...N more entries`

---

## 10. 失败恢复与一致性

1. 解析失败：active 版本不变，继续服务。
2. 发布失败：事务回滚，不产生半发布。
3. 并发控制：同 skill 的 parse/publish 串行锁。
4. 可观测：记录 parse 耗时、冲突数、注入模式（digest/full/fallback）。
5. 兼容开关：支持快速切回 legacy 全量注入。

---

## 11. 安全与质量约束

1. 路径安全：严格校验 `source_path`，禁止目录穿越。
2. 内容大小限制：单 skill、单条摘要、单次解析输入设上限。
3. 摘要质量检查：
   - 空摘要拒绝发布
   - 摘要过长自动截断并标记
4. 审计要求：
   - 记录谁在何时对哪条做了什么决策

---

## 12. 测试计划（细化）

## 12.1 后端单元测试

- 标题树解析、行号区间计算（中英文、空段、重复标题）
- 重解析匹配分类（unchanged/changed/conflict）
- review 更新与批量更新
- publish/rollback 事务一致性

## 12.2 后端集成测试

- parse → review → publish → inject 全链路
- fallback 路径（无索引/解析失败）
- 并发 parse/publish 冲突保护

## 12.3 前端测试

- 双栏工作台渲染
- 筛选、搜索、跳转、diff 展示
- 批量采纳与发布流程
- 回滚后状态一致性

## 12.4 验收指标（量化）

1. 默认注入 token 相比旧方案下降 `>= 50%`
2. 生产无索引场景可 100% 回退 legacy
3. 审核操作可追踪（审计日志完整）
4. 关键路径错误率不高于现状

---

## 13. 灰度发布计划

## 阶段 0：兼容接入

- 不改变默认行为，仅接入数据结构与 API（功能开关关闭）

## 阶段 1：单 skill 灰度（`后端`）

- 开启 digest 注入，监控 token 降幅与命中质量

## 阶段 2：小规模扩展

- 扩展到 5~10 个高频 skill

## 阶段 3：全量推广

- 默认 digest，保留 full fallback 开关

---

## 14. 配置与开关

- `SKILL_INJECTION_MODE=legacy|digest|hybrid`
- `SKILL_DIGEST_MAX_TOKENS=700`
- `SKILL_TOTAL_MAX_TOKENS=1600`
- `SKILL_INDEX_REQUIRE_REVIEW=true`

`hybrid` 建议作为默认：有索引用 digest，无索引用 legacy。

---

## 15. 实施边界（本阶段）

本阶段仅完成：
1. 索引模型 + 版本管理 + 审核流设计落地
2. digest 注入主链路
3. fallback 与灰度开关

后续再评估是否升级到倒排检索或向量检索。

---

## 16. 下一步

基于本设计，进入 `@编写计划`，拆分为可执行任务：
- 后端数据层
- API 层
- 注入链路改造
- 前端工作台
- 观测与灰度

详细实施计划见：
- `docs/plans/2026-02-19-skill-index-injection-implementation-plan.md`
