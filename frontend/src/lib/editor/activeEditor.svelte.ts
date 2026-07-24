import type { Editor } from '@tiptap/core'

// Tracks the currently-focused TipTap editor so non-editor surfaces (the Date
// Glance status chip, the global date-glance hotkey) can decide whether an
// editor insert target is available. TipTapEditor sets this on editor focus
// and clears it on blur.
//
// lastFocused persists across blur so keyboard-only openers (Tab to chip +
// Enter) — which never fire pointerdown — can still recover the editor the
// user was just typing in. The isDestroyed guard in pickDay makes a stale
// reference safe (it falls through to clipboard).
let focused: Editor | null = $state(null)
let lastFocused: Editor | null = $state(null)

export function setActiveEditor(editor: Editor | null): void {
  if (editor) lastFocused = editor
  focused = editor
}

export function getActiveEditor(): Editor | null {
  return focused
}

/** Last editor that had focus (survives blur for keyboard-opener fallback). */
export function getLastActiveEditor(): Editor | null {
  return lastFocused
}

/** Clear all tracked editors (called on editor unmount). */
export function clearActiveEditorState(): void {
  focused = null
  lastFocused = null
}
