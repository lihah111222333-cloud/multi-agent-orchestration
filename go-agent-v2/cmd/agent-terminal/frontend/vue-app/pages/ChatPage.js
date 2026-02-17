import { UnifiedChatPage } from './UnifiedChatPage.js';

export const ChatPage = {
  name: 'ChatPage',
  components: { UnifiedChatPage },
  props: {
    projectStore: { type: Object, required: true },
    threadStore: { type: Object, required: true },
  },
  template: `
    <UnifiedChatPage
      mode="chat"
      :project-store="projectStore"
      :thread-store="threadStore"
    />
  `,
};
