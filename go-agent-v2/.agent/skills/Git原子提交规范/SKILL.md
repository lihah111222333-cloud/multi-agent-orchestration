---
name: Git 原子提交规范
description: Git 版本控制最佳实践指南，涵盖原子提交、提交信息格式、分支策略和代码审查规范。适用于日常开发的版本管理。
tags: [git, version-control, commit, branch, 版本控制, Git, 提交规范, 分支策略, 代码审查]
---

# Git 原子提交规范

适用于专业软件开发的 Git 版本控制规范。

## 何时使用

在以下场景使用此技能：

- 提交代码变更
- 创建和管理分支
- 编写提交信息
- 代码审查和合并
- 解决合并冲突

---

## 第一部分：原子提交原则

### 什么是原子提交

**原子提交**：每次提交只包含一个逻辑变更，可以独立理解和回滚。

```bash
# ✅ 好的原子提交序列
git log --oneline
a1b2c3d feat(auth): add JWT token validation
b2c3d4e feat(auth): implement login endpoint
c3d4e5f feat(auth): create user model and migration
d4e5f6g chore: add bcrypt dependency

# ❌ 不好的提交
e5f6g7h add auth feature with login, JWT, user model and dependencies
```

### 原子提交规则

1. **单一职责**：一次提交只做一件事
2. **可编译**：每次提交后代码应能编译通过
3. **可测试**：每次提交后测试应能通过
4. **可理解**：提交信息能清楚描述变更
5. **可回滚**：可以独立撤销而不影响其他功能

### 拆分提交示例

```bash
# 场景：实现用户认证功能

# 第 1 步：添加依赖
git add go.mod go.sum
git commit -m "chore(deps): add bcrypt and jwt-go packages"

# 第 2 步：创建数据模型
git add internal/models/user.go
git commit -m "feat(user): add User model with password hashing"

# 第 3 步：添加数据库迁移
git add migrations/001_create_users.sql
git commit -m "feat(db): add users table migration"

# 第 4 步：实现认证逻辑
git add internal/auth/
git commit -m "feat(auth): implement JWT token generation and validation"

# 第 5 步：添加 HTTP 处理器
git add internal/handlers/auth.go
git commit -m "feat(auth): add login and register endpoints"

# 第 6 步：添加测试
git add internal/auth/*_test.go
git commit -m "test(auth): add unit tests for JWT validation"
```

---

## 第二部分：提交信息格式

### Conventional Commits 规范

```
<type>(<scope>): <subject>

[可选 body]

[可选 footer]
```

### Type 类型

| Type | 描述 | 示例 |
|------|------|------|
| `feat` | 新功能 | `feat(user): add email verification` |
| `fix` | Bug 修复 | `fix(auth): correct token expiry calculation` |
| `docs` | 文档变更 | `docs(api): update endpoint documentation` |
| `style` | 代码格式（不影响逻辑） | `style: format code with gofmt` |
| `refactor` | 重构（不改变功能） | `refactor(db): extract query builder` |
| `perf` | 性能优化 | `perf(query): add index for orders lookup` |
| `test` | 测试相关 | `test(auth): add integration tests` |
| `chore` | 构建/工具/依赖 | `chore(deps): upgrade React to v19` |
| `ci` | CI/CD 相关 | `ci: add GitHub Actions workflow` |
| `revert` | 回滚提交 | `revert: feat(user): add email verification` |

### Subject 规范

```bash
# ✅ 正确的 subject
feat(auth): add JWT token refresh endpoint
fix(order): prevent duplicate order submission
refactor(user): extract validation logic to separate function

# ❌ 错误的 subject
feat(auth): Added JWT token refresh endpoint.  # 不要大写开头，不要句号
fix: bug fix  # 太模糊
update code  # 没有 type，描述不清
```

### 完整提交示例

```bash
# 简单提交
git commit -m "feat(order): add order cancellation feature"

# 带 body 的提交
git commit -m "fix(payment): correct decimal precision in calculations

Previously, floating-point arithmetic caused rounding errors in
payment amounts. This change uses decimal.Decimal for all monetary
calculations to ensure precision.

Fixes #123"

# 带 breaking change 的提交
git commit -m "feat(api)!: change authentication header format

BREAKING CHANGE: Authorization header now requires 'Bearer' prefix.

Before: Authorization: <token>
After: Authorization: Bearer <token>

Migration: Update all API clients to include 'Bearer' prefix."
```

---

## 第三部分：分支策略

### Git Flow 简化版

```
main (生产)
  └── develop (开发)
       ├── feature/user-profile
       ├── feature/order-system
       └── fix/login-error
```

### 分支命名规范

```bash
# 功能分支
feature/user-authentication
feature/order-management
feature/JIRA-123-payment-gateway

# 修复分支
fix/login-redirect-loop
fix/JIRA-456-cart-total

# 热修复分支（生产紧急修复）
hotfix/security-patch
hotfix/critical-payment-bug

# 发布分支
release/v1.2.0
release/2024-01-sprint
```

### 分支操作

