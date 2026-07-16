import type { AIChatRequestOptions } from './ai-chat-controller.svelte'

export const AI_CHAT_COMMAND_EVENT = 'silt-ai-chat-command'

export interface AIChatCommandDetail {
  text: string
  request?: AIChatRequestOptions
}
