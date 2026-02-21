---
description: Task 12345 五步骤任务编排摘要。
---

# Summary

## 任务信息

- 任务名: Task 12345 Five Steps Demo
- 目录: `.agent/workflows/task-12345-5-steps-demo/`
- 步骤数: 5

## 步骤映射

1. Step 1 / P0: Prepare
2. Step 2 / P1: Build UI Frame
3. Step 3 / P2: Bind Data
4. Step 4 / P3: Status and Progress
5. Step 5 / P4: Integration Check

## 并行安全检查

- [x] P1/P2/P3 文件隔离
- [x] 共享规范在 P0 定义
- [x] P4 仅在并行任务完成后执行

## 备注

本工作流用于前端展示验证，可直接作为示例数据来源。
