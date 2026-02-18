import { onBeforeUnmount, onMounted } from '../../lib/vue.esm-browser.prod.js';
import { UnifiedChatPage } from './UnifiedChatPage.js';
import { logInfo } from '../services/log.js';

export const ChatPage = {
  name: 'ChatPage',
  components: { UnifiedChatPage },
  props: {
    projectStore: { type: Object, required: true },
    threadStore: { type: Object, required: true },
  },
  setup() {
    onMounted(() => {
      logInfo('page', 'chat.mounted', {});
    });
    onBeforeUnmount(() => {
      logInfo('page', 'chat.unmounted', {});
    });
    return {};
  },
  template: `
    <UnifiedChatPage
      mode="chat"
      :project-store="projectStore"
      :thread-store="threadStore"
    />
  `,
};
