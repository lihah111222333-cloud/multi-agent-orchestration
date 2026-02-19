---
name: 品牌设计规范
description: 定义和应用品牌色彩、字体、视觉识别标准。确保所有设计产出保持一致的品牌形象和专业标准。
tags: [品牌, 设计, 视觉识别, 色彩, 字体, VI, 规范, 风格指南]
---

# 品牌设计规范

## 何时使用

在以下场景使用此技能：
- 创建品牌视觉识别系统
- 确保设计一致性
- 制作品牌素材
- 指导设计产出

---

## 第一部分：色彩系统

### 主色定义

```css
/* 主色 - 用于主要元素 */
--color-primary: #2563EB;      /* 品牌蓝 */
--color-primary-light: #3B82F6;
--color-primary-dark: #1D4ED8;

/* 辅助色 - 用于强调 */
--color-secondary: #10B981;    /* 成功绿 */
--color-accent: #F59E0B;       /* 警示橙 */
--color-error: #EF4444;        /* 错误红 */
```

### 中性色

```css
/* 中性色 - 用于文本和背景 */
--color-gray-900: #111827;     /* 主文本 */
--color-gray-700: #374151;     /* 次要文本 */
--color-gray-500: #6B7280;     /* 辅助文本 */
--color-gray-300: #D1D5DB;     /* 边框 */
--color-gray-100: #F3F4F6;     /* 背景 */
--color-white: #FFFFFF;        /* 白色 */
```

### 色彩使用规则

| 场景 | 推荐色彩 | 说明 |
|------|---------|------|
| 主按钮 | Primary | 核心行动按钮 |
| 成功状态 | Secondary | 完成、通过 |
| 警告提示 | Accent | 需要注意 |
| 错误提示 | Error | 失败、禁止 |
| 正文文本 | Gray-900 | 主要阅读内容 |
| 辅助文本 | Gray-500 | 次要信息 |

---

## 第二部分：字体系统

### 字体家族

```css
/* 标题字体 */
--font-heading: 'Poppins', 'PingFang SC', 'Microsoft YaHei', sans-serif;

/* 正文字体 */
--font-body: 'Inter', 'PingFang SC', 'Microsoft YaHei', sans-serif;

/* 代码字体 */
--font-mono: 'JetBrains Mono', 'Fira Code', monospace;
```

### 字号规范

| 级别 | 字号 | 行高 | 用途 |
|------|------|------|------|
| H1 | 36px | 1.2 | 页面标题 |
| H2 | 30px | 1.3 | 章节标题 |
| H3 | 24px | 1.4 | 子标题 |
| H4 | 20px | 1.4 | 小标题 |
| Body | 16px | 1.6 | 正文 |
| Small | 14px | 1.5 | 辅助文本 |
| Caption | 12px | 1.4 | 标注 |

### 字重

| 权重 | 数值 | 用途 |
|------|------|------|
| Regular | 400 | 正文 |
| Medium | 500 | 强调文本 |
| Semibold | 600 | 标题 |
| Bold | 700 | 重要标题 |

---

## 第三部分：间距系统

### 基础间距

```css
/* 4px 基础单位 */
--space-1: 4px;
--space-2: 8px;
--space-3: 12px;
--space-4: 16px;
--space-5: 20px;
--space-6: 24px;
--space-8: 32px;
--space-10: 40px;
--space-12: 48px;
--space-16: 64px;
```

### 使用场景

| 间距 | 用途 |
|------|------|
| space-1 | 图标与文字间距 |
| space-2 | 紧凑元素间距 |
| space-4 | 表单元素间距 |
| space-6 | 段落间距 |
| space-8 | 卡片内边距 |
| space-12 | 章节间距 |

---

## 第四部分：圆角规范

```css
--radius-sm: 4px;   /* 小元素：标签、徽章 */
--radius-md: 8px;   /* 中元素：按钮、输入框 */
--radius-lg: 12px;  /* 大元素：卡片 */
--radius-xl: 16px;  /* 特大：模态框 */
--radius-full: 9999px;  /* 圆形：头像 */
```

---

## 第五部分：阴影规范

```css
/* 轻微阴影 - 卡片悬浮态 */
--shadow-sm: 0 1px 2px rgba(0, 0, 0, 0.05);

/* 标准阴影 - 卡片 */
--shadow-md: 0 4px 6px -1px rgba(0, 0, 0, 0.1);

/* 明显阴影 - 下拉菜单 */
--shadow-lg: 0 10px 15px -3px rgba(0, 0, 0, 0.1);

/* 强阴影 - 模态框 */
--shadow-xl: 0 20px 25px -5px rgba(0, 0, 0, 0.1);
```

---

## 第六部分：Logo 使用规范

### 最小尺寸

- 数字端：最小宽度 80px
- 印刷品：最小宽度 20mm

### 安全区域

- Logo 周围保留最小 Logo 高度 1/4 的空白

### 禁止行为

- ❌ 拉伸变形
- ❌ 旋转
- ❌ 添加阴影或特效
- ❌ 在复杂背景上使用
- ❌ 改变颜色
- ❌ 添加边框或描边

---

## 第七部分：组件样式

### 按钮

```css
/* 主按钮 */
.btn-primary {
  background: var(--color-primary);
  color: white;
  padding: var(--space-3) var(--space-6);
  border-radius: var(--radius-md);
  font-weight: 500;
}

/* 次要按钮 */
.btn-secondary {
  background: transparent;
  color: var(--color-primary);
  border: 1px solid var(--color-primary);
}

/* 危险按钮 */
.btn-danger {
  background: var(--color-error);
  color: white;
}
```

### 卡片

```css
.card {
  background: var(--color-white);
  border-radius: var(--radius-lg);
  padding: var(--space-6);
  box-shadow: var(--shadow-md);
}
```

### 输入框

```css
.input {
  border: 1px solid var(--color-gray-300);
  border-radius: var(--radius-md);
  padding: var(--space-3) var(--space-4);
  font-size: 16px;
}

.input:focus {
  border-color: var(--color-primary);
  outline: none;
  box-shadow: 0 0 0 3px rgba(37, 99, 235, 0.1);
}
```

---

## 第八部分：图片规范

### 图片比例

| 用途 | 比例 | 说明 |
|------|------|------|
| Banner | 16:9 | 横幅图片 |
| 文章配图 | 3:2 | 博客文章 |
| 产品图 | 1:1 | 产品展示 |
| 头像 | 1:1 圆形 | 用户头像 |

### 图片风格

- 真实、自然的光线
- 高清晰度
- 与品牌色彩协调
- 统一的后期处理风格

---

## 检查清单

### 设计评审

- [ ] 颜色符合品牌规范
- [ ] 字体使用正确
- [ ] 间距一致
- [ ] Logo 使用规范
- [ ] 响应式适配良好
- [ ] 无障碍性（对比度足够）


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
