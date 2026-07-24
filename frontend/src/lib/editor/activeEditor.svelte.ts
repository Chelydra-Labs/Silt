import type { Editor } from '@tiptap/core'

// Tracks the currently-focused TipTap editor so non-editor surfaces (the Date
// Glance status chip, the global date-glance hotkey) can decide whether an
// editor insert target is available without each caller reaching into the
// editor registry. TipTapEditor sets this on editor focus and clears it on
// blur, so a reader sees the live focus state (not a stale "last editor").
let focused: Editor | null = $state(null)

export function setActiveEditor(editor: Editor | null): void {
  focused = editor
}

export function getActiveEditor(): Editor | null {
  return focused
}
