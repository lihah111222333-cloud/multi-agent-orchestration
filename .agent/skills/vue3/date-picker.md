# DatePicker 组件

**官方文档**: https://element-plus.org/zh-CN/component/date-picker.html

## 使用说明

本文件提供 Element Plus `DatePicker` 组件的快速使用指引。

### 核心要点

- 支持单日期选择
- 支持日期区间选择
- 支持多种日期类型
- 支持格式化与快捷选项

### 示例：基础日期选择

```vue
<template>
  <el-date-picker v-model="date" type="date" placeholder="请选择日期" />
</template>

<script setup>
import { ref } from 'vue'

const date = ref('')
</script>
```

### 示例：日期范围选择

```vue
<template>
  <el-date-picker
    v-model="dateRange"
    type="daterange"
    range-separator="至"
    start-placeholder="开始日期"
    end-placeholder="结束日期"
  />
</template>

<script setup>
import { ref } from 'vue'

const dateRange = ref([])
</script>
```

### 示例：日期格式

```vue
<template>
  <el-date-picker
    v-model="date"
    type="date"
    format="YYYY-MM-DD"
    value-format="YYYY-MM-DD"
  />
</template>
```

### 关键提示

- 使用 `type` 选择模式（如 `date`、`datetime`、`daterange`）
- 使用 `format` 与 `value-format` 控制显示和提交格式
- 在业务层统一处理时区，避免前后端时间偏差
