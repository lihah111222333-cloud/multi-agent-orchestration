import { computed } from '../../lib/vue.esm-browser.prod.js';

export const ProjectModal = {
  name: 'ProjectModal',
  props: {
    store: { type: Object, required: true },
  },
  setup(props) {
    const canConfirm = computed(() => Boolean((props.store.state.modalPath || '').trim()));
    return { canConfirm };
  },
  template: `
    <div v-if="store.state.showModal" class="modal-overlay" @click.self="store.closeModal()">
      <div class="modal-box">
        <div class="modal-title">添加项目</div>
        <div style="display:flex;gap:8px;align-items:center">
          <input
            v-model="store.state.modalPath"
            class="modal-input"
            type="text"
            placeholder="/Users/you/projects/my-app"
            spellcheck="false"
            autocomplete="off"
            style="flex:1"
            @keydown.enter="store.confirmModal()"
            @keydown.esc="store.closeModal()"
          />
          <button class="btn btn-secondary" style="flex-shrink:0;font-size:11px;padding:6px 10px" @click="store.browseDirectory()" :disabled="store.state.browsing">
            {{ store.state.browsing ? '打开中...' : '浏览...' }}
          </button>
        </div>
        <div class="modal-btns">
          <button class="btn btn-ghost" @click="store.closeModal()">取消</button>
          <button class="btn btn-primary" @click="store.confirmModal()" :disabled="!canConfirm">确定</button>
        </div>
      </div>
    </div>
  `,
};
