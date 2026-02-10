---
title: 使用列表推导
impact: HIGH
category: performance
tags: 列表推导, 性能, Pythonic
---

# 使用列表推导

优先使用列表推导、字典推导和生成器表达式，代替显式循环构建集合。

## 为什么重要

- 比等价的 for 循环快 10-30%
- 更简洁、更 Pythonic
- 意图更清晰
- 减少出错机会

## ❌ 错误

```python
# ❌ 手动循环构建列表
squares = []
for x in range(10):
    squares.append(x ** 2)

# ❌ 过滤也用循环
even_numbers = []
for n in numbers:
    if n % 2 == 0:
        even_numbers.append(n)
```

## ✅ 正确

```python
# ✅ 列表推导
squares = [x ** 2 for x in range(10)]

# ✅ 带条件的列表推导
even_numbers = [n for n in numbers if n % 2 == 0]

# ✅ 字典推导
name_lengths = {name: len(name) for name in names}

# ✅ 集合推导
unique_words = {word.lower() for word in text.split()}

# ✅ 大数据用生成器表达式（惰性求值，省内存）
total = sum(x ** 2 for x in range(1_000_000))
```

## 何时不使用推导

```python
# ❌ 推导过于复杂时，用普通循环更清晰
# 不好：
result = [transform(x) for group in data for x in group if predicate(x) and validate(x)]

# ✅ 好：拆成循环
result = []
for group in data:
    for x in group:
        if predicate(x) and validate(x):
            result.append(transform(x))
```

## 最佳实践

- [ ] 简单的映射和过滤使用推导
- [ ] 推导超过一行时考虑换用循环
- [ ] 大数据集用生成器表达式代替列表推导
- [ ] 不要为了副作用使用推导（用普通 for 循环）
