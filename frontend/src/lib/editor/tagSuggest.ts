// TagSuggest — `#` tag-path typeahead.
//
// Tags remain literal markdown text. The extension only owns trigger detection
// and keyboard routing; the host loads the indexed hierarchy and renders the
// shared popup. Pure helpers keep ranking and replacement testable in jsdom.

import { Extension } from '@tiptap/core'
import type { Editor } from '@tiptap/core'
import { Plugin, PluginKey, TextSelection } from '@tiptap/pm/state'
import type { EditorState, Selection } from '@tiptap/pm/state'
import { fuzzyScore } from '../navigationCatalog'

const TAG_QUERY_RE = /^[\p{L}\p{N}_/-]*$/u

export interface TagContext {
  triggerPos: number
  query: string
  from: number
  to: number
}

export interface TagTreeNode {
  name: string
  path: string
  count: number
  children: TagTreeNode[]
}

export interface TagItem {
  path: string
  count: number
}

function inCode(selection: Selection): boolean {
  const $from = selection.$from
  for (let depth = $from.depth; depth >= 1; depth--) {
    if ($from.node(depth).type.name === 'codeBlock') return true
  }
  return $from.marks().some((mark) => mark.type.name === 'code')
}

export function getTagContextAt(selection: Selection): TagContext | null {
  if (selection.from !== selection.to || inCode(selection)) return null

  const $from = selection.$from
  const textBefore = $from.parent.textContent.slice(0, $from.parentOffset)
  const trigger = textBefore.lastIndexOf('#')
  if (trigger === -1) return null
  if (trigger > 0 && !/\s/.test(textBefore[trigger - 1])) return null

  const query = textBefore.slice(trigger + 1)
  if (!TAG_QUERY_RE.test(query)) return null

  const blockStart = $from.start()
  return {
    triggerPos: blockStart + trigger,
    query,
    from: blockStart + trigger,
    to: $from.pos
  }
}

export function getTagContext(state: EditorState): TagContext | null {
  return getTagContextAt(state.selection)
}

export function flattenTagHierarchy(nodes: readonly TagTreeNode[]): TagItem[] {
  const byPath = new Map<string, TagItem>()
  const visit = (items: readonly TagTreeNode[]) => {
    for (const item of items) {
      if (item.path) {
        const previous = byPath.get(item.path)
        if (!previous || item.count > previous.count) {
          byPath.set(item.path, { path: item.path, count: item.count })
        }
      }
      visit(item.children ?? [])
    }
  }
  visit(nodes)
  return [...byPath.values()]
}

function compareText(a: string, b: string): number {
  return a.localeCompare(b, undefined, { sensitivity: 'base' })
}

export function rankTags(
  tags: readonly TagItem[],
  recentTags: readonly string[]
): TagItem[] {
  const recentOrder = new Map(
    recentTags.map((path, index) => [path.toLocaleLowerCase(), index])
  )
  return tags.slice().sort((a, b) => {
    const aRank = recentOrder.get(a.path.toLocaleLowerCase()) ?? Infinity
    const bRank = recentOrder.get(b.path.toLocaleLowerCase()) ?? Infinity
    return aRank - bRank || b.count - a.count || compareText(a.path, b.path)
  })
}

export function filterTags(
  tags: readonly TagItem[],
  query: string,
  recentTags: readonly string[] = []
): TagItem[] {
  const ranked = rankTags(tags, recentTags)
  const normalizedQuery = query.toLocaleLowerCase()
  if (!normalizedQuery) return ranked

  const prefix = ranked.filter((tag) =>
    tag.path.toLocaleLowerCase().startsWith(normalizedQuery)
  )
  if (prefix.length) return prefix

  return ranked
    .map((tag, baseRank) => ({
      tag,
      baseRank,
      score: fuzzyScore(query, tag.path)
    }))
    .filter(
      (entry): entry is typeof entry & { score: number } => entry.score !== null
    )
    .sort((a, b) => a.score - b.score || a.baseRank - b.baseRank)
    .map((entry) => entry.tag)
}

export function applyTagSuggestion(editor: Editor, path: string): boolean {
  const ctx = getTagContext(editor.state)
  if (!ctx || !path || !TAG_QUERY_RE.test(path)) return false

  const text = `#${path}`
  const tr = editor.state.tr
    .insertText(text, ctx.from, ctx.to)
    .setMeta(tagSuggestKey, { escape: true })
  const after = ctx.from + text.length
  tr.setSelection(TextSelection.create(tr.doc, after, after))
  editor.view.dispatch(tr)
  return true
}

interface TagSuggestState {
  context: TagContext | null
  suppressed: boolean
}

const tagSuggestKey = new PluginKey<TagSuggestState>('siltTagSuggest')

export interface TagSuggestOptions {
  items: () => readonly TagItem[]
  onChange: (ctx: TagContext | null) => void
  onNavigate: (direction: 1 | -1) => void
  onSelectActive: () => void
}

export const TagSuggest = Extension.create<TagSuggestOptions>({
  name: 'siltTagSuggest',

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
      new Plugin<TagSuggestState>({
        key: tagSuggestKey,
        state: {
          init: () => ({ context: null, suppressed: false }),
          apply(tr, old, _oldState, newState) {
            const escape =
              (tr.getMeta(tagSuggestKey) as { escape?: boolean } | undefined)
                ?.escape === true
            let suppressed = old.suppressed
            if (tr.docChanged) suppressed = false
            if (escape) suppressed = true
            return {
              context: suppressed ? null : getTagContextAt(newState.selection),
              suppressed
            }
          }
        },
        view() {
          return {
            update(view) {
              const ctx = tagSuggestKey.getState(view.state)?.context ?? null
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
    const active = () => getTagContext(editor.state) !== null
    const actionable = () => active() && opts.items().length > 0
    const dismiss = () =>
      editor.view.dispatch(
        editor.state.tr.setMeta(tagSuggestKey, { escape: true })
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
