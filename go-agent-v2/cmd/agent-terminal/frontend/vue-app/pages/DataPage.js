export const DataPage = {
  name: 'DataPage',
  props: {
    pageId: { type: String, required: true },
    title: { type: String, required: true },
    icon: { type: String, default: '·' },
    items: { type: Array, default: () => [] },
    emptyText: { type: String, default: '暂无数据' },
    fields: { type: Array, default: () => [] },
  },
  template: `
    <section :id="'page-' + pageId" class="page active">
      <div class="panel-header">
        <div class="ph-bar"></div>
        <div class="ph-text">
          <h2>{{ title }}</h2>
        </div>
      </div>
      <div class="panel-body">
        <div v-if="items.length === 0" class="empty-state">
          <div class="es-icon">{{ icon }}</div>
          <h3>{{ emptyText }}</h3>
        </div>
        <div v-else class="data-list-vue">
          <article v-for="(item, idx) in items" :key="idx" class="data-card-vue">
            <div v-for="field in fields" :key="field.key" class="data-row-vue">
              <strong>{{ field.label }}</strong>
              <span>{{ item[field.key] ?? '-' }}</span>
            </div>
          </article>
        </div>
      </div>
    </section>
  `,
};
