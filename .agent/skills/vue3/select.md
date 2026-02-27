# Select 组件

**官方文档**: https://element-plus.org/zh-CN/component/select.html

## 使用说明

本文件提供 Element Plus `Select` 组件的快速使用指引。

### 核心要点

- 支持基础下拉选择
- 支持多选模式
- 支持可搜索筛选
- 支持选择事件处理

### 示例：基础选择

```vue
<template>
  <el-select v-model="value" placeholder="请选择">
    <el-option label="选项一" value="1" />
    <el-option label="选项二" value="2" />
    <el-option label="选项三" value="3" />
  </el-select>
</template>

<script setup>
import { ref } from 'vue'

const value = ref('')
</script>
```

### 示例：多选

```vue
<template>
  <el-select v-model="value" multiple placeholder="请选择多个">
    <el-option label="选项一" value="1" />
    <el-option label="选项二" value="2" />
    <el-option label="选项三" value="3" />
  </el-select>
</template>

<script setup>
import { ref } from 'vue'

const value = ref([])
</script>
```

### 示例：可搜索选择

```vue
<template>
  <el-select v-model="value" filterable placeholder="请输入并选择">
    <el-option
      v-for="item in options"
      :key="item.value"
      :label="item.label"
      :value="item.value"
    />
  </el-select>
</template>

<script setup>
import { ref } from 'vue'

const value = ref('')
const options = ref([
  { label: '选项一', value: '1' },
  { label: '选项二', value: '2' },
  { label: '选项三', value: '3' }
])
</script>
```

### 关键提示

- 使用 `v-model` 绑定当前选中值
- 使用 `multiple` 开启多选
- 使用 `filterable` 开启搜索功能
