// In-session Q&A conversation state (#227).

import type { Citation, QAMessage } from './types'

export function createConversation() {
  let messages: QAMessage[] = []

  return {
    getMessages(): QAMessage[] {
      return messages
    },
    addUser(content: string) {
      messages = [...messages, { role: 'user', content }]
    },
    addAssistant(content: string, citations?: Citation[]) {
      messages = [...messages, { role: 'assistant', content, citations }]
    },
    /** Replace the last assistant message (streaming updates). */
    updateLastAssistant(content: string, citations?: Citation[]) {
      if (messages.length === 0) return
      const last = messages[messages.length - 1]
      if (last.role !== 'assistant') {
        messages = [...messages, { role: 'assistant', content, citations }]
        return
      }
      messages = [
        ...messages.slice(0, -1),
        { role: 'assistant', content, citations }
      ]
    },
    clear() {
      messages = []
    }
  }
}

export type Conversation = ReturnType<typeof createConversation>
