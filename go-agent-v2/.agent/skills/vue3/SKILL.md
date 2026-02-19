---
name: vue3
description: Vue 3 一体化技能，覆盖 Vue3 官方指南/API 与 Element Plus Vue3 组件库。用于 Vue3 基础与进阶开发、响应式与组件设计、路由状态管理、以及 Element Plus 组件实现与样式定制。
license: Complete terms in LICENSE.txt
---

## 何时使用此技能

当用户需要以下内容时使用本技能：
- Vue 3 基础语法、模板、响应式、生命周期、组件通信
- Vue 3 工程化能力：路由、状态管理、测试、SSR、TypeScript
- Vue 3 API 查询：Composition API / Options API / SFC / 内置指令组件
- Element Plus 组件开发与排错（表单、表格、弹窗、菜单、标签页等）
- Element Plus 全局配置、主题、国际化与组件 API

## 如何使用

1. 先判断问题类型：**Vue3 核心** 或 **Element Plus**。
2. Vue3 核心问题优先读 `./examples/` 与 `./api/` 对应文件。
3. Element Plus 问题优先读同级速查文档（`./menu.md` 等）与 `./examples/guide/`。
4. 若需求同时涉及两者，合并给出可运行示例并解释关键差异。

## 路径约定

- 在项目视角该技能目录是 `vue3` 文件夹。
- 在本 `SKILL.md` 内统一使用**技能目录相对路径**（如 `./examples/getting-started/`、`./api/application.md`）。
- 不使用绝对路径。

## 文档映射

### A. Vue3 官方指南映射

**Getting Started**
- `./examples/getting-started/introduction.md` → https://cn.vuejs.org/guide/introduction.html
- `./examples/getting-started/quick-start.md` → https://cn.vuejs.org/guide/quick-start.html

**Essentials**
- `./examples/essentials/application.md` → https://cn.vuejs.org/guide/essentials/application.html
- `./examples/essentials/template-syntax.md` → https://cn.vuejs.org/guide/essentials/template-syntax.html
- `./examples/essentials/reactivity-fundamentals.md` → https://cn.vuejs.org/guide/essentials/reactivity-fundamentals.html
- `./examples/essentials/computed.md` → https://cn.vuejs.org/guide/essentials/computed.html
- `./examples/essentials/class-and-style.md` → https://cn.vuejs.org/guide/essentials/class-and-style.html
- `./examples/essentials/conditional.md` → https://cn.vuejs.org/guide/essentials/conditional.html
- `./examples/essentials/list.md` → https://cn.vuejs.org/guide/essentials/list.html
- `./examples/essentials/event-handling.md` → https://cn.vuejs.org/guide/essentials/event-handling.html
- `./examples/essentials/forms.md` → https://cn.vuejs.org/guide/essentials/forms.html
- `./examples/essentials/watchers.md` → https://cn.vuejs.org/guide/essentials/watchers.html
- `./examples/essentials/template-refs.md` → https://cn.vuejs.org/guide/essentials/template-refs.html
- `./examples/essentials/component-basics.md` → https://cn.vuejs.org/guide/essentials/component-basics.html
- `./examples/essentials/lifecycle.md` → https://cn.vuejs.org/guide/essentials/lifecycle.html

**Components In-Depth**
- `./examples/components/registration.md` → https://cn.vuejs.org/guide/components/registration.html
- `./examples/components/props.md` → https://cn.vuejs.org/guide/components/props.html
- `./examples/components/events.md` → https://cn.vuejs.org/guide/components/events.html
- `./examples/components/v-model.md` → https://cn.vuejs.org/guide/components/v-model.html
- `./examples/components/attrs.md` → https://cn.vuejs.org/guide/components/attrs.html
- `./examples/components/slots.md` → https://cn.vuejs.org/guide/components/slots.html
- `./examples/components/provide-inject.md` → https://cn.vuejs.org/guide/components/provide-inject.html
- `./examples/components/async.md` → https://cn.vuejs.org/guide/components/async.html

