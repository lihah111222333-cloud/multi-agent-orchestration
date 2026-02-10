---
title: 使用上下文管理器
impact: HIGH
category: performance
tags: with语句, 资源管理, 上下文管理器
---

# 使用上下文管理器

使用 `with` 语句和上下文管理器来管理资源（文件、连接、锁），确保正确清理。

## 为什么重要

- 保证资源被正确释放，即使发生异常
- 防止资源泄漏（文件句柄、数据库连接）
- 代码更简洁、更安全

## ❌ 错误

```python
# ❌ 手动管理文件，异常时文件不会关闭
f = open("data.txt")
data = f.read()
process(data)
f.close()  # 如果 process() 抛异常，这行永远不会执行
```

## ✅ 正确

```python
# ✅ with 语句自动管理资源
with open("data.txt") as f:
    data = f.read()
    process(data)
# 文件自动关闭，即使 process() 抛异常

# ✅ 多个资源同时管理
with open("input.txt") as fin, open("output.txt", "w") as fout:
    fout.write(fin.read().upper())
```

## 自定义上下文管理器

```python
from contextlib import contextmanager

@contextmanager
def timer(label: str):
    import time
    start = time.perf_counter()
    try:
        yield
    finally:
        elapsed = time.perf_counter() - start
        print(f"{label}: {elapsed:.3f}秒")

with timer("数据处理"):
    process_data()
```

## 最佳实践

- [ ] 所有文件操作使用 `with open(...)`
- [ ] 数据库连接和游标使用 `with`
- [ ] 锁使用 `with lock:`
- [ ] 需要自定义清理逻辑时创建上下文管理器
