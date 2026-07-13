// Host-level coordinator for the two right-side AI drawers so they never
// squeeze the note pane by being open at once (#542). The Writing Assistant
// (`silt-ai-assistant`) and AI Assistant (`silt-ai-qa`) drawers are layout
// siblings in App.svelte; opening one closes the other.
//
// The drawer state still lives in each plugin's `drawer.svelte.ts` (the single
// source of truth for the open flag). This module is the sole place that
// enforces mutual exclusion, and every external open/toggle path routes
// through it. The dependency graph is acyclic: this module imports both
// drawer modules; neither drawer module imports this one (callers do).

import {
  aiSearchDrawer,
  closeAISearchDrawer
} from '../plugins/first-party/silt-ai-qa/drawer.svelte'
import {
  writingAssistantDrawer,
  closeWritingAssistantDrawer
} from '../plugins/first-party/silt-ai-assistant/drawer.svelte'

/** Open the AI Assistant (Q&A) drawer, closing the Writing Assistant if open. */
export function openAISearchDrawerExclusive(): void {
  if (writingAssistantDrawer.open) closeWritingAssistantDrawer()
  aiSearchDrawer.open = true
}

/** Toggle the AI Assistant (Q&A) drawer, closing the Writing Assistant when opening. */
export function toggleAISearchDrawerExclusive(): void {
  if (aiSearchDrawer.open) {
    closeAISearchDrawer()
    return
  }
  if (writingAssistantDrawer.open) closeWritingAssistantDrawer()
  aiSearchDrawer.open = true
}

/** Open the Writing Assistant drawer, closing the AI Assistant (Q&A) if open. */
export function openWritingAssistantDrawerExclusive(): void {
  if (aiSearchDrawer.open) closeAISearchDrawer()
  writingAssistantDrawer.open = true
}

/** Toggle the Writing Assistant drawer, closing the AI Assistant (Q&A) when opening. */
export function toggleWritingAssistantDrawerExclusive(): void {
  if (writingAssistantDrawer.open) {
    closeWritingAssistantDrawer()
    return
  }
  if (aiSearchDrawer.open) closeAISearchDrawer()
  writingAssistantDrawer.open = true
}
