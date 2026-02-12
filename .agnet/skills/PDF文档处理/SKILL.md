---
name: PDF文档处理
description: 提取文本、表格、元数据，合并、拆分、注释 PDF 文件。支持 Python 库和命令行工具处理 PDF。
tags: [pdf, 文档, pypdf, pdfplumber, reportlab, 办公, 提取, 合并]
---

# PDF 文档处理

## 何时使用

在以下场景使用此技能：
- 提取 PDF 文本和表格
- 合并或拆分 PDF 文件
- 添加水印或注释
- 创建新的 PDF 文档
- 处理表单填写
- 提取元数据

---

## 快速开始

```python
from pypdf import PdfReader, PdfWriter

# 读取 PDF
reader = PdfReader("document.pdf")
print(f"页数: {len(reader.pages)}")

# 提取文本
text = ""
for page in reader.pages:
    text += page.extract_text()
```

---

## 第一部分：Python 库

### pypdf - 基础操作

#### 合并 PDF

```python
from pypdf import PdfWriter, PdfReader

writer = PdfWriter()
for pdf_file in ["doc1.pdf", "doc2.pdf", "doc3.pdf"]:
    reader = PdfReader(pdf_file)
    for page in reader.pages:
        writer.add_page(page)

with open("merged.pdf", "wb") as output:
    writer.write(output)
```

#### 拆分 PDF

```python
reader = PdfReader("input.pdf")
for i, page in enumerate(reader.pages):
    writer = PdfWriter()
    writer.add_page(page)
    with open(f"page_{i+1}.pdf", "wb") as output:
        writer.write(output)
```

#### 提取元数据

```python
reader = PdfReader("document.pdf")
meta = reader.metadata
print(f"标题: {meta.title}")
print(f"作者: {meta.author}")
print(f"主题: {meta.subject}")
print(f"创建者: {meta.creator}")
```

#### 旋转页面

```python
reader = PdfReader("input.pdf")
writer = PdfWriter()

page = reader.pages[0]
page.rotate(90)  # 顺时针旋转 90 度
writer.add_page(page)

with open("rotated.pdf", "wb") as output:
    writer.write(output)
```

---

### pdfplumber - 文本和表格提取

#### 提取文本（保留布局）

```python
import pdfplumber

with pdfplumber.open("document.pdf") as pdf:
    for page in pdf.pages:
        text = page.extract_text()
        print(text)
```

#### 提取表格

```python
with pdfplumber.open("document.pdf") as pdf:
    for i, page in enumerate(pdf.pages):
        tables = page.extract_tables()
        for j, table in enumerate(tables):
            print(f"第 {i+1} 页 表格 {j+1}:")
            for row in table:
                print(row)
```

#### 高级表格提取（转 Excel）

```python
import pandas as pd
import pdfplumber

with pdfplumber.open("document.pdf") as pdf:
    all_tables = []
    for page in pdf.pages:
        tables = page.extract_tables()
        for table in tables:
            if table:  # 检查表格非空
                df = pd.DataFrame(table[1:], columns=table[0])
                all_tables.append(df)

# 合并所有表格
if all_tables:
    combined_df = pd.concat(all_tables, ignore_index=True)
    combined_df.to_excel("extracted_tables.xlsx", index=False)
```

---

### reportlab - 创建 PDF

#### 基础 PDF 创建

```python
from reportlab.lib.pagesizes import letter
from reportlab.pdfgen import canvas

c = canvas.Canvas("hello.pdf", pagesize=letter)
width, height = letter

# 添加文本
c.drawString(100, height - 100, "你好，世界！")
c.drawString(100, height - 120, "这是用 reportlab 创建的 PDF")

# 添加线条
c.line(100, height - 140, 400, height - 140)

# 保存
c.save()
```

#### 创建多页 PDF

```python
from reportlab.lib.pagesizes import letter
from reportlab.platypus import SimpleDocTemplate, Paragraph, Spacer, PageBreak
from reportlab.lib.styles import getSampleStyleSheet

doc = SimpleDocTemplate("report.pdf", pagesize=letter)
styles = getSampleStyleSheet()
story = []

# 添加内容
title = Paragraph("报告标题", styles['Title'])
story.append(title)
story.append(Spacer(1, 12))

body = Paragraph("这是报告正文内容。" * 20, styles['Normal'])
story.append(body)
story.append(PageBreak())

# 第 2 页
story.append(Paragraph("第二页", styles['Heading1']))
story.append(Paragraph("第二页内容", styles['Normal']))

# 构建 PDF
doc.build(story)
```

---

## 第二部分：命令行工具

### pdftotext (poppler-utils)

```bash
# 提取文本
pdftotext input.pdf output.txt

# 保留布局提取
pdftotext -layout input.pdf output.txt

# 提取指定页
pdftotext -f 1 -l 5 input.pdf output.txt  # 第 1-5 页
```

### qpdf

```bash
# 合并 PDF
qpdf --empty --pages file1.pdf file2.pdf -- merged.pdf

# 拆分页面
qpdf input.pdf --pages . 1-5 -- pages1-5.pdf
qpdf input.pdf --pages . 6-10 -- pages6-10.pdf

# 旋转页面
qpdf input.pdf output.pdf --rotate=+90:1  # 第1页旋转90度

# 移除密码
qpdf --password=mypassword --decrypt encrypted.pdf decrypted.pdf
```

### pdftk

```bash
# 合并
pdftk file1.pdf file2.pdf cat output merged.pdf

# 拆分
pdftk input.pdf burst

# 旋转
pdftk input.pdf rotate 1east output rotated.pdf
```

---

## 第三部分：常见任务

### 从扫描件 PDF 提取文本 (OCR)

```bash
# 使用 tesseract
pdftoppm -png input.pdf page
tesseract page-1.png output -l chi_sim  # 中文简体
```

### 添加水印

```python
from pypdf import PdfReader, PdfWriter

reader = PdfReader("input.pdf")
watermark_reader = PdfReader("watermark.pdf")
watermark_page = watermark_reader.pages[0]

writer = PdfWriter()
for page in reader.pages:
    page.merge_page(watermark_page)
    writer.add_page(page)

with open("watermarked.pdf", "wb") as output:
    writer.write(output)
```

### 密码保护

```python
from pypdf import PdfWriter

writer = PdfWriter()
writer.append("input.pdf")
writer.encrypt("user_password", "owner_password")

with open("protected.pdf", "wb") as output:
    writer.write(output)
```

---

## 依赖项

| 库/工具 | 安装命令 | 用途 |
|--------|---------|------|
| pypdf | `pip install pypdf` | 基础操作 |
| pdfplumber | `pip install pdfplumber` | 文本/表格提取 |
| reportlab | `pip install reportlab` | 创建 PDF |
| poppler | `brew install poppler` | 命令行工具 |
| qpdf | `brew install qpdf` | 合并/拆分 |
| tesseract | `brew install tesseract` | OCR |

---

## 快速参考

| 任务 | 推荐工具 |
|------|---------|
| 提取文本 | pdfplumber / pdftotext |
| 提取表格 | pdfplumber |
| 合并/拆分 | pypdf / qpdf |
| 创建 PDF | reportlab |
| 表单填写 | pypdf |
| OCR | tesseract |


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
