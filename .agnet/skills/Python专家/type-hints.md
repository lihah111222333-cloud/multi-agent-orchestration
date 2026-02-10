---
title: 使用类型提示
impact: HIGH
category: type-safety
tags: 类型提示, typing, 类型注解
---

# 使用类型提示

为所有函数签名、返回值和复杂变量添加类型提示，提高代码可读性和IDE支持。

## 为什么重要

- 提高代码可读性和自文档化
- IDE 自动补全和错误检测
- 静态分析工具（mypy）可提前发现bug
- 减少运行时错误

## ❌ 错误

```python
# ❌ 没有类型提示，难以理解接口
def process_data(data, threshold, include_outliers):
    results = []
    for item in data:
        if item > threshold:
            results.append(item)
    return results
```

## ✅ 正确

```python
# ✅ 类型提示清楚表明接口契约
from typing import List, Optional, Sequence

def process_data(
    data: Sequence[float],
    threshold: float,
    include_outliers: bool = False
) -> List[float]:
    results: List[float] = []
    for item in data:
        if item > threshold:
            results.append(item)
    return results
```

## 常用类型

```python
from typing import (
    List, Dict, Set, Tuple,       # 容器类型
    Optional, Union,               # 可选和联合类型
    Callable, Iterator, Generator, # 函数和迭代器
    TypeVar, Generic,              # 泛型
    Any,                           # 任意类型（谨慎使用）
)

# Python 3.10+ 可使用内置类型
def func(items: list[int], mapping: dict[str, float]) -> tuple[int, ...]:
    ...
```

## 最佳实践

- [ ] 所有公共函数都添加类型提示
- [ ] 使用 `Optional[X]` 代替 `X | None`（兼容 3.9 以下）
- [ ] 避免过度使用 `Any`
- [ ] 复杂类型用 `TypeAlias` 定义别名
