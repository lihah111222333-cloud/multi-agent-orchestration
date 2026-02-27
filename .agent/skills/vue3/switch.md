# Switch 组件

**官方文档**: https://element-plus.org/zh-CN/component/switch.html

## 使用说明

本文件提供 Element Plus `Switch` 组件的快速使用指引。

### 核心要点

- 布尔开关切换
- 自定义开关值
- 禁用状态控制
- 变更事件处理

### 示例：基础开关

```vue
<template>
  <el-switch v-model="value" />
</template>

<script setup>
import { ref } from 'vue'

const value = ref(false)
</script>
```

### 示例：自定义开关值

```vue
<template>
  <el-switch
    v-model="status"
    :active-value="1"
    :inactive-value="0"
    active-text="开启"
    inactive-text="关闭"
  />
</template>

<script setup>
import { ref } from 'vue'

const status = ref(0)
</script>
```

### 示例：监听变更事件

```vue
<template>
  <el-switch v-model="value" @change="handleChange" />
</template>

<script setup>
const handleChange = (val) => {
  console.log('开关变化:', val)
}
</script>
```

### 关键提示

- `v-model` 可以绑定 `boolean` 或自定义值
- 使用 `active-value`/`inactive-value` 适配后端字段
- 监听 `change` 处理副作用逻辑
