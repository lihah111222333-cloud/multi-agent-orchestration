import { ref, watch, onMounted, onUpdated, onBeforeUnmount } from '../../lib/vue.esm-browser.prod.js';
import { onFilesDropped } from '../services/api.js';
import { logDebug, logInfo } from '../services/log.js';

export const ComposerBar = {
  name: 'ComposerBar',
  props: {
    composer: { type: Object, required: true },
    disabled: { type: Boolean, default: false },
    threadId: { type: String, default: '' },
    interruptible: { type: Boolean, default: false },
    compacting: { type: Boolean, default: false },
    tokenInline: { type: String, default: '' },
    tokenTooltip: { type: String, default: '' },
    skillMatches: { type: Array, default: () => [] },
    skillMatchesLoading: { type: Boolean, default: false },
    selectedSkillNames: { type: Array, default: () => [] },
  },
  emits: ['send', 'interrupt', 'compact', 'toggle-skill', 'select-all-skills', 'clear-skills'],
  setup(props, { emit }) {
    const isComposing = ref(false);
    const pauseAcknowledged = ref(false);
    const interruptPending = ref(false);
    const interruptRequestThreadId = ref('');
    const interruptTimeoutId = ref(0);
    const dropActive = ref(false);
    const dropDepth = ref(0);
    let offFilesDropped = () => { };

    function clearInterruptTimeout() {
      if (!interruptTimeoutId.value) return;
      window.clearTimeout(interruptTimeoutId.value);
      interruptTimeoutId.value = 0;
    }

    function hasReadyInput() {
      return props.composer.canSend.value;
    }

    function onInterruptConfirmed(meta = {}) {
      const currentThreadID = (props.threadId || '').toString();
      const requestThreadID = (meta.threadId || interruptRequestThreadId.value || '').toString();
      if (requestThreadID && currentThreadID && requestThreadID !== currentThreadID) {
        logDebug('ui', 'composerBar.interrupt.confirmed.ignored', {
          request_thread_id: requestThreadID,
          current_thread_id: currentThreadID,
        });
        return;
      }
      clearInterruptTimeout();
      interruptPending.value = false;
      interruptRequestThreadId.value = '';
      pauseAcknowledged.value = true;
      logInfo('ui', 'composerBar.interrupt.confirmed', {
        mode: (meta.mode || '').toString(),
      });
    }

    function onInterruptRejected(meta = {}) {
      const currentThreadID = (props.threadId || '').toString();
      const requestThreadID = (meta.threadId || interruptRequestThreadId.value || '').toString();
      if (requestThreadID && currentThreadID && requestThreadID !== currentThreadID) {
        logDebug('ui', 'composerBar.interrupt.rejected.ignored', {
          request_thread_id: requestThreadID,
          current_thread_id: currentThreadID,
        });
        return;
      }
      clearInterruptTimeout();
      interruptPending.value = false;
      interruptRequestThreadId.value = '';
      logInfo('ui', 'composerBar.interrupt.rejected', {
        reason: (meta.reason || '').toString(),
        mode: (meta.mode || '').toString(),
      });
    }

    function armInterruptTimeout(requestThreadID) {
      clearInterruptTimeout();
      interruptTimeoutId.value = window.setTimeout(() => {
        interruptTimeoutId.value = 0;
        if (!interruptPending.value) return;
        onInterruptRejected({
          reason: 'timeout',
          mode: 'timeout',
          threadId: requestThreadID,
        });
      }, 15000);
    }

    function isPauseMode() {
      return Boolean(props.interruptible);
    }

    function onPaste(event) {
      logDebug('ui', 'composerBar.paste', {});
      props.composer.handlePaste(event);
    }

    function hasFilesTransfer(event) {
      const transfer = event?.dataTransfer;
      if (!transfer) return false;
      if (transfer.files && transfer.files.length > 0) return true;
      if (!transfer.types) return false;
      return Array.from(transfer.types).includes('Files');
    }

    function resetDropState() {
      dropDepth.value = 0;
      dropActive.value = false;
    }

    function onDragEnter(event) {
      if (props.disabled) return;
      if (!hasFilesTransfer(event)) return;
      if (typeof event.preventDefault === 'function') event.preventDefault();
      dropDepth.value += 1;
      dropActive.value = true;
    }

    function onDragOver(event) {
      if (props.disabled) return;
      if (!hasFilesTransfer(event)) return;
      if (typeof event.preventDefault === 'function') event.preventDefault();
      const transfer = event?.dataTransfer;
      if (transfer) transfer.dropEffect = 'copy';
      if (!dropActive.value) dropActive.value = true;
    }

    function onDragLeave(event) {
      if (!dropActive.value) return;
      if (typeof event.preventDefault === 'function') event.preventDefault();
      dropDepth.value = Math.max(dropDepth.value - 1, 0);
      if (dropDepth.value === 0) dropActive.value = false;
    }

    async function onDrop(event) {
      if (!hasFilesTransfer(event)) return;
      if (typeof event.preventDefault === 'function') event.preventDefault();
      resetDropState();
      if (props.disabled) return;
      try {
        await props.composer.handleDrop(event);
      } catch (error) {
        logDebug('ui', 'composerBar.drop.failed', { error });
      }
    }

    function onNativeFilesDropped(evt) {
      if (props.disabled) return;
      const payload = evt && typeof evt === 'object' ? evt : {};
      const files = Array.isArray(payload.files) ? payload.files : [];
      if (files.length === 0) return;

      const details = payload.details && typeof payload.details === 'object'
        ? payload.details
        : {};
      const targetID = (details.id || '').toString().trim();
      if (targetID && targetID !== 'chat-input-bar') return;

      const added = props.composer.attachByPaths(files, 'wails-drop');
      if (added > 0) resetDropState();
      logInfo('ui', 'composerBar.nativeDrop.handled', {
        files: files.length,
        added,
        target_id: targetID,
      });
    }

    function onCompositionStart() {
      isComposing.value = true;
      logDebug('ui', 'composerBar.composition.start', {});
    }

    function onCompositionEnd() {
      isComposing.value = false;
      logDebug('ui', 'composerBar.composition.end', {});
    }

    function onSend(event) {
      const keyCode = Number(event?.keyCode || event?.which || 0);
      const key = (event?.key || '').toString();
      const imeLikely = event?.isComposing || isComposing.value || keyCode === 229 || key === 'Process' || key === 'Unidentified';
      if (event?.type === 'keydown' && imeLikely) {
        logDebug('ui', 'composerBar.send.blockedByComposition', {
          key_code: keyCode,
          key,
          composing: Boolean(event?.isComposing || isComposing.value),
        });
        return;
      }
      if (!hasReadyInput()) {
        logDebug('ui', 'composerBar.send.skipped.noInput', {
          trigger: event?.type || '',
        });
        return;
      }
      if (event?.type === 'keydown' && typeof event.preventDefault === 'function') {
        event.preventDefault();
      }
      pauseAcknowledged.value = false;
      logDebug('ui', 'composerBar.send.click', {
        disabled: props.disabled,
      });
      emit('send');
    }

    function onPrimaryAction(event) {
      if (isPauseMode()) {
        if (interruptPending.value) return;
        const requestThreadID = (props.threadId || '').toString();
        interruptPending.value = true;
        interruptRequestThreadId.value = requestThreadID;
        armInterruptTimeout(requestThreadID);
        logInfo('ui', 'composerBar.interrupt.click', {
          disabled: props.disabled,
          pause_ack: pauseAcknowledged.value,
          pending: true,
          has_input: Boolean(hasReadyInput()),
          thread_id: requestThreadID,
        });
        emit('interrupt', {
          threadId: requestThreadID,
          confirm: (meta) => onInterruptConfirmed({
            ...meta,
            threadId: requestThreadID,
          }),
          reject: (meta) => onInterruptRejected({
            ...meta,
            threadId: requestThreadID,
          }),
        });
        return;
      }
      onSend(event);
    }

    function onEscape(event) {
      if (!Boolean(props.interruptible)) return;
      if (interruptPending.value) {
        if (typeof event?.preventDefault === 'function') event.preventDefault();
        return;
      }
      const requestThreadID = (props.threadId || '').toString();
      if (!requestThreadID) return;
      if (typeof event?.preventDefault === 'function') event.preventDefault();
      interruptPending.value = true;
      interruptRequestThreadId.value = requestThreadID;
      armInterruptTimeout(requestThreadID);
      logInfo('ui', 'composerBar.interrupt.escape', {
        disabled: props.disabled,
        pause_ack: pauseAcknowledged.value,
        pending: true,
        has_input: Boolean(hasReadyInput()),
        thread_id: requestThreadID,
      });
      emit('interrupt', {
        threadId: requestThreadID,
        confirm: (meta) => onInterruptConfirmed({
          ...meta,
          threadId: requestThreadID,
        }),
        reject: (meta) => onInterruptRejected({
          ...meta,
          threadId: requestThreadID,
        }),
      });
    }

    function onCompact() {
      if (props.disabled) return;
      if (props.compacting) return;
      if (!(props.threadId || '').toString().trim()) return;
      emit('compact');
    }

    function onAttach() {
      logDebug('ui', 'composerBar.attach.click', {
        disabled: props.disabled || props.composer.state.attaching,
      });
      props.composer.attachByPicker();
    }

    function onRemoveAttachment(index) {
      logDebug('ui', 'composerBar.attachment.remove', { index });
      props.composer.removeAttachment(index);
    }

    function normalizeSkillMatchType(match) {
      const type = (match?.matchedBy || '').toString().trim().toLowerCase();
      if (type === 'force') return 'force';
      if (type === 'explicit') return 'explicit';
      return 'trigger';
    }

    function skillMatchClass(match) {
      return normalizeSkillMatchType(match);
    }

    function skillMatchReason(match) {
      const type = normalizeSkillMatchType(match);
      const typeLabel = type === 'force' ? '强制词' : (type === 'explicit' ? '显式提及' : '触发词');
      const terms = Array.isArray(match?.matchedTerms)
        ? match.matchedTerms.map((item) => (item || '').toString().trim()).filter(Boolean)
        : [];
      if (terms.length === 0) return typeLabel;
      return `${typeLabel}: ${terms.join(' / ')}`;
    }

    function skillMatchKey(match, index) {
      const name = (match?.name || '').toString().trim();
      const reason = skillMatchReason(match);
      return `${name}|${reason}|${index}`;
    }

    function isSkillSelected(rawName) {
      const name = (rawName || '').toString().trim().toLowerCase();
      if (!name) return false;
      return props.selectedSkillNames.some((item) => (item || '').toString().trim().toLowerCase() === name);
    }

    function onToggleSkill(rawName) {
      emit('toggle-skill', (rawName || '').toString().trim());
    }

    function onSelectAllSkills() {
      emit('select-all-skills');
    }

    function onClearSkills() {
      emit('clear-skills');
    }

    watch(
      () => props.threadId,
      (next, prev) => {
        const nextID = (next || '').toString();
        const prevID = (prev || '').toString();
        if (nextID === prevID) return;
        clearInterruptTimeout();
        isComposing.value = false;
        pauseAcknowledged.value = false;
        interruptPending.value = false;
        interruptRequestThreadId.value = '';
        resetDropState();
        logDebug('ui', 'composerBar.thread.switch.reset', {
          from_thread_id: prevID,
          to_thread_id: nextID,
        });
      },
    );

    onUpdated(() => {
      if (pauseAcknowledged.value && hasReadyInput()) {
        pauseAcknowledged.value = false;
        logDebug('ui', 'composerBar.pauseAck.resetByInput', {});
      }
    });

    onMounted(() => {
      offFilesDropped = onFilesDropped(onNativeFilesDropped);
    });

    onBeforeUnmount(() => {
      offFilesDropped();
      offFilesDropped = () => { };
      clearInterruptTimeout();
      resetDropState();
    });

    return {
      isComposing,
      pauseAcknowledged,
      interruptPending,
      interruptRequestThreadId,
      interruptTimeoutId,
      dropActive,
      dropDepth,
      hasReadyInput,
      isPauseMode,
      onPaste,
      onDragEnter,
      onDragOver,
      onDragLeave,
      onDrop,
      onCompositionStart,
      onCompositionEnd,
      onSend,
      onPrimaryAction,
      onEscape,
      onCompact,
      onAttach,
      onRemoveAttachment,
      skillMatchClass,
      skillMatchReason,
      skillMatchKey,
      isSkillSelected,
      onToggleSkill,
      onSelectAllSkills,
      onClearSkills,
    };
  },
  template: `
    <div
      id="chat-input-bar"
      class="chat-input-vue"
      data-testid="composer-bar"
      :class="{ 'drop-active': dropActive }"
      data-file-drop-target=""
      style="position:relative"
      @dragenter="onDragEnter"
      @dragover="onDragOver"
      @dragleave="onDragLeave"
      @drop="onDrop"
    >
      <div v-if="compacting" class="codex-loading-bar"></div>
      <div v-if="dropActive" class="composer-drop-hint" aria-live="polite">松开即可添加附件</div>
      <div class="composer-skill-selector" role="status" aria-live="polite" data-testid="composer-skill-selector">
        <div class="composer-skill-selector-head">
          <span class="composer-skill-selector-title" :class="{ 'loading-shimmer': skillMatchesLoading }">
            {{ skillMatchesLoading ? '技能匹配中…' : ('技能选择 ' + selectedSkillNames.length + '/' + skillMatches.length) }}
          </span>
          <button
            class="composer-skill-selector-btn"
            type="button"
            :disabled="skillMatches.length === 0"
            @click="onSelectAllSkills"
          >全选</button>
          <button
            class="composer-skill-selector-btn"
            type="button"
            :disabled="selectedSkillNames.length === 0"
            @click="onClearSkills"
          >清空</button>
        </div>
        <div class="composer-skill-selector-list">
          <button
            v-for="(match, index) in skillMatches"
            :key="skillMatchKey(match, index)"
            class="composer-skill-selector-item"
            :class="[skillMatchClass(match), { selected: isSkillSelected(match.name) }]"
            type="button"
            :title="skillMatchReason(match)"
            @click="onToggleSkill(match.name)"
          >
            <span class="composer-skill-selector-item-name">{{ match.name }}</span>
            <span class="composer-skill-selector-item-reason">{{ skillMatchReason(match) }}</span>
          </button>
          <span v-if="!skillMatchesLoading && skillMatches.length === 0" class="composer-skill-selector-empty">输入触发词后可点选技能</span>
        </div>
      </div>

      <div v-if="composer.state.attachments.length > 0" class="chat-attachment-list composer-attachments">
        <span v-for="(att, idx) in composer.state.attachments" :key="att.path + idx" class="chat-attachment-pill">
          <span class="attachment-kind">{{ att.kind === 'image' ? 'IMG' : 'FILE' }}</span>
          <span class="attachment-name">{{ att.name }}</span>
          <button class="attachment-remove" @click="onRemoveAttachment(idx)" aria-label="移除附件">×</button>
        </span>
      </div>

      <div id="input-row" class="chat-input-row-vue" data-testid="composer-input-row">
        <button id="btnAttach" class="btn btn-secondary" data-testid="composer-attach-button" @click="onAttach" :disabled="composer.state.attaching || disabled">
          {{ composer.state.attaching ? '选择中...' : '附件' }}
        </button>
        <textarea
          id="chatInput"
          data-testid="composer-input"
          rows="2"
          v-model="composer.state.text"
          placeholder="输入给 Agent 的内容，Enter 发送，Shift+Enter 换行"
          :disabled="disabled"
          @paste="onPaste"
          @compositionstart="onCompositionStart"
          @compositionend="onCompositionEnd"
          @keydown.enter.exact="onSend"
          @keydown.esc.exact="onEscape"
        ></textarea>
        <div class="composer-top-actions">
          <button
            class="composer-compact-btn"
            data-testid="composer-compact-button"
            :class="{ loading: compacting }"
            type="button"
            :title="compacting ? '压缩中…' : (interruptible ? '将先暂停再压缩上下文' : '压缩上下文')"
            :aria-label="compacting ? '压缩中…' : (interruptible ? '将先暂停再压缩上下文' : '压缩上下文')"
            :disabled="disabled || !threadId || compacting"
            @click="onCompact"
          >
            <svg class="composer-compact-icon" viewBox="0 0 24 24" aria-hidden="true">
              <path
                d="M9 5l-4 4 4 4M15 5l4 4-4 4M9 19l-4-4 4-4M15 19l4-4-4-4"
                fill="none"
                stroke="currentColor"
                stroke-width="1.9"
                stroke-linecap="round"
                stroke-linejoin="round"
              />
            </svg>
          </button>
          <span
            v-if="tokenInline || compacting"
            class="composer-token-chip"
            :class="{ loading: compacting }"
            :title="compacting ? '正在暂停并压缩上下文，等待 token 使用量刷新' : tokenTooltip"
          ><span :class="{ 'loading-shimmer': compacting }">{{ compacting ? 'CTX 更新中…' : ('CTX ' + tokenInline) }}</span></span>
        </div>
        <div class="composer-action-stack">
          <button
            id="btnSend"
            class="btn btn-primary"
            data-testid="composer-send-button"
            :class="{ 'btn-stop': isPauseMode() }"
            :disabled="disabled || (isPauseMode() && interruptPending) || (!isPauseMode() && !hasReadyInput())"
            :aria-label="isPauseMode() ? '中断' : '发送'"
            @click="onPrimaryAction"
          >
            <span v-if="isPauseMode()" class="btn-stop-icon" aria-hidden="true"></span>
            <svg v-else class="btn-send-icon" viewBox="0 0 24 24" aria-hidden="true">
              <path
                d="M12 17V7M7.5 11.5L12 7l4.5 4.5"
                fill="none"
                stroke="currentColor"
                stroke-width="2.2"
                stroke-linecap="round"
                stroke-linejoin="round"
              />
            </svg>
          </button>
        </div>
      </div>
    </div>
  `,
};
