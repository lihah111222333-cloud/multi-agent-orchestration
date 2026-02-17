export const ChatTimeline = {
  name: 'ChatTimeline',
  props: {
    items: { type: Array, default: () => [] },
  },
  template: `
    <div class="chat-messages-vue">
      <div v-if="items.length === 0" class="diff-empty">暂无消息，先发送一句话试试。</div>

      <article v-for="item in items" :key="item.id" class="chat-item" :class="'kind-' + item.kind">
        <template v-if="item.kind === 'user'">
          <header class="chat-item-head">你</header>
          <pre class="chat-item-body">{{ item.text }}</pre>
          <div v-if="(item.attachments || []).length > 0" class="chat-attachment-list">
            <span v-for="(att, idx) in item.attachments" :key="idx" class="chat-attachment-pill">
              <template v-if="att.kind === 'image'">🖼️</template>
              <template v-else>📎</template>
              {{ att.name || att.path }}
            </span>
          </div>
        </template>

        <template v-else-if="item.kind === 'assistant'">
          <header class="chat-item-head">助手</header>
          <pre class="chat-item-body">{{ item.text }}</pre>
        </template>

        <template v-else-if="item.kind === 'thinking'">
          <header class="chat-item-head">思考 {{ item.done ? '✓' : '…' }}</header>
          <pre class="chat-item-body">{{ item.text }}</pre>
        </template>

        <template v-else-if="item.kind === 'command'">
          <header class="chat-item-head">命令 {{ item.status === 'running' ? '执行中' : (item.status === 'failed' ? '失败' : '完成') }}</header>
          <pre class="chat-item-body">$ {{ item.command }}</pre>
          <pre v-if="item.output" class="chat-item-body cmd-output">{{ item.output }}</pre>
          <div v-if="typeof item.exitCode !== 'undefined'" class="chat-item-foot">exit {{ item.exitCode }}</div>
        </template>

        <template v-else-if="item.kind === 'tool'">
          <header class="chat-item-head">工具 {{ item.status === 'failed' ? '失败' : '调用' }}</header>
          <pre class="chat-item-body">{{ item.tool }}</pre>
          <pre v-if="item.file" class="chat-item-body">{{ item.file }}</pre>
          <pre v-if="item.preview" class="chat-item-body">{{ item.preview }}</pre>
          <div v-if="typeof item.elapsedMs !== 'undefined'" class="chat-item-foot">{{ item.elapsedMs }}ms</div>
        </template>

        <template v-else-if="item.kind === 'file'">
          <header class="chat-item-head">文件 {{ item.status === 'saved' ? '已保存' : '修改中' }}</header>
          <pre class="chat-item-body">{{ item.file || '(unknown file)' }}</pre>
        </template>

        <template v-else-if="item.kind === 'approval'">
          <header class="chat-item-head">审批请求</header>
          <pre class="chat-item-body">{{ item.command || '需要用户确认' }}</pre>
        </template>

        <template v-else-if="item.kind === 'plan'">
          <header class="chat-item-head">计划 {{ item.done ? '✓' : '' }}</header>
          <pre class="chat-item-body">{{ item.text }}</pre>
        </template>

        <template v-else-if="item.kind === 'error'">
          <header class="chat-item-head" style="color:var(--error)">错误</header>
          <pre class="chat-item-body" style="color:var(--error)">{{ item.text }}</pre>
        </template>
      </article>
    </div>
  `,
};
