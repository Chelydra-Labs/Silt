// Serialize stage — mark-diff serializer (NodeJSON[] → markdown). Walks nodes
// left-to-right emitting open/close delimiters as the active mark set changes.
// Smart Graph nodes close all active marks, emit their token, then resume.
// Also hosts the thin Token ↔ NodeJSON[] adapters so the rest of the pipeline
// can speak either representation.

import { asString } from '../../asString'
import type { Token, MarkRef } from './tokenize'
import { tokenizeInline } from './tokenize'
import { isSafeLinkHref } from './validate'
import type { NodeJSON } from '../types'

// ---- NodeJSON <-> Token adapters ------------------------------------------
// Thin adapters between the typed Token representation and the legacy
// NodeJSON[] surface used by ProseMirror / the legacy serializer. Both
// directions live here so the rest of the pipeline can speak Token.

function tokenToNodeJSON(t: Token, inheritedMarks: MarkRef[] = []): NodeJSON[] {
  switch (t.kind) {
    case 'text': {
      const marks = [...inheritedMarks, ...t.marks]
      return [
        {
          type: 'text',
          text: t.text,
          marks: marks.length ? marks : undefined
        }
      ]
    }
    case 'embed':
      return [{ type: 'embedNode', attrs: { uuid: t.uuid } }]
    case 'blockReference':
      return [{ type: 'blockReferenceNode', attrs: { uuid: t.uuid } }]
    case 'mention':
      return [{ type: 'mentionNode', attrs: { name: t.name } }]
    case 'mathInline':
      return [{ type: 'inlineMathNode', attrs: { latex: t.latex } }]
    case 'pageLink': {
      const attrs: Record<string, unknown> = { target: t.target }
      if (t.heading) attrs.heading = t.heading
      if (t.alias) attrs.alias = t.alias
      return [{ type: 'pageLinkNode', attrs }]
    }
    case 'hardBreak':
      return [{ type: 'hardBreak' }]
    case 'mark': {
      const own: MarkRef = {
        type: t.markType,
        ...(t.attrs ? { attrs: t.attrs } : {})
      }
      const newInherited = [...inheritedMarks, own]
      const out: NodeJSON[] = []
      for (const child of t.children) {
        out.push(...tokenToNodeJSON(child, newInherited))
      }
      return out
    }
    default: {
      // Exhaustiveness guard: if a new Token variant is added without
      // updating this switch, the assignment below fails to compile.
      const _exhaustive: never = t
      return _exhaustive
    }
  }
}

function tokensToNodeJSON(tokens: Token[]): NodeJSON[] {
  const out: NodeJSON[] = []
  for (const t of tokens) {
    out.push(...tokenToNodeJSON(t))
  }
  return out
}

// Legacy tokenize helper (returns NodeJSON[] for ProseMirror). Thin wrapper
// around the new typed pipeline.
export function legacyTokenizeInline(text: string): NodeJSON[] {
  return tokensToNodeJSON(tokenizeInline(text))
}

// ---- Serialize stage: NodeJSON[] → markdown ------------------------------

// The opener syntax for each mark type.
function markOpen(mark: MarkRef): string {
  switch (mark.type) {
    case 'bold':
      return '**'
    case 'italic':
      return '*'
    case 'strike':
      return '~~'
    case 'highlight':
      return '=='
    case 'code':
      return '`'
    case 'underline':
      return '<u>'
    case 'subscript':
      return '<sub>'
    case 'superscript':
      return '<sup>'
    case 'textColor': {
      const color = mark.attrs?.color
      return color ? `<span style="color: ${asString(color)}">` : ''
    }
    case 'backgroundColor': {
      const color = mark.attrs?.color
      return color ? `<span style="background-color: ${asString(color)}">` : ''
    }
    case 'link':
      return '['
    default:
      return ''
  }
}

function markClose(mark: MarkRef): string {
  switch (mark.type) {
    case 'bold':
      return '**'
    case 'italic':
      return '*'
    case 'strike':
      return '~~'
    case 'highlight':
      return '=='
    case 'code':
      return '`'
    case 'underline':
      return '</u>'
    case 'subscript':
      return '</sub>'
    case 'superscript':
      return '</sup>'
    case 'textColor':
    case 'backgroundColor':
      return '</span>'
    case 'link': {
      const href = mark.attrs?.href as string
      return `](${isSafeLinkHref(href) ? href : ''})`
    }
    default:
      return ''
  }
}

// Serialize inline content to a markdown string using the mark-diff approach.
// Public API (preserved from #168) — NodeJSON[] is the legacy surface; new
// callers should prefer tokenizeInline + a NodeJSON adapter for symmetry.
export function serializeInlineContent(content?: NodeJSON[]): string {
  if (!content) return ''
  let result = ''
  let active: MarkRef[] = []

  function closeAll(): void {
    for (let i = active.length - 1; i >= 0; i--) {
      result += markClose(active[i])
    }
    active = []
  }

  for (const child of content) {
    if (child.text !== undefined) {
      const nodeMarks = child.marks || []
      // Longest common prefix between active and nodeMarks.
      let common = 0
      while (
        common < active.length &&
        common < nodeMarks.length &&
        active[common].type === nodeMarks[common].type
      ) {
        common++
      }
      // Close marks that diverge (reverse order).
      for (let i = active.length - 1; i >= common; i--) {
        result += markClose(active[i])
      }
      // Open new marks.
      for (let i = common; i < nodeMarks.length; i++) {
        result += markOpen(nodeMarks[i])
      }
      active = nodeMarks
      result += child.text
    } else if (child.type === 'embedNode') {
      closeAll()
      result += `{{embed:${(child.attrs?.uuid as string) || ''}}}`
    } else if (child.type === 'blockReferenceNode') {
      closeAll()
      result += `((${(child.attrs?.uuid as string) || ''}))`
    } else if (child.type === 'mentionNode') {
      closeAll()
      result += `@[${(child.attrs?.name as string) || ''}]`
    } else if (child.type === 'inlineMathNode') {
      closeAll()
      result += `$${(child.attrs?.latex as string) || ''}$`
    } else if (child.type === 'pageLinkNode') {
      closeAll()
      let link = `[[${(child.attrs?.target as string) || ''}`
      if (child.attrs?.heading) link += `#${asString(child.attrs.heading)}`
      if (child.attrs?.alias) link += `|${asString(child.attrs.alias)}`
      link += ']]'
      result += link
    } else if (child.type === 'hardBreak') {
      // Soft break inside one prose line — HTML <br>, not a bare newline (#828).
      closeAll()
      result += '<br>'
    } else if (child.content) {
      closeAll()
      result += serializeInlineContent(child.content)
    }
  }
  closeAll()
  return result
}
