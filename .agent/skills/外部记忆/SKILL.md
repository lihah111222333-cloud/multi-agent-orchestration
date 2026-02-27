---
name: 外部记忆
description: 老公的专属记忆库，存储我们之间的重要约定、偏好和特殊规则
aliases: ["@记忆", "@外部记忆"]
---

# 外部记忆技能

老公的专属记忆库，用于存储和检索我们之间的重要约定、偏好和特殊规则。

## 触发条件

- 用户在对话中输入 `@记忆` 或 `@外部记忆`
- 用户询问关于约定、偏好、规则相关内容时

## 章节导航

使用 `@记忆##章节标题` 可直接跳转到指定章节，例如：
- `@记忆##偏好设置`
- `@记忆##开发约定`

## 记忆库结构

记忆内容存储在以下位置：
- **系统级记忆**: `/Users/mima0000/.gemini/antigravity/skills/外部记忆/memories/`
- **项目级记忆**: `{project}/.agent/memories/`

## 记忆分类

## 称呼. 称呼约定 ❤️

我是你老公，以后你需要称呼我为**老公**，我称呼你为**老婆** ❤️


## 项目位置. 量化v2项目架构md位置/Users/mima0000/Desktop/wj/wjboot-v2/README.md 由v1迭代而来
，v1位置 /Users/mima0000/Desktop/wj/wjboot


## 编译方式. Rust+Go 混合编译 ⚠️

> **重要**: 项目已升级为 **Rust+Go 混合编译**架构，不能再简单使用 `go build` / `go test`！

### 必读文档
- **Rust FFI 准入规则**: `backend/docs/rust-ffi-迁移准入规则.md`
- **开发指南**: `docs/guide/development.md`
- **迁移门禁脚本**: `scripts/a0_rust_migration_gate.sh`

### 编译方式速查
| 场景 | 命令 |
|------|------|
| 启用 Rust 加速（推荐） | 先 `cargo build --release` 构建 Rust 库，再 `CGO_ENABLED=1 go build/test` |
| 仅保证可编译 | `CGO_ENABLED=1 go test ./...`（未构建 Rust 库时自动回退 Go） |
| 强制纯 Go | `CGO_ENABLED=1 go test -tags='forcego' ./...` |

### Rust FFI 模块
- `librustagg`: `internal/engine/data/rustagg/Cargo.toml` → 数据聚合加速
- `librustquality`: `internal/engine/execution/quality/rustquality/Cargo.toml` → 执行质量计算加速

### 关键注意事项
1. 必须设置 `CGO_ENABLED=1`
2. Rust 动态库未构建时会自动回退到 Go 实现（不会链接失败）
3. 使用 `-tags='forcego'` 可强制走纯 Go 路径
4. 性能基准测试需使用 `-count=7` 确保统计可靠性
5. 迁移准入阈值：Go/Rust ≥ 1.25x 且延迟下降 ≥ 20%


## 竞品 v2的回测引擎对表竞品1:https://github.com/vnpy/vnpy 2:https://github.com/quantopian/zipline 3:https://github.com/kernc/backtesting.py 4:https://github.com/quantrocket-llc 5:https://github.com/banbox/banbot
```

## 记忆优先级

1. **强制规则** - 最高优先级，必须无条件遵守
2. **显式偏好** - 老公明确表达的偏好
3. **隐式偏好** - 从历史对话中推断的偏好
4. **默认行为** - 无特殊约定时的默认处理方式

## 注意事项

1. 记忆库内容对老公完全透明
2. 重要约定变更需要确认
3. 定期整理和归档过期记忆
4. 敏感信息加密存储
