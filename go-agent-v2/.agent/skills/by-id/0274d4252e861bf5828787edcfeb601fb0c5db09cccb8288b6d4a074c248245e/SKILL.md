---
name: 编写计划
description: 有规格或需求的多步骤任务时使用，在动代码之前
aliases: ["@编写计划", "@write-plan"]
---

# 编写实现计划

## 概述

编写全面的实现计划，假设工程师对代码库零上下文且品味堪忧。记录他们需要知道的一切：每个任务要改哪些文件、代码、测试、相关文档、如何验证。把完整计划拆成小任务。DRY. YAGNI. TDD. 频繁提交。

假设他们是熟练开发者，但对我们的工具集或问题域几乎一无所知。假设他们对好的测试设计不太了解。(Assume they are a skilled developer, but know almost nothing about our toolset or problem domain.)

**开始时宣布 (Announce at start)：** "我正在使用编写计划技能来创建实现计划。"

**上下文 (Context):** 这应该在专用工作树中运行（由头脑风暴技能创建）。

**保存到 (Save plans to)：** `docs/plans/YYYY-MM-DD-<功能名>.md`

## 任务粒度

**每步是一个动作（2-5 分钟）：**
- "写失败的测试" - 一步
- "运行确认失败" - 一步
- "写最小代码通过测试" - 一步
- "运行测试确认通过" - 一步
- "提交" - 一步

## 计划文档头部

**每个计划必须以此开头：**

```markdown
# [功能名] 实现计划

> **给 Claude:** 必须使用 @执行计划 逐任务实现此计划。

**目标:** [一句话描述构建什么]

**架构:** [2-3 句话描述方法]

**技术栈:** [关键技术/库]

---
```

## 任务结构

````markdown
### 任务 N: [组件名]

**文件:**
- 创建: `exact/path/to/file.py`
- 修改: `exact/path/to/existing.py:123-145`
- 测试: `tests/exact/path/to/test.py`

**步骤 1: 写失败的测试**

```python
def test_specific_behavior():
    result = function(input)
    assert result == expected
```

**步骤 2: 运行测试确认失败**

运行: `pytest tests/path/test.py::test_name -v`
预期: FAIL "function not defined"

**步骤 3: 写最小实现**

```python
def function(input):
    return expected
```

**步骤 4: 运行测试确认通过**

运行: `pytest tests/path/test.py::test_name -v`
预期: PASS

**步骤 5: 提交**

```bash
git add tests/path/test.py src/path/file.py
git commit -m "feat: add specific feature"
```
````

## 记住

- 总是精确文件路径
- 计划中包含完整代码（不是"添加验证"）
- 精确命令和预期输出
- 用 @ 语法引用相关技能
- DRY, YAGNI, TDD, 频繁提交

## 执行交接 (Execution Handoff)

保存计划后，提供执行选择：

**"计划完成并保存到 `docs/plans/<文件名>.md`。两种执行选项：**

**1. 子代理驱动（本会话）(Subagent-Driven)** - 每任务派遣新子代理，任务间审查，快速迭代

**2. 并行会话（单独）(Parallel Session)** - 新会话用 @执行计划，分批执行带检查点

**选哪个？"**

**如果选择子代理驱动 (If Subagent-Driven chosen):**
- **必需子技能 (REQUIRED SUB-SKILL):** 使用 @子代理开发
- 留在本会话
- 每任务新子代理 + 代码审查

**如果选择并行会话 (If Parallel Session chosen):**
- 引导他们在工作树中打开新会话
- **必需子技能 (REQUIRED SUB-SKILL):** 新会话使用 @执行计划
