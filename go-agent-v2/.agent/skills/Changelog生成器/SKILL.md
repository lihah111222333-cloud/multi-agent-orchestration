---
name: "Changelog生成器"
description: "从 Git 提交历史自动生成用户友好的更新日志。支持 Conventional Commits 格式，将技术提交转换为客户可读的发布说明。"
summary: "从 Git 提交历史自动生成用户友好的更新日志。支持 Conventional Commits 格式，将技术提交转换为客户可读的发布说明。"
trigger_words: ["*"]
force_words: ["git历史"]
---

# Changelog 生成器

## 何时使用

在以下场景使用此技能：
- 发布新版本前生成更新日志
- 总结 Sprint 或迭代的变更
- 为用户撰写发布说明
- 自动化 CI/CD 发布流程

---

## 核心功能

1. **分析 Git 历史** - 解析提交信息
2. **分类变更** - 按功能、修复、改进等分类
3. **用户友好转换** - 技术提交 → 客户语言
4. **多格式输出** - Markdown、JSON、HTML

---

## 第一部分：基础用法

### 生成简单 Changelog

```bash
# 从上次标签到现在的所有提交
git log $(git describe --tags --abbrev=0)..HEAD --oneline
```

### 解析 Conventional Commits

```bash
# 筛选特定类型
git log --oneline --grep="^feat:" | head -20
git log --oneline --grep="^fix:" | head -20
git log --oneline --grep="^docs:" | head -20
```

---

## 第二部分：自动生成脚本

### Python 实现

```python
import subprocess
import re
from datetime import datetime
from collections import defaultdict

def generate_changelog(from_tag=None, to_ref='HEAD'):
    """生成结构化的更新日志"""
    
    # 获取提交
    if from_tag:
        cmd = f'git log {from_tag}..{to_ref} --format="%H|%s|%an|%ad" --date=short'
    else:
        cmd = f'git log {to_ref} -50 --format="%H|%s|%an|%ad" --date=short'
    
    result = subprocess.run(cmd, shell=True, capture_output=True, text=True)
    commits = result.stdout.strip().split('\n')
    
    # 分类
    categories = defaultdict(list)
    type_map = {
        'feat': '✨ 新功能',
        'fix': '🐛 问题修复',
        'docs': '📚 文档更新',
        'style': '💄 样式调整',
        'refactor': '♻️ 代码重构',
        'perf': '⚡ 性能优化',
        'test': '✅ 测试相关',
        'chore': '🔧 构建/工具',
    }
    
    for commit in commits:
        if not commit.strip():
            continue
        parts = commit.split('|')
        if len(parts) < 4:
            continue
        hash_id, subject, author, date = parts[0], parts[1], parts[2], parts[3]
        
        # 解析 Conventional Commit
        match = re.match(r'^(\w+)(?:\(([^)]+)\))?: (.+)$', subject)
        if match:
            commit_type, scope, description = match.groups()
            category = type_map.get(commit_type, '🔹 其他')
            categories[category].append({
                'description': description,
                'scope': scope,
                'author': author,
                'date': date,
                'hash': hash_id[:7]
            })
        else:
            categories['🔹 其他'].append({
                'description': subject,
                'author': author,
                'date': date,
                'hash': hash_id[:7]
            })
    
    return categories

def format_changelog(categories, version='未发布'):
    """格式化输出 Markdown"""
    lines = []
    lines.append(f"# 更新日志")
    lines.append(f"\n## [{version}] - {datetime.now().strftime('%Y-%m-%d')}\n")
    
    for category, items in categories.items():
        if items:
            lines.append(f"### {category}\n")
            for item in items:
                scope = f"**{item['scope']}**: " if item.get('scope') else ""
                lines.append(f"- {scope}{item['description']} ({item['hash']})")
            lines.append("")
    
    return '\n'.join(lines)

# 使用示例
if __name__ == '__main__':
    categories = generate_changelog('v1.0.0')
    changelog = format_changelog(categories, 'v1.1.0')
    print(changelog)
    
    # 保存到文件
    with open('CHANGELOG.md', 'w', encoding='utf-8') as f:
        f.write(changelog)
```

---

## 第三部分：Conventional Commits 规范

### 提交类型

| 类型 | 描述 | 对应用户语言 |
|------|------|-------------|
| feat | 新功能 | 新增了 XXX 功能 |
| fix | Bug 修复 | 修复了 XXX 问题 |
| docs | 文档更新 | 更新了 XXX 文档 |
| style | 代码格式 | (通常不进入用户日志) |
| refactor | 重构 | 优化了 XXX 体验 |
| perf | 性能优化 | 提升了 XXX 性能 |
| test | 测试 | (通常不进入用户日志) |
| chore | 构建/工具 | (通常不进入用户日志) |

### 提交格式

```
<类型>(<范围>): <描述>

[可选正文]

[可选脚注]
```

**示例：**
```
feat(用户中心): 新增头像裁剪功能

支持用户上传图片后进行裁剪和旋转操作

Closes #123
```

---

## 第四部分：用户友好转换规则

### 转换示例

| 技术提交 | 用户友好版本 |
|---------|-------------|
| `fix(api): resolve null pointer exception in user service` | 修复了用户信息加载失败的问题 |
| `feat(dashboard): add export to CSV functionality` | 新增数据导出为 CSV 功能 |
| `perf(query): optimize database query with index` | 提升了数据查询速度 |
| `fix(ui): correct button alignment on mobile` | 优化了移动端按钮显示效果 |

### 转换指南

1. **移除技术术语** - API、null pointer、index 等
2. **用户视角描述** - 他们能做什么、体验如何改善
3. **保持简洁** - 一句话说明核心变更
4. **使用动词开头** - 新增、修复、优化、提升

---

## 第五部分：输出格式

### Markdown 格式

```markdown
# 更新日志

## [1.2.0] - 2025-01-20

### ✨ 新功能

- **用户中心**: 新增头像裁剪功能 (a1b2c3d)
- **报表**: 支持导出为 Excel 格式 (e4f5g6h)

### 🐛 问题修复

- 修复了登录页面验证码不显示的问题 (i7j8k9l)
- 修复了数据导出时间格式错误 (m0n1o2p)

### ⚡ 性能优化

- 提升了首页加载速度约 40% (q3r4s5t)
```

### JSON 格式

```json
{
  "version": "1.2.0",
  "date": "2025-01-20",
  "changes": {
    "features": [
      {
        "description": "新增头像裁剪功能",
        "scope": "用户中心",
        "hash": "a1b2c3d"
      }
    ],
    "fixes": [
      {
        "description": "修复登录页面验证码不显示的问题",
        "hash": "i7j8k9l"
      }
    ]
  }
}
```

---

## 集成到 CI/CD

### GitHub Actions 示例

```yaml
name: Generate Changelog

on:
  push:
    tags:
      - 'v*'

jobs:
  changelog:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3
        with:
          fetch-depth: 0
      
      - name: Generate Changelog
        run: python scripts/generate_changelog.py > RELEASE_NOTES.md
      
      - name: Create Release
        uses: actions/create-release@v1
        with:
          tag_name: ${{ github.ref }}
          body_path: RELEASE_NOTES.md
```


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