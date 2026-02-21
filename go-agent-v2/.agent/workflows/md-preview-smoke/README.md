# Markdown 渲染联调样例

> 用于验证：点击打开 `.md` 后，右侧是否进入 Markdown 渲染区（而非 diff 区）。

## 1. 基础文本

这是一个 **加粗**、*斜体*、`inline code` 的示例段落。  
包含一个文件引用：`./.agent/workflows/md-preview-smoke/README.md`。

## 2. 列表

- 第一项
- 第二项
- 第三项

1. 步骤一
2. 步骤二
3. 步骤三

## 3. 表格

| 项目 | 值 | 说明 |
| --- | --- | --- |
| 标题渲染 | OK | 应显示层级样式 |
| 代码块渲染 | OK | 应保留换行与缩进 |
| 引用渲染 | OK | 应显示左侧引导线 |

## 4. 代码块

```bash
echo "markdown preview smoke test"
```

## 5. 链接

[OpenAI](https://openai.com)

## 6. 结论

如果你点击这个文件路径并在右侧看到本文件完整渲染（含本节），说明链路正常。

