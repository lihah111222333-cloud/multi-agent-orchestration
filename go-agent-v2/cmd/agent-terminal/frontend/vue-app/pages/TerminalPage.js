import { UnifiedChatPage } from './UnifiedChatPage.js';

export const TerminalPage = {
  name: 'TerminalPage',
  components: { UnifiedChatPage },
  props: {
    projectStore: { type: Object, required: true },
    threadStore: { type: Object, required: true },
  },
  template: `
    <UnifiedChatPage
      mode="cmd"
      :project-store="projectStore"
      :thread-store="threadStore"
    />
  `,
};
