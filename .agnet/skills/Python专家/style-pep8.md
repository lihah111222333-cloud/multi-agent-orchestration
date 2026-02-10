---
title: 遵循PEP 8风格指南
impact: MEDIUM
category: style
tags: PEP8, 代码风格, 格式化
---

# 遵循PEP 8风格指南

遵循 PEP 8 编码规范，保持代码风格一致。

## 关键规则

### 命名规范
```python
# 变量和函数：snake_case
user_name = "张三"
def calculate_total_price():
    ...

# 类：PascalCase
class UserProfile:
    ...

# 常量：UPPER_SNAKE_CASE
MAX_RETRY_COUNT = 3
DEFAULT_TIMEOUT = 30

# 私有属性：前缀下划线
class User:
    def __init__(self):
        self._internal_state = {}
```

### 缩进和空格
```python
# ✅ 4个空格缩进
if condition:
    do_something()

# ✅ 运算符两边加空格
x = a + b
result = value * 2

# ✅ 逗号后加空格
items = [1, 2, 3]
func(arg1, arg2)

# ❌ 括号内不加空格
spam(ham[1], {eggs: 2})  # ✅
spam( ham[ 1 ], { eggs: 2 } )  # ❌
```

### 行长度
```python
# 每行最多79字符（或团队约定的120字符）
# 长行使用括号换行
result = (
    first_value
    + second_value
    - third_value
)
```

### 导入顺序
```python
# 1. 标准库
import os
import sys

# 2. 第三方库
import requests
import pandas as pd

# 3. 本地模块
from myproject import utils
from myproject.models import User
```

## 最佳实践

- [ ] 使用 `black` 或 `ruff` 自动格式化
- [ ] 配置 linter（flake8、ruff）检查规范
- [ ] 团队统一配置 `.editorconfig`
