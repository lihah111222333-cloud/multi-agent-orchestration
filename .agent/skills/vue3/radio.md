# Radio 组件

**官方文档**: https://element-plus.org/zh-CN/component/radio.html

## 使用说明

本文件提供 Element Plus `Radio` 组件的快速使用指引。

### 核心要点

- 适用于互斥单选场景
- 使用 `el-radio-group` 管理组选中值
- 支持按钮风格单选
- 支持变更事件监听

### 示例：基础单选组

```vue
<template>
  <el-radio-group v-model="value">
    <el-radio label="A" value="A">选项 A</el-radio>
    <el-radio label="B" value="B">选项 B</el-radio>
    <el-radio label="C" value="C">选项 C</el-radio>
  </el-radio-group>
</template>

<script setup>
import { ref } from 'vue'

const value = ref('A')
</script>
```

### 示例：按钮风格

```vue
<template>
  <el-radio-group v-model="value">
    <el-radio-button label="左对齐" value="left" />
    <el-radio-button label="居中" value="center" />
    <el-radio-button label="右对齐" value="right" />
  </el-radio-group>
</template>
```

### 示例：变更事件

```vue
<template>
  <el-radio-group v-model="value" @change="handleChange">
    <el-radio label="是" value="yes" />
    <el-radio label="否" value="no" />
  </el-radio-group>
</template>

<script setup>
const handleChange = (val) => {
  console.log('单选变化:', val)
}
</script>
```

### 关键提示

- 单选组件用于多选项中的唯一选择
- 使用 `el-radio-group` 统一维护状态
- 文案应简洁明确，默认值应符合业务预期
