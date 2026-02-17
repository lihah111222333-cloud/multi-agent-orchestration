export const ProjectSelect = {
  name: 'ProjectSelect',
  props: {
    modelValue: { type: String, default: '.' },
    options: { type: Array, default: () => [] },
  },
  emits: ['update:modelValue', 'add-project'],
  template: `
    <div class="project-select-wrap">
      <select class="project-selector" :value="modelValue" @change="$emit('update:modelValue', $event.target.value)">
        <option v-for="item in options" :key="item.value" :value="item.value" :title="item.full">{{ item.label }}</option>
      </select>
      <button class="btn btn-ghost btn-xs" @click="$emit('add-project')" title="添加项目">+ 项目</button>
    </div>
  `,
};