```bash
# 创建功能分支
git checkout develop
git pull origin develop
git checkout -b feature/new-feature

# 保持分支更新
git fetch origin
git rebase origin/develop

# 完成功能（推送供 PR）
git push -u origin feature/new-feature

# 合并后清理
git checkout develop
git pull origin develop
git branch -d feature/new-feature
git push origin --delete feature/new-feature
```

---

## 第四部分：常用操作

### 交互式暂存

```bash
# 交互式选择要提交的内容
git add -p

# 选项：
# y - 暂存此块
# n - 不暂存此块
# s - 分割成更小的块
# e - 手动编辑
# q - 退出
```

### 修改提交

```bash
# 修改最后一次提交信息
git commit --amend -m "新的提交信息"

# 添加遗漏的文件到最后一次提交
git add forgotten-file.go
git commit --amend --no-edit

# 交互式变基（修改多个提交）
git rebase -i HEAD~3
# 在编辑器中：
# pick   a1b2c3d 第一个提交
# squash b2c3d4e 第二个提交（合并到上一个）
# reword c3d4e5f 第三个提交（修改信息）
```

### 暂存工作

```bash
# 暂存当前变更
git stash push -m "WIP: feature description"

# 查看暂存列表
git stash list

# 恢复暂存
git stash pop  # 恢复并删除
git stash apply stash@{0}  # 恢复但保留

# 清理暂存
git stash drop stash@{0}
git stash clear
```

### 撤销操作

```bash
# 撤销工作区变更
git checkout -- file.go
git restore file.go  # Git 2.23+

# 取消暂存
git reset HEAD file.go
git restore --staged file.go  # Git 2.23+

# 撤销提交（保留变更）
git reset --soft HEAD~1

# 撤销提交（丢弃变更）
git reset --hard HEAD~1

# 安全回滚（创建新提交）
git revert HEAD
git revert a1b2c3d
```

---

## 第五部分：代码审查

### PR/MR 描述模板

```markdown
## 变更类型
- [ ] 新功能
- [ ] Bug 修复
- [ ] 重构
- [ ] 文档更新

## 变更描述
<!-- 清晰描述此 PR 的目的和变更内容 -->

## 相关 Issue
Closes #123

## 测试
<!-- 描述如何测试这些变更 -->
- [ ] 单元测试通过
- [ ] 集成测试通过
- [ ] 手动测试完成

## 截图（如适用）
<!-- UI 变更请附截图 -->

## 检查清单
- [ ] 代码遵循项目规范
- [ ] 自我审查过代码
- [ ] 添加了必要的注释
- [ ] 文档已更新
```

### 审查要点

```bash
# 查看变更统计
git diff --stat develop..feature/branch

# 查看某个提交的详细内容
git show a1b2c3d

# 检查提交历史
git log --oneline --graph develop..feature/branch
```

---

## 第六部分：高级技巧

### Cherry-pick

```bash
# 选择性应用提交
git cherry-pick a1b2c3d

# 应用多个提交
git cherry-pick a1b2c3d b2c3d4e

# 只应用变更不提交
git cherry-pick -n a1b2c3d
```

### Bisect（二分查找 bug）

```bash
# 开始二分
git bisect start

# 标记当前版本有 bug
git bisect bad

# 标记已知正常的版本
git bisect good v1.0.0

# Git 会自动 checkout 中间版本，测试后标记
git bisect good  # 或 git bisect bad

# 找到后重置
git bisect reset
```

### 日志查询

```bash
# 搜索提交信息
git log --grep="fix" --oneline

# 搜索代码变更
git log -S "functionName" --oneline

# 查看文件历史
git log --follow -p -- path/to/file

# 查看某人的提交
git log --author="username" --oneline

# 时间范围
git log --since="2024-01-01" --until="2024-01-31"
```

---

## 审查清单

### 提交前检查
- [ ] 每个提交只包含一个逻辑变更
- [ ] 提交信息遵循 Conventional Commits 格式
- [ ] 代码可以编译通过
- [ ] 测试通过
- [ ] 没有包含调试代码或敏感信息

### PR 检查
- [ ] 分支名称规范
- [ ] 提交历史清晰（必要时 rebase/squash）
- [ ] PR 描述完整
- [ ] 已关联相关 Issue
- [ ] CI 检查通过


---

## ⚠️ 强制输出 Token 空间

> **重要规则**：使用此技能时，必须在每次重要输出前检查上下文空间。

### 输出规范

所有对话回复内容都要输出

### 输出格式

```
📊 剩余上下文空间: ~{百分比}%
```

### 告警与自动保存

**当剩余上下文空间 ≤ 30%（即已使用 ≥ 70%）时，必须执行：**

1. **立即暂停当前工作**
2. **保存工作进度**：创建 `.agent/workflows/checkpoint-{timestamp}.md`
3. **通知用户**：
   ```
   ⚠️ 上下文空间即将耗尽 (剩余 ~{百分比}%)
   📋 工作进度已保存至: .agent/workflows/checkpoint-{timestamp}.md
   请检查后决定是否继续或开启新对话
   ```
