---
title: 使用数据类
impact: HIGH
category: type-safety
tags: dataclass, 数据结构, 类型安全
---

# 使用数据类

优先使用 `dataclass` 或 `NamedTuple` 代替普通字典和元组来表示结构化数据。

## 为什么重要

- 类型安全，IDE可检查属性名
- 自动生成 `__init__`、`__repr__`、`__eq__`
- 代码更清晰，比字典更不易出错
- 支持默认值和不可变性

## ❌ 错误

```python
# ❌ 字典容易拼错键名，没有类型检查
user = {"name": "张三", "age": 25, "email": "zhang@example.com"}
print(user["naem"])  # 运行时 KeyError，拼写错误难以发现
```

## ✅ 正确

```python
# ✅ dataclass 提供类型安全和清晰接口
from dataclasses import dataclass, field
from typing import List, Optional

@dataclass
class User:
    name: str
    age: int
    email: str
    tags: List[str] = field(default_factory=list)
    bio: Optional[str] = None

user = User(name="张三", age=25, email="zhang@example.com")
print(user.name)  # IDE自动补全，拼错会被IDE标记

# ✅ 不可变数据类
@dataclass(frozen=True)
class Point:
    x: float
    y: float
```

## 何时选择什么

| 场景 | 推荐 |
|------|------|
| 简单数据容器 | `dataclass` |
| 不可变数据 | `dataclass(frozen=True)` 或 `NamedTuple` |
| 需要验证 | `pydantic.BaseModel` |
| 配置对象 | `dataclass` |
| API响应 | `pydantic.BaseModel` |

## 最佳实践

- [ ] 用 `dataclass` 代替字典存储结构化数据
- [ ] 需要不可变时使用 `frozen=True`
- [ ] 可变默认值使用 `field(default_factory=...)`
- [ ] 需要数据验证时考虑 pydantic
