---
name: DOCX文档处理
description: 创建、编辑、分析 Word 文档，支持修订追踪、批注、格式保留和文本提取。当需要处理专业文档(.docx 文件)时使用此技能。
tags: [docx, word, 文档, 办公, 修订, 批注, pandoc, docx-js]
---

# DOCX 文档处理

## 何时使用

在以下场景使用此技能：
- 创建新的 Word 文档
- 编辑或修改现有文档
- 提取文档内容和元数据
- 处理修订追踪和批注
- 文档格式转换

---

## 工作流决策树

```
需要做什么？
├── 读取/分析内容 → 使用"文本提取"或"原始 XML 访问"
├── 创建新文档 → 使用"创建新文档"工作流
└── 编辑现有文档
    ├── 简单修改 → 使用"基础 OOXML 编辑"
    └── 专业/法律文档 → 使用"修订追踪工作流"（推荐）
```

---

## 第一部分：读取和分析内容

### 文本提取

使用 pandoc 将文档转换为 markdown：

```bash
# 转换文档为 markdown，保留修订
pandoc --track-changes=all 文件路径.docx -o 输出.md

# 选项：--track-changes=accept/reject/all
```

### 原始 XML 访问

需要访问以下内容时使用原始 XML：
- 批注
- 复杂格式
- 文档结构
- 嵌入媒体
- 元数据

#### 解包文件

```bash
python ooxml/scripts/unpack.py <office_file> <output_directory>
```

#### 关键文件结构

| 文件路径 | 用途 |
|---------|------|
| `word/document.xml` | 主文档内容 |
| `word/comments.xml` | 批注（在 document.xml 中引用）|
| `word/media/` | 嵌入的图片和媒体文件 |

> **修订标记**：使用 `<w:ins>` (插入) 和 `<w:del>` (删除) 标签

---

## 第二部分：创建新文档

使用 **docx-js** 创建新的 Word 文档：

### 工作流

1. 创建 JavaScript/TypeScript 文件
2. 使用 Document, Paragraph, TextRun 组件
3. 使用 Packer.toBuffer() 导出为 .docx

### 示例代码

```javascript
const { Document, Packer, Paragraph, TextRun } = require("docx");
const fs = require("fs");

const doc = new Document({
  sections: [{
    properties: {},
    children: [
      new Paragraph({
        children: [
          new TextRun({ text: "标题", bold: true, size: 48 }),
        ],
      }),
      new Paragraph({
        children: [
          new TextRun("这是正文内容。"),
        ],
      }),
    ],
  }],
});

Packer.toBuffer(doc).then((buffer) => {
  fs.writeFileSync("我的文档.docx", buffer);
});
```

---

## 第三部分：编辑现有文档

### 基础工作流

1. 解包文档：`python ooxml/scripts/unpack.py <file.docx> <dir>`
2. 使用 Document 库创建并运行 Python 脚本
3. 打包最终文档：`python ooxml/scripts/pack.py <dir> <output.docx>`

### 修订追踪工作流（推荐用于专业文档）

#### 原则：最小化、精确编辑

只标记实际更改的文本：

```python
# ❌ 错误 - 替换整句
'<w:del><w:r><w:delText>期限为30天。</w:delText></w:r></w:del>'
'<w:ins><w:r><w:t>期限为60天。</w:t></w:r></w:ins>'

# ✅ 正确 - 只标记更改部分
'<w:r><w:t>期限为</w:t></w:r>'
'<w:del><w:r><w:delText>30</w:delText></w:r></w:del>'
'<w:ins><w:r><w:t>60</w:t></w:r></w:ins>'
'<w:r><w:t>天。</w:t></w:r>'
```

#### 完整工作流

1. **获取 markdown 表示**
   ```bash
   pandoc --track-changes=all 文件.docx -o current.md
   ```

2. **识别并分组更改** - 将相关更改分批（每批3-10个）

3. **解包文档**
   ```bash
   python ooxml/scripts/unpack.py <file.docx> <dir>
   ```

4. **分批实现更改** - 使用 `get_node` 查找节点，实现更改，然后 `doc.save()`

5. **打包文档**
   ```bash
   python ooxml/scripts/pack.py unpacked reviewed-document.docx
   ```

6. **最终验证**
   ```bash
   pandoc --track-changes=all reviewed-document.docx -o verification.md
   ```

---

## 第四部分：文档转图片

将 Word 文档转换为图片进行可视化分析：

```bash
# 步骤1：DOCX 转 PDF
soffice --headless --convert-to pdf document.docx

# 步骤2：PDF 转 JPEG
pdftoppm -jpeg -r 150 document.pdf page
# 生成：page-1.jpg, page-2.jpg, ...
```

**选项说明：**
- `-r 150`：设置分辨率为 150 DPI
- `-jpeg`：输出 JPEG 格式（可用 `-png` 替换）
- `-f N`：起始页码
- `-l N`：结束页码

---

## 依赖项

| 工具 | 安装命令 | 用途 |
|-----|---------|------|
| pandoc | `brew install pandoc` | 文本提取 |
| docx | `npm install -g docx` | 创建新文档 |
| LibreOffice | `brew install libreoffice` | PDF 转换 |
| Poppler | `brew install poppler` | PDF 转图片 |
| defusedxml | `pip install defusedxml` | 安全 XML 解析 |

---

## 代码风格指南

- 编写简洁代码
- 避免冗长的变量名
- 避免不必要的 print 语句


---

## ⚠️ 强制输出 Token 空间

> **重要规则**：使用此技能时，必须在每次重要输出前检查上下文空间。

### 输出规范

所有对话回复内容都要输出

### 输出格式

```
📊 剩余上下文空间: ~{百分比}%
```

### 告警与自动保存

**当剩余上下文空间 ≤ 30%（即已使用 ≥ 70%）时，必须执行：**

1. **立即暂停当前工作**
2. **保存工作进度**：创建 `.agent/workflows/checkpoint-{timestamp}.md`
3. **通知用户**：
   ```
   ⚠️ 上下文空间即将耗尽 (剩余 ~{百分比}%)
   📋 工作进度已保存至: .agent/workflows/checkpoint-{timestamp}.md
   请检查后决定是否继续或开启新对话
   ```
