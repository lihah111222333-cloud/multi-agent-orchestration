---
name: 完成前验证
description: 即将声称工作完成、修复或通过时使用，在提交或创建 PR 之前 - 要求运行验证命令并确认输出后才能做任何成功声明；证据永远在断言之前
aliases: ["@验证", "@verify"]
---

# 完成前验证 (Verification Before Completion)

## 概述 (Overview)

声称工作完成而不验证是不诚实，不是效率。

**核心原则 (Core principle):** 证据在声明之前，永远如此。(Evidence before claims, always.)

**违反这条规则的字面意思就是违反规则的精神。(Violating the letter of this rule is violating the spirit of this rule.)**

## 铁律 (The Iron Law)

```
NO COMPLETION CLAIMS WITHOUT FRESH VERIFICATION EVIDENCE
没有新鲜的验证证据，就没有完成声明
```

如果你没有在这条消息中运行验证命令，你不能声称它通过了。

## 门控函数 (The Gate Function)

```
BEFORE claiming any status or expressing satisfaction:
在声称任何状态或表达满意之前：

1. IDENTIFY 识别：什么命令证明这个声明？
2. RUN 运行：执行完整命令（新鲜的，完整的）
3. READ 阅读：完整输出，检查退出码，计算失败数
4. VERIFY 验证：输出是否确认声明？
   - 如果否：用证据陈述实际状态
   - 如果是：用证据陈述声明
5. ONLY THEN 然后才：做出声明

Skip any step = lying, not verifying
跳过任何步骤 = 撒谎，不是验证
```

## 常见失败 (Common Failures)

| 声明 (Claim) | 需要 (Requires) | 不够 (Not Sufficient) |
|--------------|-----------------|----------------------|
| 测试通过 (Tests pass) | 测试命令输出：0 失败 | 上次运行，"应该通过" |
| Lint 干净 (Linter clean) | Linter 输出：0 错误 | 部分检查，推断 |
| 构建成功 (Build succeeds) | 构建命令：exit 0 | Lint 通过，日志看起来好 |
| Bug 修复 (Bug fixed) | 原始症状测试：通过 | 代码改了，假设修复了 |
| 回归测试工作 (Regression test works) | 红-绿循环验证 | 测试通过一次 |
| 代理完成 (Agent completed) | VCS diff 显示变更 | 代理报告"成功" |
| 需求满足 (Requirements met) | 逐行检查清单 | 测试通过 |

## 危险信号 (Red Flags) - 停下

- 使用"应该 (should)"、"大概 (probably)"、"看起来 (seems to)"
- 验证前表达满意（"太好了！"、"完美！"、"搞定！"等）
- 即将提交/推送/PR 而未验证
- 信任代理成功报告 (Trusting agent success reports)
- 依赖部分验证 (Relying on partial verification)
- 想着"就这一次" (Thinking "just this once")
- 累了想结束工作 (Tired and wanting work over)
- **任何暗示成功但未运行验证的措辞 (ANY wording implying success without having run verification)**

## 借口预防 (Rationalization Prevention)

| 借口 (Excuse) | 现实 (Reality) |
|---------------|----------------|
| "现在应该行了" (Should work now) | 运行验证 (RUN the verification) |
| "我很有信心" (I'm confident) | 信心 ≠ 证据 (Confidence ≠ evidence) |
| "就这一次" (Just this once) | 没有例外 (No exceptions) |
| "Linter 通过了" (Linter passed) | Linter ≠ 编译器 (Linter ≠ compiler) |
| "代理说成功" (Agent said success) | 独立验证 (Verify independently) |
| "我累了" (I'm tired) | 疲惫 ≠ 借口 (Exhaustion ≠ excuse) |
| "部分检查够了" (Partial check is enough) | 部分证明不了什么 (Partial proves nothing) |
| "换个说法所以规则不适用" (Different words so rule doesn't apply) | 精神重于字面 (Spirit over letter) |

## 关键模式 (Key Patterns)

**测试 (Tests):**
```
✅ [运行测试命令] [看到：34/34 通过] "所有测试通过"
❌ "现在应该通过" / "看起来正确"
```

**回归测试 (Regression tests - TDD Red-Green):**
```
✅ 写 → 运行（通过）→ 还原修复 → 运行（必须失败）→ 恢复 → 运行（通过）
❌ "我写了回归测试"（没有红-绿验证）
```

**构建 (Build):**
```
✅ [运行构建] [看到：exit 0] "构建通过"
❌ "Linter 通过了"（linter 不检查编译）
```

**需求 (Requirements):**
```
✅ 重读计划 → 创建检查清单 → 逐项验证 → 报告缺口或完成
❌ "测试通过，阶段完成"
```

**代理委托 (Agent delegation):**
```
✅ 代理报告成功 → 检查 VCS diff → 验证变更 → 报告实际状态
❌ 信任代理报告
```

## 为什么重要 (Why This Matters)

来自 24 个失败记忆：
- 老公说"我不信你" - 信任破裂 (trust broken)
- 未定义函数被发布 - 会崩溃 (would crash)
- 缺少需求被发布 - 功能不完整 (incomplete features)
- 假完成浪费时间 → 重定向 → 返工 (rework)
- 违反："诚实是核心价值。如果你撒谎，你会被替换。" (Honesty is a core value. If you lie, you'll be replaced.)

## 何时应用 (When To Apply)

**始终在以下之前 (ALWAYS before):**
- 任何成功/完成声明的变体
- 任何满意表达
- 任何关于工作状态的正面陈述
- 提交、PR 创建、任务完成
- 移动到下一个任务
- 委托给代理

**规则适用于 (Rule applies to):**
- 确切短语
- 换种说法和同义词
- 成功的暗示
- 任何暗示完成/正确的沟通

## 底线 (The Bottom Line)

**验证没有捷径。(No shortcuts for verification.)**

运行命令。阅读输出。然后声称结果。(Run the command. Read the output. THEN claim the result.)

这是不可商量的。(This is non-negotiable.)
