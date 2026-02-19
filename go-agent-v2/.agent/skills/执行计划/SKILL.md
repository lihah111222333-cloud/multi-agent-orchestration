---
name: 执行计划
description: 有书面实现计划时使用，分批执行并在批次间进行审查检查点
aliases: ["@执行计划", "@execute-plan"]
---

# 执行计划 (Executing Plans)

## 概述 (Overview)

加载计划，批判性审查，分批执行，批次间报告等待审查。

**核心原则 (Core principle):** 分批执行 + 检查点审查。(Batch execution with checkpoints for architect review.)

**开始时宣布 (Announce at start):** "我正在使用执行计划技能来实现这个计划。"

## 流程 (The Process)

### Step 1: Load and Review Plan 第一步：加载并审查计划

1. 读取计划文件 (Read plan file)
2. 批判性审查 - 识别对计划的任何疑问或担忧 (Review critically - identify any questions or concerns)
3. 有担忧：开始前向老公提出 (If concerns: Raise them before starting)
4. 无担忧：创建待办事项列表并继续 (If no concerns: Create TodoWrite and proceed)

### Step 2: Execute Batch 第二步：执行批次

**默认：前 3 个任务 (Default: First 3 tasks)**

每个任务 (For each task):
1. 标记为进行中 (Mark as in_progress)
2. 严格按每个步骤执行（计划有精细步骤）(Follow each step exactly - plan has bite-sized steps)
3. 按计划运行验证 (Run verifications as specified)
4. 标记为已完成 (Mark as completed)

### Step 3: Report 第三步：报告

批次完成时 (When batch complete):
- 展示实现了什么 (Show what was implemented)
- 展示验证输出 (Show verification output)
- 说："等待反馈。" (Say: "Ready for feedback.")

### Step 4: Continue 第四步：继续

根据反馈 (Based on feedback):
- 需要时应用更改 (Apply changes if needed)
- 执行下一批次 (Execute next batch)
- 重复直到完成 (Repeat until complete)

### Step 5: Complete Development 第五步：完成开发

所有任务完成并验证后 (After all tasks complete and verified):
- 宣布："我正在使用完成开发分支技能来完成此工作。"
- **必须使用子技能 (REQUIRED SUB-SKILL):** `@完成分支`
- 遵循该技能验证测试、呈现选项、执行选择

## 何时停下来求助 (When to Stop and Ask for Help)

**立即停止执行 (STOP executing immediately when):**
- 批次中遇到阻塞（缺少依赖、测试失败、指令不清）
- 计划有关键缺口无法开始
- 不理解某个指令
- 验证反复失败

**有疑问就问，不要猜测。(Ask for clarification rather than guessing.)**

## 何时回到早期步骤 (When to Revisit Earlier Steps)

**返回审查（第一步）当 (Return to Review - Step 1 - when):**
- 老公根据你的反馈更新了计划 (Partner updates the plan based on your feedback)
- 根本方法需要重新思考 (Fundamental approach needs rethinking)

**不要强行通过阻塞 (Don't force through blockers)** - 停下来问。

## 记住 (Remember)

- 先批判性审查计划 (Review plan critically first)
- 严格按计划步骤执行 (Follow plan steps exactly)
- 不跳过验证 (Don't skip verifications)
- 计划说引用技能就引用 (Reference skills when plan says to)
- 批次间：报告并等待 (Between batches: just report and wait)
- 遇阻就停，不要猜 (Stop when blocked, don't guess)
- 未经用户明确同意，永远不要在 main/master 分支上开始实现 (Never start implementation on main/master branch without explicit user consent)

## 集成 (Integration)

**必需的工作流技能 (Required workflow skills):**
- `@Git工作树` - **必须**: 在开始前设置隔离工作区
- `@编写计划` - 创建此技能执行的计划
- `@完成分支` - 所有任务完成后收尾开发
