export const SidebarNav = {
  name: 'SidebarNav',
  props: {
    page: { type: String, required: true },
    items: { type: Array, required: true },
  },
  emits: ['change'],
  template: `
    <nav id="sidebar">
      <button
        v-for="item in items"
        :key="item.key"
        class="sidebar-btn"
        :class="{ active: item.key === page }"
        @click="$emit('change', item.key)"
      >
        <span class="sb-icon">{{ item.icon }}</span>
        <span class="sb-label">{{ item.label }}</span>
      </button>
      <div class="sidebar-spacer"></div>
    </nav>
  `,
};
