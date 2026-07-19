// BlockRefSuggest — `((` block-reference typeahead.
//
// The extension follows the editor's existing suggester lifecycle: a small
// ProseMirror plugin detects the active trigger range and leaves searching and
// popup rendering to the host. Confirming a result replaces the complete
// `((query` range with the existing atomic blockReferenceNode in one
// transaction, so the edit is one undo step and renders as a live chip.

import { Extension } from '@tiptap/core'
import type { Editor } from '@tiptap/core'
import { Plugin, PluginKey, TextSelection } from '@tiptap/pm/state'
import type { EditorState, Selection } from '@tiptap/pm/state'

export interface BlockRefContext {
  triggerPos: number
  query: string
  from: number
  to: number
}

function inCodeBlock(selection: Selection): boolean {
  const $from = selection.$from
  for (let depth = $from.depth; depth >= 1; depth--) {
    if ($from.node(depth).type.name === 'codeBlock') return true
  }
  return false
}

function inCodeMark(selection: Selection): boolean {
  return selection.$from.marks().some((mark) => mark.type.name === 'code')
}

export function getBlkRefContextAt(
  selection: Selection
): BlockRefContext | null {
  if (selection.from !== selection.to) return null
  if (inCodeBlock(selection) || inCodeMark(selection)) return null

  const $from = selection.$from
  const textBefore = $from.parent.textContent.slice(0, $from.parentOffset)
  const trigger = textBefore.lastIndexOf('((')
  if (trigger === -1) return null

  // A block reference starts a line or follows whitespace. This avoids
  // treating prose such as `word((` as an authoring command.
  if (trigger > 0 && !/\s/.test(textBefore[trigger - 1])) return null

  const query = textBefore.slice(trigger + 2)
  // Search text is intentionally not UUID-shaped: users search block content.
  // A closing parenthesis completes/manual-cancels the token.
  if (/[)\r\n]/.test(query)) return null

  const blockStart = $from.start()
  return {
    triggerPos: blockStart + trigger,
    query,
    from: blockStart + trigger,
    to: $from.pos
  }
}

export function getBlkRefContext(state: EditorState): BlockRefContext | null {
  return getBlkRefContextAt(state.selection)
}

export function applyBlockRefSuggestion(
  editor: Editor,
  blockId: string
): boolean {
  const ctx = getBlkRefContext(editor.state)
  const nodeType = editor.schema.nodes.blockReferenceNode
  if (!ctx || !nodeType || !blockId) return false

  const node = nodeType.create({ uuid: blockId })
  const tr = editor.state.tr.delete(ctx.from, ctx.to).insert(ctx.from, node)
  const after = ctx.from + node.nodeSize
  tr.setSelection(TextSelection.create(tr.doc, after, after))
  editor.view.dispatch(tr)
  return true
}

interface BlockRefSuggestState {
  context: BlockRefContext | null
  suppressed: boolean
}

const blockRefSuggestKey = new PluginKey<BlockRefSuggestState>(
  'siltBlockRefSuggest'
)

export interface BlockRefSuggestOptions {
  items: () => readonly unknown[]
  onChange: (ctx: BlockRefContext | null) => void
  onNavigate: (direction: 1 | -1) => void
  onSelectActive: () => void
}

export const BlockRefSuggest = Extension.create<BlockRefSuggestOptions>({
  name: 'siltBlockRefSuggest',

  addOptions() {
    return {
      items: () => [],
      onChange: () => {},
      onNavigate: () => {},
      onSelectActive: () => {}
    }
  },

  addProseMirrorPlugins() {
    const onChange = this.options.onChange
    let lastSig = ''

    return [
      new Plugin<BlockRefSuggestState>({
        key: blockRefSuggestKey,
        state: {
          init() {
            return { context: null, suppressed: false }
          },
          apply(tr, old, _oldState, newState) {
            const escape =
              (
                tr.getMeta(blockRefSuggestKey) as
                  { escape?: boolean } | undefined
              )?.escape === true
            let suppressed = old.suppressed
            if (tr.docChanged) suppressed = false
            if (escape) suppressed = true
            const context = suppressed
              ? null
              : getBlkRefContextAt(newState.selection)
            return { context, suppressed }
          }
        },
        view() {
          return {
            update(view) {
              const ctx =
                blockRefSuggestKey.getState(view.state)?.context ?? null
              const sig = ctx ? `${ctx.from}|${ctx.query}` : ''
              if (sig !== lastSig) {
                lastSig = sig
                onChange(ctx)
              }
            }
          }
        }
      })
    ]
  },

  addKeyboardShortcuts() {
    const editor = this.editor
    const opts = this.options
    const active = () => getBlkRefContext(editor.state) !== null
    const actionable = () => active() && opts.items().length > 0

    return {
      ArrowUp: () => {
        if (!actionable()) return false
        opts.onNavigate(-1)
        return true
      },
      ArrowDown: () => {
        if (!actionable()) return false
        opts.onNavigate(1)
        return true
      },
      Enter: () => {
        if (!actionable()) return false
        opts.onSelectActive()
        return true
      },
      Escape: () => {
        if (!active()) return false
        editor.view.dispatch(
          editor.state.tr.setMeta(blockRefSuggestKey, { escape: true })
        )
        return true
      },
      Tab: () => {
        if (!active()) return false
        editor.view.dispatch(
          editor.state.tr.setMeta(blockRefSuggestKey, { escape: true })
        )
        return false
      }
    }
  }
})
