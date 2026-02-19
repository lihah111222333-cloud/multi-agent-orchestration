# Tabs 组件

**官方文档**: https://element-plus.org/zh-CN/component/tabs.html

## 使用说明

本文件提供 Element Plus `Tabs` 组件的快速使用指引。

### 核心要点

- 使用 `v-model` 控制当前激活标签
- 支持多种标签页样式
- 支持标签点击事件

### 示例：基础 Tabs

```vue
<template>
  <el-tabs v-model="activeName">
    <el-tab-pane label="用户" name="first">用户内容</el-tab-pane>
    <el-tab-pane label="配置" name="second">配置内容</el-tab-pane>
    <el-tab-pane label="角色" name="third">角色内容</el-tab-pane>
  </el-tabs>
</template>

<script setup>
import { ref } from 'vue'

const activeName = ref('first')
</script>
```

### 示例：卡片风格 Tabs

```vue
<template>
  <el-tabs v-model="activeName" type="card">
    <el-tab-pane label="标签一" name="1">内容一</el-tab-pane>
    <el-tab-pane label="标签二" name="2">内容二</el-tab-pane>
  </el-tabs>
</template>
```

### 示例：标签点击事件

```vue
<template>
  <el-tabs v-model="activeName" @tab-click="handleClick">
    <el-tab-pane label="A" name="a">A 内容</el-tab-pane>
    <el-tab-pane label="B" name="b">B 内容</el-tab-pane>
  </el-tabs>
</template>

<script setup>
const handleClick = (tab) => {
  console.log('点击标签:', tab.props.name)
}
</script>
```

### 关键提示

- 使用 `v-model` 管理当前激活标签页名称
- 使用 `type="card"` 或 `type="border-card"` 设置样式
- 监听 `tab-click` 处理切换逻辑
