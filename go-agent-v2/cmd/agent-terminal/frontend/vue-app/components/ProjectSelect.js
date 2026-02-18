import { logDebug } from '../services/log.js';

export const ProjectSelect = {
  name: 'ProjectSelect',
  props: {
    modelValue: { type: String, default: '.' },
    options: { type: Array, default: () => [] },
  },
  emits: ['update:modelValue', 'add-project'],
  methods: {
    onProjectChange(value) {
      logDebug('ui', 'projectSelect.changed', {
        value: value || '.',
      });
      this.$emit('update:modelValue', value);
    },
    onAddProject() {
      logDebug('ui', 'projectSelect.add.click', {});
      this.$emit('add-project');
    },
  },
  template: `
    <div class="project-select-wrap">
      <select class="project-selector" :value="modelValue" @change="onProjectChange($event.target.value)">
        <option v-for="item in options" :key="item.value" :value="item.value" :title="item.full">{{ item.label }}</option>
      </select>
      <button class="btn btn-ghost btn-xs" @click="onAddProject" title="添加项目">+ 项目</button>
    </div>
  `,
};
