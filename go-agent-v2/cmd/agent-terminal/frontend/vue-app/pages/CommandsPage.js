import { logDebug } from '../services/log.js';

export const CommandsPage = {
    name: 'CommandsPage',
    props: {
        commandCards: { type: Array, default: () => [] },
        prompts: { type: Array, default: () => [] },
        commandFields: { type: Array, default: () => [] },
        promptFields: { type: Array, default: () => [] },
    },
    emits: ['run-command', 'run-prompt'],
    setup(props, { emit }) {
        function onRunCommand(item) {
            logDebug('page', 'commands.runCommand.click', {});
            emit('run-command', item);
        }

        function onRunPrompt(item) {
            logDebug('page', 'commands.runPrompt.click', {});
            emit('run-prompt', item);
        }

        return {
            onRunCommand,
            onRunPrompt,
        };
    },
    template: `
    <section id="page-commands" class="page active">
      <div class="panel-header">
        <div class="ph-bar"></div>
        <div class="ph-text"><h2>命令卡 / 提示词</h2></div>
      </div>
      <div class="split-duo">
        <div class="split-left">
          <div class="section-header">COMMANDS</div>
          <div class="panel-body">
            <div v-if="commandCards.length === 0" class="empty-state">
              <div class="es-icon">C</div>
              <h3>暂无命令卡</h3>
            </div>
            <div v-else class="data-list-vue">
              <article v-for="(item, idx) in commandCards" :key="item.card_key || ('cmd-' + idx)" class="data-card-vue">
                <div v-for="field in commandFields" :key="field.key" class="data-row-vue">
                  <strong>{{ field.label }}</strong>
                  <span>{{ item[field.key] ?? '-' }}</span>
                </div>
                <div class="data-actions-vue">
                  <button class="btn btn-ghost btn-xs" @click="onRunCommand(item)">发送到当前会话</button>
                </div>
              </article>
            </div>
          </div>
        </div>
        <div class="split-divider"></div>
        <div class="split-right">
          <div class="section-header">PROMPTS</div>
          <div class="panel-body">
            <div v-if="prompts.length === 0" class="empty-state">
              <div class="es-icon">P</div>
              <h3>暂无提示词</h3>
            </div>
            <div v-else class="data-list-vue">
              <article v-for="(item, idx) in prompts" :key="item.prompt_key || ('prompt-' + idx)" class="data-card-vue">
                <div v-for="field in promptFields" :key="field.key" class="data-row-vue">
                  <strong>{{ field.label }}</strong>
                  <span>{{ item[field.key] ?? '-' }}</span>
                </div>
                <div class="data-actions-vue">
                  <button class="btn btn-ghost btn-xs" @click="onRunPrompt(item)">发送到当前会话</button>
                </div>
              </article>
            </div>
          </div>
        </div>
      </div>
    </section>
  `,
};
