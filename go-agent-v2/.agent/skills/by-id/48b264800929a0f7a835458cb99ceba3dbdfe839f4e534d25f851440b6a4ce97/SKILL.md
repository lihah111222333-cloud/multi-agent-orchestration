---
name: 请求代码审查
description: 完成任务、实现主要功能或合并前使用，以验证工作满足需求
aliases: ["@请求审查", "@code-review"]
---

# 请求代码审查

派遣代码审查子代理，在问题级联前捕获问题。

**核心原则：** 早审查，勤审查。

## 何时请求审查

**必须：**
- 子代理驱动开发中每个任务后
- 完成主要功能后
- 合并到主分支前

**可选但有价值：**
- 卡住时（新视角）
- 重构前（基线检查）
- 复杂 bug 修复后

## 如何请求

**1. 获取 git SHA：**
```bash
BASE_SHA=$(git rev-parse HEAD~1)  # 或 origin/main
HEAD_SHA=$(git rev-parse HEAD)
```

**2. 派遣代码审查子代理：**

使用 Task 工具，填充模板：

**占位符：**
- `{WHAT_WAS_IMPLEMENTED}` - 你刚构建了什么
- `{PLAN_OR_REQUIREMENTS}` - 应该做什么
- `{BASE_SHA}` - 起始提交
- `{HEAD_SHA}` - 结束提交
- `{DESCRIPTION}` - 简要描述

**3. 根据反馈行动：**
- 立即修复 Critical 问题
- 继续前修复 Important 问题
- 记录 Minor 问题留待以后
- 如果审查者错了就反驳（带理由）

## 示例

```
[刚完成任务 2：添加验证函数]

你：让我在继续前请求代码审查。

BASE_SHA=$(git log --oneline | grep "Task 1" | head -1 | awk '{print $1}')
HEAD_SHA=$(git rev-parse HEAD)

[派遣代码审查子代理]
  WHAT_WAS_IMPLEMENTED: 对话索引的验证和修复函数
  PLAN_OR_REQUIREMENTS: docs/plans/deployment-plan.md 中的任务 2
  BASE_SHA: a7981ec
  HEAD_SHA: 3df7661
  DESCRIPTION: 添加了 verifyIndex() 和 repairIndex()，支持 4 种问题类型

[子代理返回]:
  优点：干净架构，真实测试
  问题：
    Important：缺少进度指示器
    Minor：魔法数字 (100) 用于报告间隔
  评估：可以继续

你：[修复进度指示器]
[继续任务 3]
```

## 工作流集成

**子代理驱动开发：**
- 每个任务后审查
- 问题级联前捕获
- 下一任务前修复

**执行计划：**
- 每批次（3 个任务）后审查
- 获取反馈，应用，继续

**临时开发：**
- 合并前审查
- 卡住时审查

## 危险信号

**永不：**
- 因为"很简单"就跳过审查
- 忽略 Critical 问题
- 带着未修复的 Important 问题继续
- 与有效的技术反馈争论

**如果审查者错了 (If reviewer wrong):**
- 用技术理由反驳 (Push back with technical reasoning)
- 展示证明其工作的代码/测试 (Show code/tests that prove it works)
- 请求澄清 (Request clarification)

