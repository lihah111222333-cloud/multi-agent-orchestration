---
name: TailwindCSS 样式规范
description: TailwindCSS v4 配置与最佳实践指南，涵盖响应式设计、主题定制、组件样式和性能优化。适用于美化 UI 和样式开发。
tags: [tailwindcss, css, styling, responsive, theme, 样式设计, TailwindCSS, 响应式, 主题, UI美化]
---

# TailwindCSS 样式规范

适用于 TailwindCSS v4.x 的现代样式开发规范。

## 何时使用

在以下场景使用此技能：

- 编写组件样式
- 实现响应式布局
- 定制主题和设计系统
- 优化样式性能
- 处理暗色模式

---

## 第一部分：基础规范

### 类名顺序

按以下顺序组织 Tailwind 类名：

```tsx
// ✅ 推荐顺序
<div className="
  // 1. 布局 (display, position)
  flex absolute inset-0
  // 2. 尺寸 (width, height)
  w-full h-screen min-h-[400px]
  // 3. 间距 (margin, padding)
  m-4 p-6 gap-4
  // 4. 边框 (border, rounded)
  border border-gray-200 rounded-lg
  // 5. 背景 (background)
  bg-white dark:bg-gray-900
  // 6. 文字 (text, font)
  text-lg font-medium text-gray-800
  // 7. 效果 (shadow, opacity)
  shadow-md opacity-90
  // 8. 过渡动画 (transition, animation)
  transition-all duration-300
  // 9. 交互状态 (hover, focus)
  hover:bg-gray-50 focus:ring-2
">
```

### 响应式设计

```tsx
// ✅ 移动优先设计
<div className="
  grid grid-cols-1        // 默认：单列
  sm:grid-cols-2          // ≥640px：两列
  md:grid-cols-3          // ≥768px：三列
  lg:grid-cols-4          // ≥1024px：四列
  xl:grid-cols-5          // ≥1280px：五列
  gap-4 sm:gap-6 lg:gap-8
">

// ✅ 容器宽度
<div className="container mx-auto px-4 sm:px-6 lg:px-8">
```

### 暗色模式

```tsx
// ✅ 使用 dark: 前缀
<div className="
  bg-white dark:bg-gray-900
  text-gray-900 dark:text-gray-100
  border-gray-200 dark:border-gray-700
">

// ✅ 渐变在暗色模式
<div className="
  bg-gradient-to-r from-blue-500 to-purple-500
  dark:from-blue-600 dark:to-purple-600
">
```

---

## 第二部分：组件样式模式

### 按钮组件

```tsx
// ✅ 按钮变体
const buttonVariants = {
  primary: `
    bg-blue-600 text-white
    hover:bg-blue-700 active:bg-blue-800
    focus:ring-2 focus:ring-blue-500 focus:ring-offset-2
  `,
  secondary: `
    bg-gray-100 text-gray-900
    hover:bg-gray-200 active:bg-gray-300
    dark:bg-gray-800 dark:text-gray-100
    dark:hover:bg-gray-700
  `,
  ghost: `
    bg-transparent text-gray-600
    hover:bg-gray-100 hover:text-gray-900
    dark:text-gray-400 dark:hover:bg-gray-800
  `,
  danger: `
    bg-red-600 text-white
    hover:bg-red-700 active:bg-red-800
    focus:ring-2 focus:ring-red-500
  `,
};

const buttonSizes = {
  sm: 'px-3 py-1.5 text-sm',
  md: 'px-4 py-2 text-base',
  lg: 'px-6 py-3 text-lg',
};

// ✅ 基础按钮样式
const baseButton = `
  inline-flex items-center justify-center
  font-medium rounded-lg
  transition-colors duration-200
  disabled:opacity-50 disabled:cursor-not-allowed
`;
```

### 卡片组件

```tsx
// ✅ 卡片样式
<div className="
  rounded-xl border border-gray-200 dark:border-gray-700
  bg-white dark:bg-gray-800
  shadow-sm hover:shadow-md
  transition-shadow duration-200
  overflow-hidden
">
  {/* 卡片头部 */}
  <div className="px-6 py-4 border-b border-gray-100 dark:border-gray-700">
    <h3 className="text-lg font-semibold text-gray-900 dark:text-white">
      标题
    </h3>
  </div>
  
  {/* 卡片内容 */}
  <div className="px-6 py-4">
    <p className="text-gray-600 dark:text-gray-300">
      内容文本
    </p>
  </div>
  
  {/* 卡片底部 */}
  <div className="px-6 py-4 bg-gray-50 dark:bg-gray-900/50">
    <Button>操作</Button>
  </div>
</div>
```

### 表单元素

```tsx
// ✅ 输入框
<input
  className="
    w-full px-4 py-2
    border border-gray-300 dark:border-gray-600
    rounded-lg
    bg-white dark:bg-gray-800
    text-gray-900 dark:text-gray-100
    placeholder:text-gray-400 dark:placeholder:text-gray-500
    focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-transparent
    disabled:bg-gray-100 disabled:cursor-not-allowed
    transition-colors duration-200
  "
/>

// ✅ 标签
<label className="
  block mb-2
  text-sm font-medium
  text-gray-700 dark:text-gray-300
">

// ✅ 错误状态
<input className="
  border-red-500 dark:border-red-400
  focus:ring-red-500
  text-red-900 dark:text-red-400
"/>
<p className="mt-1 text-sm text-red-600 dark:text-red-400">
  错误信息
</p>
```

