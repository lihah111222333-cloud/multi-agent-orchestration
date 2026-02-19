# Checkbox 组件

**官方文档**: https://element-plus.org/zh-CN/component/checkbox.html

## 使用说明

本文件提供 Element Plus `Checkbox` 组件的快速使用指引。

### 核心要点

- 支持单个复选框
- 支持复选框组
- 支持半选状态
- 适合全选/反选场景

### 示例：基础复选框

```vue
<template>
  <el-checkbox v-model="checked">选项</el-checkbox>
</template>

<script setup>
import { ref } from 'vue'

const checked = ref(false)
</script>
```

### 示例：复选框组

```vue
<template>
  <el-checkbox-group v-model="checkedList">
    <el-checkbox label="A" value="A" />
    <el-checkbox label="B" value="B" />
    <el-checkbox label="C" value="C" />
  </el-checkbox-group>
</template>

<script setup>
import { ref } from 'vue'

const checkedList = ref(['A'])
</script>
```

### 示例：半选状态

```vue
<template>
  <el-checkbox v-model="checkAll" :indeterminate="isIndeterminate">
    全选
  </el-checkbox>
</template>

<script setup>
import { ref } from 'vue'

const checkAll = ref(false)
const isIndeterminate = ref(true)
</script>
```

### 关键提示

- 单个复选框适合布尔字段
- 复选框组适合多选列表
- 半选状态适合父子联动和部分选中场景
