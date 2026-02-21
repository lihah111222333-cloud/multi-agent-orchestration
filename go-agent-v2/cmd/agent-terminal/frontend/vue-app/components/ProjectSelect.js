import { logDebug } from '../services/log.js';

export const ProjectSelect = {
  name: 'ProjectSelect',
  props: {
    modelValue: { type: String, default: '.' },
    options: { type: Array, default: () => [] },
  },
  emits: ['update:modelValue', 'add-project'],
  setup(props, { emit }) {
    function onProjectChange(value) {
      logDebug('ui', 'projectSelect.changed', {
        value: value || '.',
      });
      emit('update:modelValue', value);
    }

    function onAddProject() {
      logDebug('ui', 'projectSelect.add.click', {});
      emit('add-project');
    }

    return {
      onProjectChange,
      onAddProject,
    };
  },
  template: `
    <div class="project-select-wrap">
      <select class="project-selector" :value="modelValue" @change="onProjectChange($event.target.value)">
        <option v-for="item in options" :key="item.value" :value="item.value" :title="item.full">{{ item.label }}</option>
      </select>
      <button
        class="btn btn-ghost btn-xs project-add-btn"
        @click="onAddProject"
        title="添加项目"
        aria-label="添加项目"
      >
        <svg viewBox="0 0 20 20" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
          <path d="M2.8 6.4C2.8 5.5 3.5 4.8 4.4 4.8H8.1L9.8 6.6H15.6C16.5 6.6 17.2 7.3 17.2 8.2V13.6C17.2 14.5 16.5 15.2 15.6 15.2H4.4C3.5 15.2 2.8 14.5 2.8 13.6V6.4Z"></path>
          <path d="M9.9 9.1V12.9"></path>
          <path d="M8 11H11.8"></path>
        </svg>
      </button>
    </div>
  `,
};
