import { logDebug } from '../services/log.js';

export const ComposerBar = {
  name: 'ComposerBar',
  props: {
    composer: { type: Object, required: true },
    disabled: { type: Boolean, default: false },
  },
  emits: ['send'],
  data() {
    return {
      isComposing: false,
    };
  },
  methods: {
    onPaste(event) {
      logDebug('ui', 'composerBar.paste', {});
      this.composer.handlePaste(event);
    },
    onCompositionStart() {
      this.isComposing = true;
    },
    onCompositionEnd() {
      this.isComposing = false;
    },
    onSend(event) {
      const keyCode = Number(event?.keyCode || event?.which || 0);
      if (event?.type === 'keydown' && (event?.isComposing || this.isComposing || keyCode === 229)) {
        logDebug('ui', 'composerBar.send.blockedByComposition', {
          key_code: keyCode,
        });
        return;
      }
      logDebug('ui', 'composerBar.send.click', {
        disabled: this.disabled,
      });
      this.$emit('send');
    },
    onAttach() {
      logDebug('ui', 'composerBar.attach.click', {
        disabled: this.disabled || this.composer.state.attaching,
      });
      this.composer.attachByPicker();
    },
    onRemoveAttachment(index) {
      logDebug('ui', 'composerBar.attachment.remove', { index });
      this.composer.removeAttachment(index);
    },
  },
  template: `
    <div id="chat-input-bar" class="chat-input-vue">
      <div v-if="composer.state.attachments.length > 0" class="chat-attachment-list composer-attachments">
        <span v-for="(att, idx) in composer.state.attachments" :key="att.path + idx" class="chat-attachment-pill">
          <span class="attachment-kind">{{ att.kind === 'image' ? 'IMG' : 'FILE' }}</span>
          <span class="attachment-name">{{ att.name }}</span>
          <button class="attachment-remove" @click="onRemoveAttachment(idx)" aria-label="移除附件">×</button>
        </span>
      </div>

      <div id="input-row" class="chat-input-row-vue">
        <button id="btnAttach" class="btn btn-secondary" @click="onAttach" :disabled="composer.state.attaching || disabled">
          {{ composer.state.attaching ? '选择中...' : '附件' }}
        </button>
        <textarea
          id="chatInput"
          rows="2"
          v-model="composer.state.text"
          placeholder="输入给 Agent 的内容，Enter 发送，Shift+Enter 换行"
          :disabled="disabled"
          @paste="onPaste"
          @compositionstart="onCompositionStart"
          @compositionend="onCompositionEnd"
          @keydown.enter.exact.prevent="onSend"
        ></textarea>
        <button id="btnSend" class="btn btn-primary" :disabled="disabled || !composer.canSend.value" @click="onSend">发送</button>
      </div>
    </div>
  `,
};
