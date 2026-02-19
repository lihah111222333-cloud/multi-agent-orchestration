---
name: Excel电子表格
description: Excel/XLSX 文件操作，包括公式、图表、数据转换和财务模型规范。使用 openpyxl 和 pandas 进行数据分析。
tags: [excel, xlsx, 表格, 数据分析, pandas, openpyxl, 财务, 公式]
---

# Excel 电子表格处理

## 何时使用

在以下场景使用此技能：
- 创建或编辑 Excel 文件
- 数据分析和可视化
- 财务模型构建
- 报表自动生成
- 数据转换和清洗

---

## 重要原则

### ⚠️ 关键：使用公式，而非硬编码值

**始终使用 Excel 公式，而不是在 Python 中计算后硬编码结果。** 这确保电子表格保持动态和可更新。

```python
# ❌ 错误 - 硬编码计算值
ws['C2'] = 150  # 假设 A2=100, B2=50

# ✅ 正确 - 使用 Excel 公式
ws['C2'] = '=A2+B2'
```

---

## 第一部分：读取和分析数据

### 使用 pandas 进行数据分析

```python
import pandas as pd

# 读取 Excel
df = pd.read_excel('file.xlsx')  # 默认：第一个工作表
all_sheets = pd.read_excel('file.xlsx', sheet_name=None)  # 所有工作表返回字典

# 分析
df.head()      # 数据预览
df.info()      # 列信息
df.describe()  # 统计信息

# 写入 Excel
df.to_excel('output.xlsx', index=False)
```

### 使用 openpyxl 读取

```python
from openpyxl import load_workbook

wb = load_workbook('file.xlsx')
ws = wb.active

# 读取单元格
value = ws['A1'].value
print(f"A1 的值: {value}")

# 遍历行
for row in ws.iter_rows(min_row=1, max_row=10, values_only=True):
    print(row)
```

---

## 第二部分：创建和编辑 Excel

### 创建新工作簿

```python
from openpyxl import Workbook
from openpyxl.styles import Font, Alignment, Border, Side

wb = Workbook()
ws = wb.active
ws.title = "销售数据"

# 添加标题行
headers = ['产品', '数量', '单价', '总价']
for col, header in enumerate(headers, 1):
    cell = ws.cell(row=1, column=col, value=header)
    cell.font = Font(bold=True)
    cell.alignment = Alignment(horizontal='center')

# 添加数据和公式
data = [
    ['产品A', 10, 100],
    ['产品B', 20, 150],
    ['产品C', 15, 200],
]

for row_idx, row_data in enumerate(data, 2):
    for col_idx, value in enumerate(row_data, 1):
        ws.cell(row=row_idx, column=col_idx, value=value)
    # 总价公式
    ws.cell(row=row_idx, column=4, value=f'=B{row_idx}*C{row_idx}')

# 汇总行
ws.cell(row=5, column=3, value='合计:')
ws.cell(row=5, column=4, value='=SUM(D2:D4)')

wb.save('sales.xlsx')
```

### 编辑现有文件

```python
from openpyxl import load_workbook

wb = load_workbook('existing.xlsx')
ws = wb.active

# 修改单元格
ws['A1'] = '更新后的值'

# 插入行
ws.insert_rows(3)

# 删除行
ws.delete_rows(5, 2)  # 从第5行开始删除2行

wb.save('modified.xlsx')
```

---

## 第三部分：财务模型规范

### 颜色编码标准

| 颜色 | 用途 | 十六进制 |
|------|------|---------|
| 蓝色 | 输入/假设 | #0066CC |
| 黑色 | 公式/计算 | #000000 |
| 绿色 | 链接到其他工作表 | #008000 |
| 红色 | 警告/错误 | #FF0000 |

### 数字格式标准

```python
from openpyxl.styles import numbers

# 货币格式
ws['A1'].number_format = '¥#,##0.00'

# 百分比格式
ws['B1'].number_format = '0.00%'

# 日期格式
ws['C1'].number_format = 'YYYY-MM-DD'

# 千位分隔符
ws['D1'].number_format = '#,##0'
```

### 公式构建规则

1. **避免硬编码** - 所有可变值应放在输入区域
2. **使用命名区域** - 提高公式可读性
3. **分步计算** - 复杂公式拆分为多个单元格
4. **添加备注** - 解释复杂计算逻辑

---

## 第四部分：图表创建

```python
from openpyxl import Workbook
from openpyxl.chart import BarChart, Reference

wb = Workbook()
ws = wb.active

# 添加数据
data = [
    ['月份', '销售额'],
    ['1月', 100],
    ['2月', 120],
    ['3月', 150],
    ['4月', 180],
]
for row in data:
    ws.append(row)

# 创建图表
chart = BarChart()
chart.title = "月度销售额"
chart.x_axis.title = "月份"
chart.y_axis.title = "销售额"

# 数据引用
data_ref = Reference(ws, min_col=2, min_row=1, max_row=5)
cats_ref = Reference(ws, min_col=1, min_row=2, max_row=5)

chart.add_data(data_ref, titles_from_data=True)
chart.set_categories(cats_ref)

# 添加到工作表
ws.add_chart(chart, "D2")

wb.save('chart.xlsx')
```

---

## 第五部分：公式重算

### 使用 LibreOffice 重算

```bash
# 假设已安装 LibreOffice
python recalc.py input.xlsx output.xlsx
```

### 公式验证检查表

- [ ] 所有计算单元格包含公式（非硬编码值）
- [ ] 汇总公式正确引用数据范围
- [ ] 循环引用已处理
- [ ] 公式结果符合预期

---

## 依赖项

| 库 | 安装命令 | 用途 |
|---|---------|------|
| openpyxl | `pip install openpyxl` | Excel 读写 |
| pandas | `pip install pandas` | 数据分析 |
| xlrd | `pip install xlrd` | 读取 .xls |
| xlsxwriter | `pip install xlsxwriter` | 高级写入 |

---

## 最佳实践

### 库选择

| 场景 | 推荐库 |
|------|-------|
| 数据分析 | pandas |
| 复杂格式 | openpyxl |
| 大文件写入 | xlsxwriter |
| 读取旧格式 (.xls) | xlrd |

### 性能提示

- 批量写入比逐单元格写入快
- 使用 `write_only` 模式处理大文件
- 避免在循环中频繁保存


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
