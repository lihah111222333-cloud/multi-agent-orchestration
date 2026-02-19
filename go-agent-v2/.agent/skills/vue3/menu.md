# Menu 组件

**官方文档**: https://element-plus.org/zh-CN/component/menu.html

## 使用说明

本文件提供 Element Plus `Menu` 组件的快速使用指引。

### 核心要点

- 支持横向与纵向菜单
- 支持默认激活项控制
- 支持多级子菜单
- 支持选择事件处理

### 示例：基础纵向菜单

```vue
<template>
  <el-menu default-active="1" @select="handleSelect">
    <el-menu-item index="1">首页</el-menu-item>
    <el-menu-item index="2">订单</el-menu-item>
    <el-menu-item index="3">设置</el-menu-item>
  </el-menu>
</template>

<script setup>
const handleSelect = (index) => {
  console.log('当前选中:', index)
}
</script>
```

### 示例：带子菜单

```vue
<template>
  <el-menu default-active="2-1">
    <el-sub-menu index="1">
      <template #title>导航一</template>
      <el-menu-item index="1-1">选项一</el-menu-item>
      <el-menu-item index="1-2">选项二</el-menu-item>
    </el-sub-menu>
    <el-sub-menu index="2">
      <template #title>导航二</template>
      <el-menu-item index="2-1">选项一</el-menu-item>
      <el-menu-item index="2-2">选项二</el-menu-item>
    </el-sub-menu>
  </el-menu>
</template>
```

### 示例：横向菜单

```vue
<template>
  <el-menu mode="horizontal" default-active="1">
    <el-menu-item index="1">仪表盘</el-menu-item>
    <el-menu-item index="2">分析</el-menu-item>
    <el-menu-item index="3">个人中心</el-menu-item>
  </el-menu>
</template>
```

### 关键提示

- 使用 `default-active` 设置默认激活菜单项
- 使用 `mode="horizontal"` 构建顶部导航
- 使用 `el-sub-menu` 组织多级菜单
- 监听 `select` 事件处理路由跳转或业务逻辑
