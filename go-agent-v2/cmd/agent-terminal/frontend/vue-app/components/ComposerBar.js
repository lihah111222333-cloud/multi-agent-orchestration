export const ComposerBar = {
  name: 'ComposerBar',
  props: {
    composer: { type: Object, required: true },
    disabled: { type: Boolean, default: false },
  },
  emits: ['send'],
  methods: {
    onPaste(event) {
      this.composer.handlePaste(event);
    },
    onSend() {
      this.$emit('send');
    },
  },
  template: `
    <div id="chat-input-bar" class="chat-input-vue">
      <div v-if="composer.state.attachments.length > 0" class="chat-attachment-list composer-attachments">
        <span v-for="(att, idx) in composer.state.attachments" :key="att.path + idx" class="chat-attachment-pill">
          <template v-if="att.kind === 'image'">🖼️</template>
          <template v-else>📎</template>
          {{ att.name }}
          <button class="attachment-remove" @click="composer.removeAttachment(idx)">✕</button>
        </span>
      </div>

      <div id="input-row" class="chat-input-row-vue">
        <button id="btnAttach" class="btn btn-secondary" @click="composer.attachByPicker()" :disabled="composer.state.attaching || disabled">
          {{ composer.state.attaching ? '选择中...' : '+' }}
        </button>
        <textarea
          id="chatInput"
          rows="2"
          v-model="composer.state.text"
          placeholder="Ask anything..."
          :disabled="disabled"
          @paste="onPaste"
          @keydown.enter.exact.prevent="onSend"
        ></textarea>
        <button id="btnSend" class="btn btn-primary" :disabled="disabled || !composer.canSend.value" @click="onSend">发送</button>
      </div>
    </div>
  `,
};
