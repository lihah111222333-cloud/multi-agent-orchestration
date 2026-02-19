---
name: 系统性调试
description: 遇到任何 bug、测试失败或意外行为时使用，在提出修复方案之前
aliases: ["@调试", "@debug"]
---

# 系统性调试 (Systematic Debugging)

## 概述 (Overview)

随机修复浪费时间并制造新 bug。快速补丁掩盖根本问题。

**核心原则 (Core principle):** 修复前必须找到根因 (root cause)。只治症状 (symptom) 是失败。

**违反这个流程的字面意思就是违反调试的精神。(Violating the letter of this process is violating the spirit of debugging.)**

## 铁律 (The Iron Law)

```
NO FIXES WITHOUT ROOT CAUSE INVESTIGATION FIRST
没有根因调查，就没有修复
```

如果你没完成第一阶段，你不能提出修复方案。
(If you haven't completed Phase 1, you cannot propose fixes.)

## 何时使用 (When to Use)

用于任何技术问题 (Use for ANY technical issue):
- 测试失败 (Test failures)
- 生产 bug (Bugs in production)
- 意外行为 (Unexpected behavior)
- 性能问题 (Performance problems)
- 构建失败 (Build failures)
- 集成问题 (Integration issues)

**尤其在以下情况使用 (Use this ESPECIALLY when):**
- 时间压力下（紧急情况让猜测变得诱人）
- "就一个快速修复"看起来很明显
- 你已经尝试了多次修复
- 之前的修复没起作用
- 你没完全理解问题

**不要跳过当 (Don't skip when):**
- 问题看起来简单（简单 bug 也有根因）
- 你很急（急躁保证返工）
- 老板想立即修好（系统化比来回折腾更快）

## 四个阶段 (The Four Phases)

必须完成每个阶段才能进入下一阶段。
(You MUST complete each phase before proceeding to the next.)

### Phase 1: Root Cause Investigation 阶段一：根因调查

**在尝试任何修复之前 (BEFORE attempting ANY fix):**

1. **仔细阅读错误信息 (Read Error Messages Carefully)**
   - 不要跳过错误或警告
   - 它们通常包含确切的解决方案
   - 完整阅读堆栈跟踪 (stack traces)
   - 记下行号、文件路径、错误代码

2. **稳定复现 (Reproduce Consistently)**
   - 能可靠地触发吗？
   - 确切的步骤是什么？
   - 每次都发生吗？
   - 如果不能复现 → 收集更多数据，不要猜测

3. **检查最近变更 (Check Recent Changes)**
   - 什么改动可能导致这个问题？
   - Git diff，最近提交
   - 新依赖，配置变更
   - 环境差异

4. **多组件系统收集证据 (Gather Evidence in Multi-Component Systems)**

   **当系统有多个组件时（CI → 构建 → 签名，API → 服务 → 数据库）：**

   **提出修复前，添加诊断监控：**
   ```
   对于每个组件边界：
     - 记录什么数据进入组件
     - 记录什么数据离开组件
     - 验证环境/配置传播
     - 检查每层状态

   运行一次收集证据，显示哪里断了
   然后分析证据确定失败的组件
   然后调查那个特定组件
   ```

   **示例（多层系统）：**
   ```bash
   # Layer 1: Workflow
   echo "=== Secrets available in workflow: ==="
   echo "IDENTITY: ${IDENTITY:+SET}${IDENTITY:-UNSET}"

   # Layer 2: Build script
   echo "=== Env vars in build script: ==="
   env | grep IDENTITY || echo "IDENTITY not in environment"

   # Layer 3: Signing script
   echo "=== Keychain state: ==="
   security list-keychains
   security find-identity -v

   # Layer 4: Actual signing
   codesign --sign "$IDENTITY" --verbose=4 "$APP"
   ```

   **这揭示了：** 哪层失败（secrets → workflow ✓, workflow → build ✗）

5. **追踪数据流 (Trace Data Flow)**

   **当错误在调用栈深处时：**

   完整的回溯追踪技术参见本目录下的 `root-cause-tracing.md`。

   **简要版本 (Quick version):**
   - 坏值从哪里来的？(Where does bad value originate?)
   - 什么用坏值调用了这里？(What called this with bad value?)
   - 继续往上追踪直到找到源头
   - 在源头修复，不是在症状处 (Fix at source, not at symptom)

### Phase 2: Pattern Analysis 阶段二：模式分析

**修复前找到模式 (Find the pattern before fixing):**

1. **找到工作的例子 (Find Working Examples)**
   - 在同一代码库找类似的工作代码
   - 什么类似的东西是工作的？

2. **对照参考 (Compare Against References)**
   - 如果实现某个模式，完整阅读参考实现
   - 不要略读 - 逐行阅读
   - 在应用前完全理解模式

3. **识别差异 (Identify Differences)**
   - 工作的和坏的有什么不同？
   - 列出每个差异，无论多小
   - 不要假设"那不可能有关系"

4. **理解依赖 (Understand Dependencies)**
   - 这需要什么其他组件？
   - 什么设置、配置、环境？
   - 它做了什么假设？

### Phase 3: Hypothesis and Testing 阶段三：假设与测试

**科学方法 (Scientific method):**

1. **形成单一假设 (Form Single Hypothesis)**
   - 清楚陈述："我认为 X 是根因因为 Y"
   - 写下来
   - 要具体，不要模糊

2. **最小测试 (Test Minimally)**
   - 做最小的可能变更来测试假设
   - 一次一个变量
   - 不要同时修多个东西

3. **继续前验证 (Verify Before Continuing)**
   - 成功了？进入阶段四
   - 没成功？形成新假设
   - 不要在上面叠加更多修复 (DON'T add more fixes on top)

4. **不知道时 (When You Don't Know)**
   - 说"我不理解 X"
   - 不要假装知道
   - 请求帮助
   - 更多研究

### Phase 4: Implementation 阶段四：实现

**修复根因，不是症状 (Fix the root cause, not the symptom):**

1. **创建失败的测试用例 (Create Failing Test Case)**
   - 最简单的可能复现
   - 如果可能用自动化测试
   - 修复之前必须有
   - 使用 `@TDD` 技能写正确的失败测试

2. **实现单一修复 (Implement Single Fix)**
   - 解决确定的根因
   - 一次一个变更 (ONE change at a time)
   - 没有"顺便"改进 (No "while I'm here" improvements)
   - 没有捆绑重构 (No bundled refactoring)

3. **验证修复 (Verify Fix)**
   - 测试现在通过了？
   - 其他测试没坏？
   - 问题真的解决了？

4. **修复不起作用时 (If Fix Doesn't Work)**
   - 停下 (STOP)
   - 数一数：你尝试了多少次修复？
   - 如果 < 3：返回阶段一，用新信息重新分析
   - **如果 ≥ 3：停下来质疑架构（见下面第 5 步）**
   - 不要在没有架构讨论的情况下尝试第 4 次修复

5. **3+ 次修复失败：质疑架构 (If 3+ Fixes Failed: Question Architecture)**

   **指示架构问题的模式 (Pattern indicating architectural problem):**
   - 每次修复揭示新的共享状态/耦合/不同位置的问题
   - 修复需要"大规模重构"才能实现
   - 每次修复在其他地方产生新症状

   **停下来质疑根本 (STOP and question fundamentals):**
   - 这个模式从根本上是合理的吗？
   - 我们是不是"出于惯性坚持" (sticking with it through sheer inertia)？
   - 应该重构架构还是继续修复症状？

   **在尝试更多修复前与老公讨论**

   这不是假设失败 - 这是架构错误。(This is NOT a failed hypothesis - this is a wrong architecture.)

## 危险信号 (Red Flags) - 停下来遵循流程

如果你发现自己在想：
- "先快速修复，以后再调查" (Quick fix for now, investigate later)
- "就试试改 X 看看行不行" (Just try changing X and see if it works)
- "加多个改动，跑测试" (Add multiple changes, run tests)
- "跳过测试，我手动验证" (Skip the test, I'll manually verify)
- "大概是 X，让我修一下" (It's probably X, let me fix that)
- "我不完全理解但这可能行" (I don't fully understand but this might work)
- "模式说 X 但我会不同地调整" (Pattern says X but I'll adapt it differently)
- "主要问题是：[列出修复但没调查]" (Here are the main problems: [lists fixes without investigation])
- 没追踪数据流就提方案 (Proposing solutions before tracing data flow)
- **"再试一次修复"（已经试过 2+ 次时）(One more fix attempt - when already tried 2+)**
- **每次修复揭示不同位置的新问题 (Each fix reveals new problem in different place)**

**所有这些都意味着：停下。返回阶段一。(ALL of these mean: STOP. Return to Phase 1.)**

**如果 3+ 次修复失败：** 质疑架构（见阶段 4.5）

## 老公的信号：你做错了 (Your Human Partner's Signals You're Doing It Wrong)

**注意这些重定向：**
- "那没发生吗？" (Is that not happening?) - 你假设没验证
- "它会告诉我们...？" (Will it show us...?) - 你应该加证据收集
- "停止猜测" (Stop guessing) - 你在没理解的情况下提方案
- "Ultrathink 这个" (Ultrathink this) - 质疑根本，不只是症状
- "我们卡住了？"（沮丧）(We're stuck?) - 你的方法没用

**当你看到这些：** 停下。返回阶段一。

## 常见借口 (Common Rationalizations)

| 借口 (Excuse) | 现实 (Reality) |
|---------------|----------------|
| "问题简单，不需要流程" (Issue is simple, don't need process) | 简单问题也有根因。流程对简单 bug 很快。 |
| "紧急情况，没时间走流程" (Emergency, no time for process) | 系统调试比猜测折腾更快。 |
| "先试这个，然后再调查" (Just try this first, then investigate) | 第一次修复定下模式。从一开始就做对。 |
| "我确认修复工作后再写测试" (I'll write test after confirming fix works) | 没测试的修复不稳定。先测试证明它。 |
| "同时多个修复省时间" (Multiple fixes at once saves time) | 不能隔离什么起作用。造成新 bug。 |
| "参考太长，我会调整模式" (Reference too long, I'll adapt the pattern) | 部分理解保证 bug。完整读它。 |
| "我看到问题了，让我修" (I see the problem, let me fix it) | 看到症状 ≠ 理解根因。 |
| "再试一次修复"（2+ 次失败后）(One more fix attempt after 2+ failures) | 3+ 次失败 = 架构问题。质疑模式，别再修。 |

## 快速参考 (Quick Reference)

| 阶段 (Phase) | 关键活动 (Key Activities) | 成功标准 (Success Criteria) |
|--------------|--------------------------|----------------------------|
| **1. 根因 (Root Cause)** | 读错误，复现，查变更，收集证据 | 理解是什么和为什么 |
| **2. 模式 (Pattern)** | 找工作例子，对比 | 识别差异 |
| **3. 假设 (Hypothesis)** | 形成理论，最小测试 | 确认或新假设 |
| **4. 实现 (Implementation)** | 创建测试，修复，验证 | Bug 解决，测试通过 |

## 流程揭示"无根因"时 (When Process Reveals "No Root Cause")

如果系统调查揭示问题确实是环境相关、时序相关或外部的：

1. 你已完成流程
2. 记录你调查了什么
3. 实现适当处理（retry、timeout、error message）
4. 添加监控/日志用于未来调查

**但是：** 95% 的"无根因"情况是不完整的调查。

## 支持技术 (Supporting Techniques)

这些技术是系统性调试的一部分，在此目录下可用：
(These techniques are part of systematic debugging and available in this directory)

- **`root-cause-tracing.md`** - 通过调用栈向后追踪 bug 找到原始触发器
- **`defense-in-depth.md`** - 找到根因后在多层添加验证
- **`condition-based-waiting.md`** - 用条件轮询替换任意 timeout

**相关技能 (Related skills):**
- **@TDD** - 用于创建失败测试用例（阶段 4，步骤 1）
- **@完成前验证** - 声称成功前验证修复确实起作用

## 真实影响 (Real-World Impact)

来自调试会话：
- 系统方法：15-30 分钟修复
- 随机修复方法：2-3 小时折腾
- 首次修复成功率：95% vs 40%
- 引入新 bug：几乎为零 vs 常见
