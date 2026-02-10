---
title: 编写文档字符串
impact: MEDIUM
category: style
tags: docstring, 文档, 注释
---

# 编写文档字符串

为所有公共模块、类、函数和方法编写文档字符串。

## 格式（Google风格）

```python
def fetch_user(user_id: int, include_posts: bool = False) -> User:
    """根据ID获取用户信息。

    从数据库查询用户，可选择性地包含用户的帖子。

    Args:
        user_id: 用户的唯一标识符。
        include_posts: 是否同时获取用户的帖子，默认为False。

    Returns:
        包含用户信息的User对象。如果include_posts为True，
        User.posts将包含帖子列表。

    Raises:
        UserNotFoundError: 用户ID不存在时。
        DatabaseError: 数据库连接失败时。

    Example:
        >>> user = fetch_user(42)
        >>> print(user.name)
        '张三'
    """
```

## 类的文档字符串

```python
class TaskQueue:
    """异步任务队列，支持优先级和重试。

    管理后台任务的提交、执行和状态跟踪。
    支持配置最大并发数、重试策略和超时。

    Attributes:
        max_workers: 最大并发工作线程数。
        retry_limit: 任务失败后的最大重试次数。
    """
```

## 最佳实践

- [ ] 所有公共函数和类都要有文档字符串
- [ ] 第一行是简明摘要（一句话）
- [ ] 包含 Args、Returns、Raises 部分
- [ ] 复杂逻辑添加 Example 部分
- [ ] 私有方法如果逻辑复杂也应添加文档
