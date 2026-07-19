// PageLinkSuggest — `[[` page-link typeahead.
//
// The extension only detects an unfinished target and routes keyboard input.
// Search and resolution stay with the host so IPC remains race guarded, while
// the helpers below keep ranking and the atomic node replacement testable.

import { Extension } from '@tiptap/core'
import type { Editor } from '@tiptap/core'
import { Plugin, PluginKey, TextSelection } from '@tiptap/pm/state'
import type { EditorState, Selection } from '@tiptap/pm/state'
import { fuzzyScore, normalizeSearch } from '../navigationCatalog'

export interface PageLinkContext {
  triggerPos: number
  query: string
  from: number
  to: number
}

export interface PageLinkItem {
  source?: string
  notebook: string
  section: string
  page: string
}

export interface PageLinkResolution {
  exists: boolean
  shortest: string
}

export type PageLinkResolver = (
  target: string
) => Promise<PageLinkResolution | null | undefined>

function inCode(selection: Selection): boolean {
  const $from = selection.$from
  for (let depth = $from.depth; depth >= 1; depth--) {
    if ($from.node(depth).type.name === 'codeBlock') return true
  }
  return $from.marks().some((mark) => mark.type.name === 'code')
}

export function getPageLinkContextAt(
  selection: Selection
): PageLinkContext | null {
  if (selection.from !== selection.to || inCode(selection)) return null

  const $from = selection.$from
  const textBefore = $from.parent.textContent.slice(0, $from.parentOffset)
  const trigger = textBefore.lastIndexOf('[[')
  if (trigger === -1) return null
  if (trigger > 0 && !/\s/.test(textBefore[trigger - 1])) return null

  const query = textBefore.slice(trigger + 2)
  // Alias and heading authoring are existing manual modes. Once either
  // delimiter appears this picker steps aside instead of stealing keystrokes.
  if (/[\]|#\r\n]/.test(query)) return null

  const blockStart = $from.start()
  return {
    triggerPos: blockStart + trigger,
    query,
    from: blockStart + trigger,
    to: $from.pos
  }
}

export function getPageLinkContext(state: EditorState): PageLinkContext | null {
  return getPageLinkContextAt(state.selection)
}

export function pageLinkPath(item: PageLinkItem): string {
  return [item.notebook, item.section, item.page].filter(Boolean).join('/')
}

export function rankPageLinks(
  items: readonly PageLinkItem[],
  query: string
): PageLinkItem[] {
  return items
    .map((item) => ({ item, score: fuzzyScore(query, item.page) }))
    .filter(
      (entry): entry is typeof entry & { score: number } => entry.score !== null
    )
    .sort(
      (a, b) =>
        a.score - b.score ||
        normalizeSearch(a.item.page).localeCompare(
          normalizeSearch(b.item.page)
        ) ||
        pageLinkPath(a.item).localeCompare(pageLinkPath(b.item))
    )
    .map((entry) => entry.item)
}

export async function resolvePageLinkTarget(
  item: PageLinkItem,
  resolve: PageLinkResolver
): Promise<string> {
  const fullPath = pageLinkPath(item)
  const result = await resolve(fullPath)
  if (!result?.exists || !result.shortest) {
    throw new Error('The selected page could not be resolved')
  }
  return result.shortest
}

export function insertPageLinkSuggestion(
  editor: Editor,
  target: string,
  alias: string | null = null
): boolean {
  const ctx = getPageLinkContext(editor.state)
  const nodeType = editor.schema.nodes.pageLinkNode
  if (!ctx || !nodeType || !target) return false

  const node = nodeType.create({ target, heading: null, alias: alias || null })
  const tr = editor.state.tr.delete(ctx.from, ctx.to).insert(ctx.from, node)
  const after = ctx.from + node.nodeSize
  tr.setSelection(TextSelection.create(tr.doc, after, after))
  tr.setMeta(pageLinkSuggestKey, { escape: true })
  editor.view.dispatch(tr)
  return true
}

export async function applyPageLinkSuggestion(
  editor: Editor,
  item: PageLinkItem,
  resolve: PageLinkResolver,
  alias: string | null = null
): Promise<boolean> {
  const target = await resolvePageLinkTarget(item, resolve)
  return insertPageLinkSuggestion(editor, target, alias)
}

interface PageLinkSuggestState {
  context: PageLinkContext | null
  suppressed: boolean
}

const pageLinkSuggestKey = new PluginKey<PageLinkSuggestState>(
  'siltPageLinkSuggest'
)

export interface PageLinkSuggestOptions {
  items: () => readonly PageLinkItem[]
  onChange: (ctx: PageLinkContext | null) => void
  onNavigate: (direction: 1 | -1) => void
  onSelectActive: () => void
}

export const PageLinkSuggest = Extension.create<PageLinkSuggestOptions>({
  name: 'siltPageLinkSuggest',

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
      new Plugin<PageLinkSuggestState>({
        key: pageLinkSuggestKey,
        state: {
          init: () => ({ context: null, suppressed: false }),
          apply(tr, old, _oldState, newState) {
            const escape =
              (
                tr.getMeta(pageLinkSuggestKey) as
                  { escape?: boolean } | undefined
              )?.escape === true
            let suppressed = old.suppressed
            if (tr.docChanged) suppressed = false
            if (escape) suppressed = true
            return {
              context: suppressed
                ? null
                : getPageLinkContextAt(newState.selection),
              suppressed
            }
          }
        },
        view() {
          return {
            update(view) {
              const ctx =
                pageLinkSuggestKey.getState(view.state)?.context ?? null
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
    const active = () => getPageLinkContext(editor.state) !== null
    const actionable = () => active() && opts.items().length > 0
    const dismiss = () =>
      editor.view.dispatch(
        editor.state.tr.setMeta(pageLinkSuggestKey, { escape: true })
      )
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
        dismiss()
        return true
      },
      Tab: () => {
        if (!active()) return false
        dismiss()
        return false
      }
    }
  }
})
