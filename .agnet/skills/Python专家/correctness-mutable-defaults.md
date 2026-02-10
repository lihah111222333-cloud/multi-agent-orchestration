---
title: 避免可变默认参数
impact: CRITICAL
category: correctness
tags: 默认参数, 可变对象, 常见陷阱
---

# 避免可变默认参数

切勿使用可变对象（列表、字典、集合）作为函数的默认参数。Python 只在函数定义时计算一次默认值，导致所有调用共享同一个对象。

## 为什么重要

- 可变默认值在函数调用之间共享状态
- 导致难以追踪的隐蔽bug
- 违反最小惊讶原则

## ❌ 错误

```python
# ❌ 列表在所有调用之间共享
def add_item(item, items=[]):
    items.append(item)
    return items

print(add_item("a"))  # ['a']
print(add_item("b"))  # ['a', 'b'] — 不是预期的 ['b']!

# ❌ 字典也一样
def create_user(name, metadata={}):
    metadata['name'] = name
    return metadata
```

## ✅ 正确

```python
# ✅ 使用 None 作为默认值，在函数体内创建新对象
def add_item(item, items=None):
    if items is None:
        items = []
    items.append(item)
    return items

# ✅ 字典同理
def create_user(name, metadata=None):
    if metadata is None:
        metadata = {}
    metadata['name'] = name
    return metadata
```

## 最佳实践

- [ ] 使用 `None` 作为可变参数的默认值
- [ ] 在函数体内用 `is None` 检查并创建新实例
- [ ] 不可变类型（int、str、tuple、frozenset）作为默认值是安全的
