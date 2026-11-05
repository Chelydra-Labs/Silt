import { Extension } from '@tiptap/core'
import { normalizePastedHTML } from './normalizePastedHTML'

// WHY an extension-level transformPastedHTML:
//
// - It must NOT live in `editorProps.transformPastedHTML`. Both editor
//   surfaces build editorProps from a shared object and call `setOptions()`
//   later; that re-applies the raw editorProps and silently drops any
//   composed extension hook (Tiptap composes extension transforms INTO the
//   view prop at createView time only).
// - It must NOT be a ProseMirror plugin prop. Tiptap always sets a direct
//   `transformPastedHTML` view prop, which shadows plugin props entirely.
//
// extensionManager.transformPastedHTML() is the only composition point that
// survives both, so the normalizer is an Extension with a high priority (it
// should see the raw clipboard HTML before any other transform).
export const ExternalPasteNormalizer = Extension.create({
  name: 'externalPasteNormalizer',
  priority: 1000,

  transformPastedHTML(html: string): string {
    return normalizePastedHTML(html)
  }
})
