---
name: Git工作树
description: 开始需要与当前工作区隔离的功能开发或执行实现计划前使用 - 创建带有智能目录选择和安全验证的隔离 git worktree
aliases: ["@工作树", "@worktree"]
---

# 使用 Git 工作树

## 概述

Git worktrees 创建共享同一仓库的隔离工作区，允许同时在多个分支上工作而无需切换。

**核心原则：** 系统化目录选择 + 安全验证 = 可靠隔离。

**开始时宣布：** "我正在使用 Git 工作树技能来设置隔离工作区。"

## 目录选择流程

按此优先级顺序：

### 1. 检查现有目录

```bash
# 按优先级检查
ls -d .worktrees 2>/dev/null     # 首选（隐藏）
ls -d worktrees 2>/dev/null      # 备选
```

**如果找到：** 使用该目录。如果两者都存在，`.worktrees` 优先。

### 2. 检查配置文件

```bash
grep -i "worktree.*director" CLAUDE.md 2>/dev/null
grep -i "worktree.*director" .agent/config.md 2>/dev/null
```

**如果指定了偏好：** 直接使用，不询问。

### 3. 询问用户

如果没有目录存在且无偏好：

```
未找到 worktree 目录。应在哪里创建 worktrees？

1. .worktrees/（项目本地，隐藏）
2. ~/.config/superpowers/worktrees/<项目名>/（全局位置）

你更喜欢哪个？
```

## 安全验证 (Safety Verification)

### 项目本地目录 (For Project-Local Directories)

(.worktrees or worktrees)

**创建 worktree 前必须验证目录被忽略 (MUST verify directory is ignored before creating worktree)：**

```bash
git check-ignore -q .worktrees 2>/dev/null || git check-ignore -q worktrees 2>/dev/null
```

**如果未被忽略 (If NOT ignored):**

按规则 "Fix broken things immediately"：
1. 添加适当行到 .gitignore
2. 提交更改
3. 继续创建 worktree

**为何关键 (Why critical):** 防止意外将 worktree 内容提交到仓库。

### 全局目录 (For Global Directory)

(~/.config/superpowers/worktrees)

无需 .gitignore 验证 - 完全在项目外。
(No .gitignore verification needed - outside project entirely.)

## 创建步骤

### 1. 检测项目名称

```bash
project=$(basename "$(git rev-parse --show-toplevel)")
```

### 2. 创建 Worktree

```bash
# 确定完整路径
case $LOCATION in
  .worktrees|worktrees)
    path="$LOCATION/$BRANCH_NAME"
    ;;
  ~/.config/superpowers/worktrees/*)
    path="~/.config/superpowers/worktrees/$project/$BRANCH_NAME"
    ;;
esac

# 创建带新分支的 worktree
git worktree add "$path" -b "$BRANCH_NAME"
cd "$path"
```

### 3. 运行项目设置

自动检测并运行适当设置：

```bash
# Node.js
if [ -f package.json ]; then npm install; fi

# Rust
if [ -f Cargo.toml ]; then cargo build; fi

# Python
if [ -f requirements.txt ]; then pip install -r requirements.txt; fi
if [ -f pyproject.toml ]; then poetry install; fi

# Go
if [ -f go.mod ]; then go mod download; fi
```

### 4. 验证干净基线 (Verify Clean Baseline)

运行测试确保 worktree 启动干净：

```bash
# 示例 - 使用项目适当的命令
npm test
cargo test
pytest
go test ./...
```

**如果测试失败 (If tests fail):** 报告失败，询问是否继续或调查。

**如果测试通过 (If tests pass):** 报告就绪。

### 5. 报告位置

```
Worktree 就绪于 <完整路径>
测试通过（<N> 个测试，0 失败）
准备实现 <功能名称>
```

## 快速参考

| 情况 | 操作 |
|------|------|
| `.worktrees/` 存在 | 使用它（验证忽略） |
| `worktrees/` 存在 | 使用它（验证忽略） |
| 两者都存在 | 使用 `.worktrees/` |
| 都不存在 | 检查配置 → 询问用户 |
| 目录未被忽略 | 添加到 .gitignore + 提交 |
| 基线测试失败 | 报告失败 + 询问 |
| 无 package.json/Cargo.toml | 跳过依赖安装 |

## 常见错误

### 跳过忽略验证
- **问题：** Worktree 内容被跟踪，污染 git status
- **修复：** 创建项目本地 worktree 前总是用 `git check-ignore`

### 假设目录位置
- **问题：** 造成不一致，违反项目约定
- **修复：** 遵循优先级：现有 > 配置 > 询问

### 测试失败时继续
- **问题：** 无法区分新 bug 和已有问题
- **修复：** 报告失败，获得明确许可后再继续

### 硬编码设置命令
- **问题：** 在使用不同工具的项目上失败
- **修复：** 从项目文件自动检测（package.json 等）

## 示例工作流

```
你：我正在使用 Git 工作树技能来设置隔离工作区。

[检查 .worktrees/ - 存在]
[验证忽略 - git check-ignore 确认 .worktrees/ 被忽略]
[创建 worktree: git worktree add .worktrees/auth -b feature/auth]
[运行 npm install]
[运行 npm test - 47 通过]

Worktree 就绪于 /Users/xxx/myproject/.worktrees/auth
测试通过（47 个测试，0 失败）
准备实现 auth 功能
```

## 危险信号

**永不：**
- 创建项目本地 worktree 时不验证忽略
- 跳过基线测试验证
- 测试失败不问就继续
- 模糊时假设目录位置
- 跳过配置检查

**始终：**
- 遵循目录优先级：现有 > 配置 > 询问
- 项目本地目录验证被忽略
- 自动检测并运行项目设置
- 验证干净测试基线

## 集成

**被调用于：**
- `@头脑风暴`（阶段 4）- 设计批准后实现开始时必需
- 任何需要隔离工作区的技能

**配对使用：**
- `@完成分支` - 工作完成后清理必需
- `@执行计划` 或 `@子代理开发` - 工作在此 worktree 中进行