**Reusability**
- `./examples/reusability/composables.md` → https://cn.vuejs.org/guide/reusability/composables.html
- `./examples/reusability/custom-directives.md` → https://cn.vuejs.org/guide/reusability/custom-directives.html
- `./examples/reusability/plugins.md` → https://cn.vuejs.org/guide/reusability/plugins.html

**Built-in Components**
- `./examples/built-ins/transition.md` → https://cn.vuejs.org/guide/built-ins/transition.html
- `./examples/built-ins/transition-group.md` → https://cn.vuejs.org/guide/built-ins/transition-group.html
- `./examples/built-ins/keep-alive.md` → https://cn.vuejs.org/guide/built-ins/keep-alive.html
- `./examples/built-ins/teleport.md` → https://cn.vuejs.org/guide/built-ins/teleport.html
- `./examples/built-ins/suspense.md` → https://cn.vuejs.org/guide/built-ins/suspense.html

### B. Vue3 API 映射

**Global API**
- `./api/application.md` → https://cn.vuejs.org/api/application.html
- `./api/general.md` → https://cn.vuejs.org/api/general.html

**Composition API**
- `./api/composition-api-setup.md` → https://cn.vuejs.org/api/composition-api-setup.html
- `./api/reactivity-core.md` → https://cn.vuejs.org/api/reactivity-core.html
- `./api/reactivity-utilities.md` → https://cn.vuejs.org/api/reactivity-utilities.html
- `./api/reactivity-advanced.md` → https://cn.vuejs.org/api/reactivity-advanced.html
- `./api/composition-api-lifecycle.md` → https://cn.vuejs.org/api/composition-api-lifecycle.html
- `./api/composition-api-dependency-injection.md` → https://cn.vuejs.org/api/composition-api-dependency-injection.html
- `./api/composition-api-helpers.md` → https://cn.vuejs.org/api/composition-api-helpers.html

**Options API**
- `./api/options-state.md` → https://cn.vuejs.org/api/options-state.html
- `./api/options-rendering.md` → https://cn.vuejs.org/api/options-rendering.html
- `./api/options-lifecycle.md` → https://cn.vuejs.org/api/options-lifecycle.html
- `./api/options-composition.md` → https://cn.vuejs.org/api/options-composition.html
- `./api/options-misc.md` → https://cn.vuejs.org/api/options-misc.html
- `./api/component-instance.md` → https://cn.vuejs.org/api/component-instance.html

### C. Element Plus 映射

**同级速查文档**
- `./menu.md`
- `./tabs.md`
- `./date-picker.md`
- `./select.md`
- `./switch.md`
- `./checkbox.md`
- `./radio.md`

**Guide（Element Plus）**
- `./examples/guide/installation.md`
- `./examples/guide/quick-start.md`
- `./examples/guide/design.md`
- `./examples/guide/i18n.md`
- `./examples/guide/theme.md`
- `./examples/guide/global-config.md`

**Element Plus API**
- `./api/component-api.md`
- `./api/props-and-events.md`
- `./api/global-config.md`

## 最佳实践

1. 先给最小可运行示例，再逐步扩展业务逻辑。
2. 组合式 API 优先，状态来源保持单一且可追踪。
3. Element Plus 优先按需引入，减少打包体积。
4. 复杂表单明确校验、联动与提交数据结构。
5. TypeScript 场景默认补全关键类型定义。

## 资源

- Vue3 Guide: https://cn.vuejs.org/guide/introduction.html
- Vue3 API: https://cn.vuejs.org/api/
- Element Plus: https://element-plus.org/zh-CN/

## 关键词

Vue 3, Vue.js, Composition API, Options API, reactivity, template syntax, components, directives, lifecycle, routing, state management, TypeScript, Element Plus, UI 组件库, 表单, 表格, 弹窗
