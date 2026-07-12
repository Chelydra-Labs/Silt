// Concrete editor-token defaults from authored mode colors only.
// Never use flatten previewTokens (var()/color-mix) — backend validate rejects those.

import type { EditorTokens, Mode } from '../types'

export function concreteEditorDefaults(m: Mode): EditorTokens {
  const glow = m.accent.primary.glow?.trim()
  return {
    caret: m.editor?.caret ?? m.accent.primary.start,
    selection: m.editor?.selection ?? (glow || m.accent.primary.start),
    selection_text:
      m.editor?.selection_text ??
      m.surfaces.editor?.text ??
      m.surfaces.app.text,
    link: m.editor?.link ?? m.accent.secondary.start,
    link_hover: m.editor?.link_hover ?? m.accent.secondary.end,
    highlight: m.editor?.highlight ?? m.status.warn
  }
}