---

## 第三部分：布局模式

### Flexbox 布局

```tsx
// ✅ 水平居中
<div className="flex items-center justify-center">

// ✅ 两端对齐
<div className="flex items-center justify-between">

// ✅ 垂直堆叠
<div className="flex flex-col gap-4">

// ✅ 自动换行
<div className="flex flex-wrap gap-2">
```

### Grid 布局

```tsx
// ✅ 等宽网格
<div className="grid grid-cols-3 gap-4">

// ✅ 自适应网格
<div className="grid grid-cols-[repeat(auto-fill,minmax(250px,1fr))] gap-4">

// ✅ 复杂布局
<div className="grid grid-cols-12 gap-4">
  <aside className="col-span-3">侧边栏</aside>
  <main className="col-span-9">主内容</main>
</div>
```

### 常用布局

```tsx
// ✅ 粘性头部
<header className="sticky top-0 z-50 bg-white/80 backdrop-blur-sm border-b">

// ✅ 固定底部
<footer className="fixed bottom-0 inset-x-0 bg-white border-t">

// ✅ 全屏居中
<div className="min-h-screen flex items-center justify-center">

// ✅ 侧边栏布局
<div className="flex min-h-screen">
  <aside className="w-64 shrink-0 border-r">侧边栏</aside>
  <main className="flex-1 overflow-auto">主内容</main>
</div>
```

---

## 第四部分：动画效果

### 过渡动画

```tsx
// ✅ 基础过渡
<button className="
  transition-all duration-200 ease-in-out
  hover:scale-105 hover:shadow-lg
">

// ✅ 颜色过渡
<a className="
  text-gray-600 hover:text-blue-600
  transition-colors duration-150
">

// ✅ 变换过渡
<div className="
  transform hover:-translate-y-1
  transition-transform duration-300
">
```

### 关键帧动画

```tsx
// ✅ 内置动画
<div className="animate-spin">旋转</div>
<div className="animate-pulse">脉冲</div>
<div className="animate-bounce">弹跳</div>
<div className="animate-ping">扩散</div>

// ✅ 加载状态
<div className="flex items-center gap-2">
  <div className="w-2 h-2 bg-blue-500 rounded-full animate-bounce [animation-delay:-0.3s]" />
  <div className="w-2 h-2 bg-blue-500 rounded-full animate-bounce [animation-delay:-0.15s]" />
  <div className="w-2 h-2 bg-blue-500 rounded-full animate-bounce" />
</div>
```

---

## 第五部分：最佳实践

### 使用 clsx/cn 合并类名

```tsx
import { clsx, type ClassValue } from 'clsx';
import { twMerge } from 'tailwind-merge';

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs));
}

// ✅ 使用示例
<button
  className={cn(
    'px-4 py-2 rounded-lg font-medium',
    variant === 'primary' && 'bg-blue-600 text-white',
    variant === 'secondary' && 'bg-gray-100 text-gray-900',
    disabled && 'opacity-50 cursor-not-allowed',
    className, // 允许外部覆盖
  )}
>
```

### 提取复用样式

```tsx
// ✅ 在 CSS 中定义可复用样式
/* index.css */
@layer components {
  .btn-base {
    @apply inline-flex items-center justify-center px-4 py-2 
           font-medium rounded-lg transition-colors duration-200;
  }
  
  .input-base {
    @apply w-full px-4 py-2 border rounded-lg
           focus:outline-none focus:ring-2 focus:ring-blue-500;
  }
  
  .card {
    @apply rounded-xl border bg-white dark:bg-gray-800 
           shadow-sm overflow-hidden;
  }
}
```

### 避免的模式

```tsx
// ❌ 避免：内联样式混用
<div className="p-4" style={{ marginTop: '20px' }}>

// ✅ 改用 Tailwind
<div className="p-4 mt-5">

// ❌ 避免：过于具体的任意值
<div className="w-[347px] h-[183px] mt-[23px]">

// ✅ 使用设计系统值
<div className="w-80 h-44 mt-6">

// ❌ 避免：重复的长类名
<div className="flex items-center justify-center p-4 bg-white rounded-lg shadow">
<div className="flex items-center justify-center p-4 bg-white rounded-lg shadow">

// ✅ 提取为组件或 @apply
```

---

## 第六部分：性能优化

### 减少类名数量

```tsx
// ❌ 冗余类名
<div className="m-0 p-0 border-0">  // 默认值无需声明

// ✅ 只声明需要的
<div className="mt-4 p-6 border rounded-lg">
```

### 使用 CSS 变量

```css
/* ✅ 主题变量 */
:root {
  --color-primary: theme('colors.blue.600');
  --color-background: theme('colors.white');
  --radius-default: theme('borderRadius.lg');
}

.dark {
  --color-primary: theme('colors.blue.400');
  --color-background: theme('colors.gray.900');
}
```

---

## 审查清单

- [ ] 类名按逻辑顺序排列
- [ ] 响应式设计遵循移动优先
- [ ] 暗色模式样式完整
- [ ] 交互状态（hover/focus/active）已定义
- [ ] 使用设计系统值而非任意值
- [ ] 复用样式已提取
- [ ] 无冗余或重复的类名


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
