// ProseMirror / TipTap node schema for Silt's block model.
//
// Each Silt block type (TASK / NOTE / HEADER) maps to a ProseMirror block node
// whose attrs carry the block identity and metadata. The editor surface (Phase 3)
// and NodeViews (Phase 4/5) build on this schema; this file only defines the
// node types and their attrs so the converter layer (converters.ts) can
// produce/consume doc JSON that a TipTap editor accepts.
//
// Design notes:
// - `id` is the block UUID (parser.ParsedBlock.ID). It is the node identity, NOT
//   content. Pasted/duplicated nodes get a fresh UUID via uniqueIdPlugin.ts so
//   the blocks-table PK and embed/reference resolution stay consistent.
// - `parent_id` is NOT a node attr: parentage is derived once by the Go
//   parser (its single validated hierarchy derivation) and consumed as-is
//   from the block payload. The editor keeps `depth` as the only structural
//   attr it owns, so stored depth and server-side parentage cannot drift.
// - `line_number` is NOT a node attr: it is reassigned by doc order in
//   docToBlocks. Line numbers are positional, not semantic.
// - Notes carry a `bullet` attr ('- ', '* ', '+ ', or '' for plain prose) so
//   the Go serializer (renderBlock) preserves the original bullet marker.
//   The editor-created default is '' (blank prose) — explicit actions (input
//   rules, Enter-inheritance, slash-Note) set a bullet when the user wants one.

import {
  Node,
  Mark,
  mergeAttributes,
  InputRule,
  markInputRule,
  markPasteRule
} from '@tiptap/core'
import { newlineInCode } from '@tiptap/pm/commands'
import { Slice, Fragment } from '@tiptap/pm/model'
import type { Node as PMNode, ResolvedPos } from '@tiptap/pm/model'
import { TextSelection } from '@tiptap/pm/state'
import type { EditorView } from '@tiptap/pm/view'
import { DEFAULT_TASK_PRIORITY } from './types'
import {
  Details,
  DetailsContent,
  DetailsSummary
} from '@tiptap/extension-details'
import { Table } from '@tiptap/extension-table'
import { TableRow } from '@tiptap/extension-table-row'
import { TableCell } from '@tiptap/extension-table-cell'
import { TableHeader } from '@tiptap/extension-table-header'
import Subscript from '@tiptap/extension-subscript'
import Superscript from '@tiptap/extension-superscript'
import { colorFromStyle } from './paste/styleValues'
import { classifyPastedHTML } from './paste/normalizePastedHTML'
import { resetPlainPasteShiftState } from './plainPaste'

// ---- Highlight mark (==text== / background color) ------------------------
// Inline highlight. The default (no color) renders a <mark> element whose
// background comes from the `.ProseMirror mark` rule consuming the per-theme
// --color-editor-highlight tint; an explicit color renders the same <mark>
// with an inline `background-color` style that overrides the token. Serialized
// on-disk as `==text==` (default) or
// `<span style="background-color: #hex">text</span>` (explicit color), both of
// which the Go parser preserves verbatim in clean_text. Consolidates the former
// separate Highlight (==text==) and BackgroundColor marks into one (#858).
export const Highlight = Mark.create({
  name: 'highlight',
  inclusive: true,
  addAttributes() {
    return {
      color: {
        default: null,
        parseHTML: (el) => colorFromStyle(el.style.backgroundColor),
        renderHTML: (attrs) =>
          attrs.color ? { style: `background-color: ${attrs.color}` } : {}
      }
    }
  },
  parseHTML() {
    return [
      { tag: 'mark' },
      {
        // A `style` rule rather than `span[style]`: ProseMirror's tag rules are
        // first-match-wins per element, so an external run carrying both a
        // text color and a background color would lose one of the two marks.
        // Style rules are matched per property, so both coexist.
        style: 'background-color',
        getAttrs: (value) => {
          const color = colorFromStyle(value)
          return color ? { color } : false
        }
      }
    ]
  },
  renderHTML({ HTMLAttributes }) {
    return ['mark', mergeAttributes(HTMLAttributes), 0]
  },
  // Restore the ==text== input/paste rules that the former TipTap Highlight
  // extension provided (lost when the mark was inlined in #858). Typing or
  // pasting `==text==` wraps the inner text in a default highlight mark
  // (color: null → the themed tint); toolbar, Mod+Shift+H, and on-disk load
  // paths are unaffected. Verbatim from @tiptap/extension-highlight 3.29.2.
  addInputRules() {
    return [
      markInputRule({
        find: /(?:^|\s)(==(?!\s+==)((?:[^=]+))==(?!\s+==))$/,
        type: this.type
      })
    ]
  },
  addPasteRules() {
    return [
      markPasteRule({
        find: /(?:^|\s)(==(?!\s+==)((?:[^=]+))==(?!\s+==))/g,
        type: this.type
      })
    ]
  }
})

// Subscript/Superscript are NOT in StarterKit and are added alongside the
// custom Highlight mark above. All three round-trip through clean_text (the Go
// parser preserves HTML/tags and ==...== verbatim — clean_text is opaque to
// Go); the converter (converters.ts) handles parse/serialize symmetrically.
export const SiltInlineMarkExtensions = [Highlight, Subscript, Superscript]

// ---- TextColor mark (#170) -----------------------------------------------
// Inline character color. Serialized on-disk as
// `<span style="color: #hex">text</span>`. The Go parser preserves HTML in
// clean_text verbatim, so the span round-trips with zero parser changes.
export const TextColor = Mark.create({
  name: 'textColor',
  inclusive: true,
  addAttributes() {
    return {
      color: {
        default: null,
        parseHTML: (el) => colorFromStyle(el.style.color),
        renderHTML: (attrs) =>
          attrs.color ? { style: `color: ${attrs.color}` } : {}
      }
    }
  },
  parseHTML() {
    return [
      {
        // `style` (not `span[style]`) so a run with both color and
        // background-color keeps this mark alongside Highlight — tag rules
        // match only the first rule, style rules match their own property.
        style: 'color',
        getAttrs: (value) => {
          const color = colorFromStyle(value)
          return color ? { color } : false
        }
      }
    ]
  },
  renderHTML({ HTMLAttributes }) {
    return ['span', mergeAttributes(HTMLAttributes), 0]
  }
})

// Color marks: TextColor (foreground) is its own mark; background highlighting
// is the Highlight mark above (consolidated, #858). Both are HTML spans the Go
// parser preserves verbatim in clean_text.
export const SiltColorMarkExtensions = [TextColor]

// ---- TaskBlock -----------------------------------------------------------
// Renders on-disk as:
//   <indent>- [x] STATUS TASK [owner](start,due)#priority desc <!-- id: uuid -->
export const TaskBlock = Node.create({
  name: 'taskBlock',
  group: 'block',
  content: 'inline*',
  defining: true,
  isolating: true,

  addAttributes() {
    return {
      id: {
        default: null,
        parseHTML: (el) => el.getAttribute('data-id') || null,
        renderHTML: (attrs) => (attrs.id ? { 'data-id': attrs.id } : {})
      },
      depth: {
        default: 0,
        parseHTML: (el) => Number(el.getAttribute('data-depth') || 0),
        renderHTML: (attrs) => ({ 'data-depth': String(attrs.depth) })
      },
      status: {
        default: 'TODO',
        parseHTML: (el) => el.getAttribute('data-status') || 'TODO',
        renderHTML: (attrs) => ({ 'data-status': attrs.status })
      },
      owner: {
        default: '',
        parseHTML: (el) => el.getAttribute('data-owner') || '',
        renderHTML: (attrs) =>
          attrs.owner ? { 'data-owner': attrs.owner } : {}
      },
      start_date: {
        default: '',
        parseHTML: (el) => el.getAttribute('data-start-date') || '',
        renderHTML: (attrs) =>
          attrs.start_date ? { 'data-start-date': attrs.start_date } : {}
      },
      due_date: {
        default: '',
        parseHTML: (el) => el.getAttribute('data-due-date') || '',
        renderHTML: (attrs) =>
          attrs.due_date ? { 'data-due-date': attrs.due_date } : {}
      },
      recurrence: {
        default: '',
        parseHTML: (el) => el.getAttribute('data-recurrence') || '',
        renderHTML: (attrs) =>
          attrs.recurrence ? { 'data-recurrence': attrs.recurrence } : {}
      },
      priority: {
        default: DEFAULT_TASK_PRIORITY,
        parseHTML: (el) =>
          Number(el.getAttribute('data-priority')) || DEFAULT_TASK_PRIORITY,
        renderHTML: (attrs) => ({ 'data-priority': String(attrs.priority) })
      },
      file_date: {
        default: '',
        parseHTML: (el) => el.getAttribute('data-file-date') || '',
        renderHTML: (attrs) =>
          attrs.file_date ? { 'data-file-date': attrs.file_date } : {}
      }
    }
  },

  parseHTML() {
    return [{ tag: 'div[data-type="task"]' }]
  },

  renderHTML({ HTMLAttributes }) {
    return ['div', mergeAttributes({ 'data-type': 'task' }, HTMLAttributes), 0]
  },

  addInputRules() {
    return [
      // Markdown-style checkbox shortcut: typing "[]", "[ ]", or "[x]"
      // followed by a space converts the current block into a task (#262).
      // Matches inside a noteBlock or headerBlock; the matched text is
      // deleted and the node type is swapped via setNodeMarkup so inline
      // content survives.
      new InputRule({
        find: /^\s*\[([ xX]?)]\s$/,
        handler: (({
          state,
          range,
          match
        }: Parameters<NonNullable<InputRule['handler']>>[0]) => {
          const $start = state.doc.resolve(range.from)
          if ($start.parentOffset !== range.from - $start.start()) {
            return null
          }
          const depth = $start.depth
          const nodePos = $start.before(depth)
          const node = $start.node(depth)
          if (
            node.type.name !== 'noteBlock' &&
            node.type.name !== 'headerBlock'
          ) {
            return null
          }
          const taskType = state.schema.nodes.taskBlock
          const isDone = match[1] === 'x' || match[1] === 'X'
          const tr = state.tr.delete(range.from, range.to)
          tr.setNodeMarkup(nodePos, taskType, {
            id: node.attrs.id,
            depth: node.attrs.depth || 0,
            status: isDone ? 'DONE' : 'TODO',
            owner: '',
            start_date: '',
            due_date: '',
            recurrence: '',
            priority: DEFAULT_TASK_PRIORITY,
            file_date: node.attrs.file_date || ''
          })
          return tr
        }) as unknown as InputRule['handler']
      })
    ]
  }
})

// Containers whose serializers depend on `paragraph` children: table cells
// (TipTap table commands hard-depend on schema.nodes.paragraph) and callouts
// (serializeCalloutToText reads the title from children[0]). The paste helpers
// below use the same set so the parse rule and the context-aware paste path
// cannot drift apart.
const PARAGRAPH_CONTAINER_SELECTOR = 'td, th, [data-type="callout"]'
const PARAGRAPH_CONTAINER_NODES = new Set([
  'tableCell',
  'tableHeader',
  'calloutBlock'
])

function inParagraphContainer($pos: ResolvedPos): boolean {
  for (let d = $pos.depth; d > 0; d--) {
    if (PARAGRAPH_CONTAINER_NODES.has($pos.node(d).type.name)) return true
  }
  return false
}

// ---- NoteBlock -----------------------------------------------------------
// Renders on-disk as: <indent><bullet?>desc <!-- id: uuid -->
export const NoteBlock = Node.create({
  name: 'noteBlock',
  group: 'block',
  content: 'inline*',
  defining: true,
  isolating: true,

  addAttributes() {
    return {
      id: {
        default: null,
        parseHTML: (el) => el.getAttribute('data-id') || null,
        renderHTML: (attrs) => (attrs.id ? { 'data-id': attrs.id } : {})
      },
      depth: {
        default: 0,
        parseHTML: (el) => Number(el.getAttribute('data-depth') || 0),
        renderHTML: (attrs) => ({ 'data-depth': String(attrs.depth) })
      },
      bullet: {
        // '- ', '* ', '+ ', or '' (plain prose — the default for new pages)
        default: '',
        parseHTML: (el) => {
          const b = el.getAttribute('data-bullet')
          return b !== null ? b : ''
        },
        renderHTML: (attrs) => ({ 'data-bullet': attrs.bullet })
      },
      // Blockquote prefix (#188). A `> ` (or nested `>> `, `>>> `) prefix is
      // just another recognized marker style, parallel to `bullet`. Empty
      // string = not a quote. The marker is stored verbatim so the exact `>`
      // run round-trips and the NodeView can render nested left borders.
      quote: {
        default: '',
        parseHTML: (el) => el.getAttribute('data-quote') || '',
        renderHTML: (attrs) =>
          attrs.quote ? { 'data-quote': attrs.quote } : {}
      },
      // Block-level text alignment (#173). Only NOTE and HEADER support it;
      // TASK blocks do not get this attr. Default 'left' = no marker emitted.
      align: {
        default: 'left',
        parseHTML: (el) => el.getAttribute('data-align') || 'left',
        renderHTML: (attrs) =>
          attrs.align && attrs.align !== 'left'
            ? { 'data-align': attrs.align }
            : {}
      },
      file_date: {
        default: '',
        parseHTML: (el) => el.getAttribute('data-file-date') || '',
        renderHTML: (attrs) =>
          attrs.file_date ? { 'data-file-date': attrs.file_date } : {}
      }
    }
  },

  parseHTML() {
    return [
      { tag: 'div[data-type="note"]' },
      {
        // External/plain paragraphs become notes so pasted prose keeps its
        // block boundary. priority 60 beats StarterKit's paragraph rule
        // ({ tag: 'p' }, default priority 50), which is registered first and
        // would otherwise win.
        //
        // Paragraph-required containers are the exception: tableCell content
        // and calloutBlock children must stay real `paragraph` nodes (TipTap's
        // table commands and serializeCalloutToText depend on them). Internal
        // clipboard re-parse has to keep that invariant too, so whole-callout
        // copy/paste does not demote its title to a noteBlock. `closest` also
        // covers nested callouts.
        tag: 'p',
        priority: 60,
        getAttrs: (el) =>
          el.closest(PARAGRAPH_CONTAINER_SELECTOR) ? false : null
      }
    ]
  },

  renderHTML({ HTMLAttributes }) {
    return ['div', mergeAttributes({ 'data-type': 'note' }, HTMLAttributes), 0]
  },

  addInputRules() {
    // Markdown-as-you-type (#657). Order matters: callout before generic quote.
    // Handlers mirror task/bullet rules — only fire at start of a noteBlock.
    type IRArgs = Parameters<NonNullable<InputRule['handler']>>[0]
    const atNoteStart = (state: IRArgs['state'], range: IRArgs['range']) => {
      const $start = state.doc.resolve(range.from)
      if ($start.parentOffset !== range.from - $start.start()) return null
      const depth = $start.depth
      const nodePos = $start.before(depth)
      const node = $start.node(depth)
      if (node.type.name !== 'noteBlock') return null
      return { nodePos, node }
    }

    const toCallout = (
      state: IRArgs['state'],
      nodePos: number,
      node: { attrs: Record<string, unknown>; nodeSize: number },
      variantRaw: string
    ) => {
      const calloutType = state.schema.nodes.calloutBlock
      const paragraphType = state.schema.nodes.paragraph
      if (!calloutType) return null
      const variant = normalizeCalloutVariant(variantRaw)
      const children = paragraphType ? [paragraphType.create()] : []
      const callout = calloutType.create(
        {
          id: node.attrs.id,
          variant,
          file_date: node.attrs.file_date || ''
        },
        children
      )
      return state.tr.replaceWith(nodePos, nodePos + node.nodeSize, callout)
    }

    return [
      // Full paste/type path: `> [!note] ` before the generic `> ` rule can fire
      // on a single keystroke (e.g. paste). Character-by-character typing hits
      // `> ` first, then the quote-body rule below.
      new InputRule({
        find: /^>\s\[!([a-zA-Z0-9_-]+)\]\s$/,
        handler: (({ state, range, match }: IRArgs) => {
          const ctx = atNoteStart(state, range)
          if (!ctx) return null
          return toCallout(state, ctx.nodePos, ctx.node, match[1] ?? 'note')
        }) as unknown as InputRule['handler']
      }),
      // After `> ` became a quote, typing `[!warning] ` promotes to callout.
      new InputRule({
        find: /^\[!([a-zA-Z0-9_-]+)\]\s$/,
        handler: (({ state, range, match }: IRArgs) => {
          const ctx = atNoteStart(state, range)
          if (!ctx || !ctx.node.attrs.quote) return null
          return toCallout(state, ctx.nodePos, ctx.node, match[1] ?? 'note')
        }) as unknown as InputRule['handler']
      }),
      // Blockquote: `> ` → noteBlock quote attr (#657 / #188 model).
      new InputRule({
        find: /^>\s$/,
        handler: (({ state, range }: IRArgs) => {
          const ctx = atNoteStart(state, range)
          if (!ctx) return null
          const tr = state.tr.delete(range.from, range.to)
          tr.setNodeMarkup(ctx.nodePos, undefined, {
            ...ctx.node.attrs,
            quote: '> ',
            bullet: ''
          })
          return tr
        }) as unknown as InputRule['handler']
      }),
      // Headings: `# ` … `###### ` → headerBlock depth (#657).
      new InputRule({
        find: /^(#{1,6})\s$/,
        handler: (({ state, range, match }: IRArgs) => {
          const ctx = atNoteStart(state, range)
          if (!ctx) return null
          const headerType = state.schema.nodes.headerBlock
          if (!headerType) return null
          const level = match[1]?.length ?? 1
          const tr = state.tr.delete(range.from, range.to)
          tr.setNodeMarkup(ctx.nodePos, headerType, {
            id: ctx.node.attrs.id,
            depth: level,
            align: ctx.node.attrs.align || 'left',
            file_date: ctx.node.attrs.file_date || ''
          })
          return tr
        }) as unknown as InputRule['handler']
      }),
      // Code fence: ``` or ```lang + space/newline → codeBlock (#657).
      new InputRule({
        find: /^```([a-zA-Z0-9_+-]*)[\s\n]$/,
        handler: (({ state, range, match }: IRArgs) => {
          const ctx = atNoteStart(state, range)
          if (!ctx) return null
          const codeType = state.schema.nodes.codeBlock
          if (!codeType) return null
          const language = match[1] ?? ''
          const codeNode = codeType.create({
            id: ctx.node.attrs.id,
            language,
            file_date: ctx.node.attrs.file_date || ''
          })
          const tr = state.tr.replaceWith(
            ctx.nodePos,
            ctx.nodePos + ctx.node.nodeSize,
            codeNode
          )
          return tr
        }) as unknown as InputRule['handler']
      }),
      new InputRule({
        find: /^\s*([-*+])\s$/,
        handler: (({
          state,
          range,
          match
        }: Parameters<NonNullable<InputRule['handler']>>[0]) => {
          const $start = state.doc.resolve(range.from)
          if ($start.parentOffset !== range.from - $start.start()) {
            return null
          }
          const depth = $start.depth
          const nodePos = $start.before(depth)
          const node = $start.node(depth)
          if (node.type.name !== 'noteBlock') {
            return null
          }
          const tr = state.tr.delete(range.from, range.to)
          tr.setNodeAttribute(nodePos, 'bullet', match[1] + ' ')
          return tr
        }) as unknown as InputRule['handler']
      }),
      new InputRule({
        find: /^\s*(\d+[.)])\s$/,
        handler: (({
          state,
          range,
          match
        }: Parameters<NonNullable<InputRule['handler']>>[0]) => {
          const $start = state.doc.resolve(range.from)
          if ($start.parentOffset !== range.from - $start.start()) {
            return null
          }
          const depth = $start.depth
          const nodePos = $start.before(depth)
          const node = $start.node(depth)
          if (node.type.name !== 'noteBlock') {
            return null
          }
          const tr = state.tr.delete(range.from, range.to)
          tr.setNodeAttribute(nodePos, 'bullet', match[1] + ' ')
          return tr
        }) as unknown as InputRule['handler']
      })
    ]
  }
})

// ---- HeaderBlock ---------------------------------------------------------
// Renders on-disk as: <hashes> desc <!-- id: uuid -->
// `depth` is the heading level (1-6), matching block.Depth for headers.
export const HeaderBlock = Node.create({
  name: 'headerBlock',
  group: 'block',
  content: 'inline*',
  defining: true,
  isolating: true,

  addAttributes() {
    return {
      id: {
        default: null,
        parseHTML: (el) => el.getAttribute('data-id') || null,
        renderHTML: (attrs) => (attrs.id ? { 'data-id': attrs.id } : {})
      },
      depth: {
        default: 1,
        // External pastes carry the level in the tag (`<h3>`), not in
        // data-depth. TipTap injects attribute parsers into every rule and
        // they override rule-level attrs, so both shapes are resolved here.
        parseHTML: (el) => {
          const explicit = el.getAttribute('data-depth')
          if (explicit !== null && explicit !== '') {
            const parsed = Number(explicit)
            if (Number.isFinite(parsed)) return parsed
          }
          const level = /^h([1-6])$/.exec(el.tagName.toLowerCase())
          return level ? Number(level[1]) : 1
        },
        renderHTML: (attrs) => ({ 'data-depth': String(attrs.depth) })
      },
      // Block-level text alignment (#173).
      align: {
        default: 'left',
        parseHTML: (el) => el.getAttribute('data-align') || 'left',
        renderHTML: (attrs) =>
          attrs.align && attrs.align !== 'left'
            ? { 'data-align': attrs.align }
            : {}
      },
      file_date: {
        default: '',
        parseHTML: (el) => el.getAttribute('data-file-date') || '',
        renderHTML: (attrs) =>
          attrs.file_date ? { 'data-file-date': attrs.file_date } : {}
      }
    }
  },

  parseHTML() {
    return [
      { tag: 'div[data-type="header"]' },
      // External headings land as headerBlocks at the matching depth. StarterKit's
      // heading extension is disabled on every Silt surface, so there is no
      // competing rule here.
      { tag: 'h1' },
      { tag: 'h2' },
      { tag: 'h3' },
      { tag: 'h4' },
      { tag: 'h5' },
      { tag: 'h6' }
    ]
  },

  renderHTML({ HTMLAttributes }) {
    return [
      'div',
      mergeAttributes({ 'data-type': 'header' }, HTMLAttributes),
      0
    ]
  }
})

// ---- Shared paste path ---------------------------------------------------
// `clipboardTextParser` only runs when the clipboard has no HTML (or the paste
// is plain). ProseMirror's default would synthesize one `<p>` per line and feed
// it through the parser above, which turns a single-line Ctrl+V into an
// isolating noteBlock split. Handling text here keeps the native expectation:
// one line inserts inline at the caret, multiple lines become one block each.
export function siltClipboardTextParser(
  text: string,
  $context: ResolvedPos,
  _plain: boolean,
  view: EditorView
): Slice {
  const schema = view.state.schema
  const lines = text.replace(/\r\n?/g, '\n').split('\n')
  // A terminal/VS Code copy appends a newline; drop one trailing empty line so
  // "X\n" stays a single inline line instead of becoming a block paste.
  if (lines.length > 1 && lines[lines.length - 1] === '') lines.pop()

  const marks = $context.marks()
  if (lines.length === 1) {
    return new Slice(
      Fragment.from(lines[0] ? schema.text(lines[0], marks) : []),
      0,
      0
    )
  }

  const blockType = inParagraphContainer($context)
    ? schema.nodes.paragraph
    : schema.nodes.noteBlock
  const blocks = lines.map((line) =>
    blockType.create(null, line ? schema.text(line, marks) : null)
  )
  return new Slice(Fragment.from(blocks), 0, 0)
}

// HTML paste into a paragraph-required container has no ancestry to inspect:
// the pasted `<p>` is parsed while the caret already sits inside a cell or
// callout, so the DOM fragment carries no `td`/callout wrapper. Normalize the
// parsed slice against the paste target's context instead (this covers partial
// copies out of a cell/callout). Drops are NOT covered: ProseMirror transforms
// a dragged slice before the selection moves to the drop target, so the check
// below sees the drag origin, not the container being dropped into.
export function siltTransformPasted(slice: Slice, view: EditorView): Slice {
  const paragraphType = view.state.schema.nodes.paragraph
  if (!paragraphType || !inParagraphContainer(view.state.selection.$from)) {
    return slice
  }

  let changed = false
  const blocks: PMNode[] = []
  slice.content.forEach((node) => {
    if (node.type.name === 'noteBlock') {
      blocks.push(paragraphType.create(null, node.content))
      changed = true
    } else {
      blocks.push(node)
    }
  })
  return changed
    ? new Slice(Fragment.from(blocks), slice.openStart, slice.openEnd)
    : slice
}

// Replacing an *empty* textblock with a block slice is the one case
// `replaceSelection` cannot do itself: the surrounding noteBlock/callout/
// cell is isolating, so the insertion leaves the empty block behind (a blank
// line above pasted content). Handle that case explicitly.
export function siltHandlePaste(
  view: EditorView,
  _event: ClipboardEvent,
  slice: Slice
): boolean {
  const { selection } = view.state
  const $from = selection.$from
  if (!selection.empty || !$from.parent.isTextblock) return false
  // Only prose blocks: replacing an empty task/header block with prose would
  // silently change the block's type and drop its metadata.
  if (
    $from.parent.type.name !== 'noteBlock' &&
    $from.parent.type.name !== 'paragraph'
  ) {
    return false
  }
  if ($from.parent.content.size !== 0) return false
  if (slice.openStart || slice.openEnd || slice.content.childCount === 0) {
    return false
  }
  let blockContent = true
  slice.content.forEach((node) => {
    if (!node.isBlock) blockContent = false
  })
  if (!blockContent) return false

  const from = $from.before($from.depth)
  const to = $from.after($from.depth)
  // Replacing the empty parent wholesale would also discard the markers the
  // user typed on it (bullet, quote, indent, align) and its persisted
  // `file_date` — the same metadata loss the guard above avoids for
  // task/header blocks. Carry them onto the pasted notes; ids stay unset so
  // UniqueBlockIds mints fresh ones.
  const parent = $from.parent
  let content = slice.content
  if (parent.type.name === 'noteBlock') {
    const inherited = {
      bullet: parent.attrs.bullet,
      quote: parent.attrs.quote,
      depth: parent.attrs.depth,
      align: parent.attrs.align,
      file_date: parent.attrs.file_date
    }
    const marked =
      inherited.bullet ||
      inherited.quote ||
      inherited.depth ||
      inherited.align !== 'left' ||
      inherited.file_date
    if (marked) {
      // Pasted list content carries its own markers. Inherit the empty
      // parent's attrs only when no note in the slice has a marker of its own:
      // overwriting markers would flatten a pasted ordered/nested list, and
      // inheriting into a mixed slice (list plus marker-less continuation
      // notes) would push those continuations to the parent's depth, out of
      // step with the list items they belong to.
      let pasteHasOwnMarkers = false
      slice.content.forEach((node) => {
        if (node.type.name !== 'noteBlock') return
        if (
          String(node.attrs.bullet ?? '') !== '' ||
          String(node.attrs.quote ?? '') !== ''
        ) {
          pasteHasOwnMarkers = true
        }
      })
      const blocks: PMNode[] = []
      slice.content.forEach((node) => {
        if (node.type.name !== 'noteBlock') {
          blocks.push(node)
          return
        }
        blocks.push(
          node.type.create(
            pasteHasOwnMarkers ? node.attrs : inherited,
            node.content,
            node.marks
          )
        )
      })
      content = Fragment.from(blocks)
    }
  }

  view.dispatch(
    view.state.tr
      .replace(from, to, new Slice(content, 0, 0))
      .scrollIntoView()
      .setMeta('paste', true)
      .setMeta('uiEvent', 'paste')
  )
  return true
}

// ProseMirror parses `text/html` before any `handlePaste` handler runs, so an
// oversized payload would still build a multi-megabyte DOM on the main thread.
// This DOM-level hook intercepts first and inserts the plain-text clipboard
// payload instead; it must return false for every other paste so PlainPaste's
// shift-tracking hook (a plugin prop, consulted after direct editor props) and
// ProseMirror's own paste handler still run.
export function siltHandlePasteEvent(
  view: EditorView,
  event: ClipboardEvent
): boolean {
  const html = event.clipboardData?.getData('text/html') ?? ''
  if (classifyPastedHTML(html) !== 'text') return false
  event.preventDefault()
  // The DOM hook that snapshots the plain-paste flag is bypassed when this
  // handler claims the event, so clear the flag explicitly.
  resetPlainPasteShiftState()
  const text = event.clipboardData?.getData('text/plain') ?? ''
  // An oversized payload with no text alternative is still better dropped than
  // parsed; browsers mirror HTML into text/plain, so this is a safety net.
  if (text) view.pasteText(text)
  return true
}

// One object both editor surfaces spread into `createEditor`'s editorProps.
export const siltPasteProps = {
  clipboardTextParser: siltClipboardTextParser,
  transformPasted: siltTransformPasted,
  handleDOMEvents: { paste: siltHandlePasteEvent },
  handlePaste: siltHandlePaste
}

// Silt-specific block extensions. NoteBlock MUST be first — ProseMirror uses
// the first registered block type as the "default" for content normalization.
// NoteBlock (plain text) is the natural default; TaskBlock and HeaderBlock
// are opt-in types the user creates via the slash menu.
export const SiltBlockExtensions = [NoteBlock, TaskBlock, HeaderBlock]

// ---- CalloutBlock (#180) -------------------------------------------------
// An Obsidian/GFM admonition: a `>`-prefixed line whose marker is `[!variant]`.
// On-disk it is `> [!variant] message`; the converter detects the marker and
// emits this node, stripping the prefix from the displayed text. The variant
// drives the icon + accent color (CALLOUT_VARIANTS). Pure-frontend: the Go
// parser sees the line as an opaque NOTE whose clean_text starts with
// `> [!variant]`, exactly like a quote — no parser change.
export type CalloutVariant =
  'note' | 'info' | 'tip' | 'warning' | 'danger' | 'success' | 'quote'

/** Normalize typed/Obsidian aliases to a Silt callout variant (#657). */
export function normalizeCalloutVariant(raw: string): CalloutVariant {
  const key = raw.trim().toLowerCase()
  const aliases: Record<string, CalloutVariant> = {
    note: 'note',
    info: 'info',
    tip: 'tip',
    hint: 'tip',
    important: 'tip',
    warning: 'warning',
    caution: 'warning',
    attention: 'warning',
    danger: 'danger',
    error: 'danger',
    failure: 'danger',
    success: 'success',
    check: 'success',
    done: 'success',
    quote: 'quote',
    cite: 'quote'
  }
  return aliases[key] ?? 'note'
}

export const CALLOUT_VARIANTS: Record<
  CalloutVariant,
  { icon: string; label: string; accent: string; role: string }
> = {
  note: {
    icon: 'info',
    label: 'Note',
    accent: 'var(--color-accent-primary-start)',
    role: 'note'
  },
  info: {
    icon: 'campaign',
    label: 'Info',
    accent: 'var(--color-accent-secondary-start)',
    role: 'note'
  },
  tip: {
    icon: 'lightbulb',
    label: 'Tip',
    accent: 'var(--color-status-success)',
    role: 'note'
  },
  warning: {
    icon: 'warning',
    label: 'Warning',
    accent: 'var(--color-status-warn)',
    role: 'note'
  },
  danger: {
    icon: 'error',
    label: 'Danger',
    accent: 'var(--color-status-danger)',
    role: 'alert'
  },
  success: {
    icon: 'check_circle',
    label: 'Success',
    accent: 'var(--color-status-success)',
    role: 'status'
  },
  quote: {
    icon: 'format_quote',
    label: 'Quote',
    accent: 'var(--color-text-muted)',
    role: 'blockquote'
  }
}

export const CalloutBlock = Node.create({
  name: 'calloutBlock',
  group: 'block',
  // block+ lets a callout contain arbitrary block children (task lists, code
  // blocks, tables, nested callouts) like Obsidian and <details> already do.
  // This is safe because the serializer (serializeCalloutToText) emits an
  // explicit branch for every allowed block type — the same no-silent-drop
  // guarantee <details> relies on — so block content round-trips through the
  // `>`-prefixed on-disk lines instead of being flattened or lost. Plain body
  // lines parse as paragraphs, so legacy multi-paragraph callouts stay
  // byte-identical.
  content: 'block+',
  defining: true,
  isolating: true,

  addAttributes() {
    return {
      id: {
        default: null,
        parseHTML: (el) => el.getAttribute('data-id') || null,
        renderHTML: (attrs) => (attrs.id ? { 'data-id': attrs.id } : {})
      },
      variant: {
        default: 'note',
        parseHTML: (el) => el.getAttribute('data-variant') || 'note',
        renderHTML: (attrs) => ({ 'data-variant': attrs.variant })
      },
      file_date: {
        default: '',
        parseHTML: (el) => el.getAttribute('data-file-date') || '',
        renderHTML: (attrs) =>
          attrs.file_date ? { 'data-file-date': attrs.file_date } : {}
      }
    }
  },

  parseHTML() {
    return [{ tag: 'div[data-type="callout"]' }]
  },

  renderHTML({ HTMLAttributes }) {
    return [
      'div',
      mergeAttributes({ 'data-type': 'callout' }, HTMLAttributes),
      0
    ]
  }
})

// ---- CodeBlock (#189) ----------------------------------------------------
// A managed fenced code block. The Go parser emits a BlockCode ParsedBlock
// whose clean_text retains internal newlines (parser.go); this node carries
// that text as plain `text*` content and a `language` attr (the info string).
// `code: true` disables inline marks inside (code is literal). Enter inserts a
// newline rather than a new block (newlineInCode). On-disk render goes through
// Go's renderBlock BlockCode branch, which emits the ``` ``` fence verbatim.
export const CodeBlock = Node.create({
  name: 'codeBlock',
  group: 'block',
  content: 'text*',
  defining: true,
  isolating: true,
  code: true,

  addAttributes() {
    return {
      id: {
        default: null,
        parseHTML: (el) => el.getAttribute('data-id') || null,
        renderHTML: (attrs) => (attrs.id ? { 'data-id': attrs.id } : {})
      },
      language: {
        default: '',
        // Silt's own shape carries data-language; external `<pre><code>` puts
        // the fence info in the nested code class (`language-ts`, `lang-ts`).
        parseHTML: (el) => {
          const explicit = el.getAttribute('data-language')
          if (explicit) return explicit
          const cls = el.querySelector('code')?.getAttribute('class') ?? ''
          const match = /(?:^|\s)(?:language-|lang-)([\w+#.-]+)/.exec(cls)
          return match ? match[1] : ''
        },
        renderHTML: (attrs) =>
          attrs.language ? { 'data-language': attrs.language } : {}
      },
      file_date: {
        default: '',
        parseHTML: (el) => el.getAttribute('data-file-date') || '',
        renderHTML: (attrs) =>
          attrs.file_date ? { 'data-file-date': attrs.file_date } : {}
      }
    }
  },

  parseHTML() {
    return [
      { tag: 'div[data-type="code"]' },
      { tag: 'pre[data-type="code"]' },
      // Last: external `<pre>` blocks. The Silt shapes above must win when
      // both match, so this generic rule stays at the end.
      { tag: 'pre' }
    ]
  },

  renderHTML({ HTMLAttributes }) {
    return ['div', mergeAttributes({ 'data-type': 'code' }, HTMLAttributes), 0]
  },

  addKeyboardShortcuts() {
    return {
      // Enter inserts a newline inside the code instead of creating a new
      // block below — the defining behaviour of a code block.
      Enter: () => newlineInCode(this.editor.state, this.editor.view.dispatch)
    }
  }
})

// ---- Foldable details (#183) ---------------------------------------------
// TipTap's Details extension renders a native `<details><summary>` container,
// which gives free disclosure behaviour (click summary to toggle), keyboard
// operability, and an implicit aria-expanded. The on-disk form is the HTML
// itself, preserved line-by-line as opaque NOTE blocks by the Go parser (HTML
// passes through clean_text verbatim). The converter groups a `<details>` run
// into this node tree on load and re-emits the run on save. Collapse state is
// ephemeral in v1 (never written as `<details open>`).
//
// `open: false` default keeps sections collapsed on load (the outliner's "the
// file is the truth" model — collapse is a view concern, not persisted).
// The extensions are wrapped with `.extend()` to add `id` and `file_date`
// attributes — without these, ProseMirror silently drops the attrs on
// node creation, causing identity instability across save cycles (#310).
export const SiltDetailsExtensions = [
  Details.extend({
    addAttributes() {
      return {
        ...this.parent?.(),
        id: {
          default: null,
          parseHTML: (el) => el.getAttribute('data-id') || null,
          renderHTML: (attrs) => (attrs.id ? { 'data-id': attrs.id } : {})
        },
        file_date: {
          default: '',
          parseHTML: (el) => el.getAttribute('data-file-date') || '',
          renderHTML: (attrs) =>
            attrs.file_date ? { 'data-file-date': attrs.file_date } : {}
        }
      }
    }
  }).configure({ HTMLAttributes: { 'data-type': 'details' } }),
  DetailsSummary,
  DetailsContent
]

// ---- GFM Tables (#172) ---------------------------------------------------
// TipTap's Table family renders an editable grid with native Tab/arrow-key
// navigation, column resizing, and cell selection. The on-disk form is
// standard GFM pipe syntax; the converter groups a run of pipe-prefixed NOTE
// blocks (one per GFM line) into this node tree on load and re-emits the run
// on save. resizable lets the user drag column borders; the table's block
// identity lives on the LAST row (so the whole table has one id).
export const SiltTableExtensions = [
  Table.extend({
    addAttributes() {
      return {
        ...this.parent?.(),
        id: {
          default: null,
          parseHTML: (el) => el.getAttribute('data-id') || null,
          renderHTML: (attrs) => (attrs.id ? { 'data-id': attrs.id } : {})
        },
        file_date: {
          default: '',
          parseHTML: (el) => el.getAttribute('data-file-date') || '',
          renderHTML: (attrs) =>
            attrs.file_date ? { 'data-file-date': attrs.file_date } : {}
        }
      }
    }
  }).configure({
    resizable: true,
    allowTableNodeSelection: false,
    HTMLAttributes: { 'data-type': 'table' }
  }),
  TableRow,
  TableCell,
  TableHeader
]

// ---- EmbedNode (block-level, atomic) -------------------------------------
// Renders Smart Graph `{{embed:uuid}}` as a live EmbedPortal NodeView (#85).
// Atomic (no editable children); the NodeView fetches the referenced block
// via ResolveBlockReference and renders it as a nested live portal. The
// `uuid` attr is the block UUID; the textual form `{{embed:uuid}}` is
// reconstructed in clean_text on save.
//
// `bullet` is round-trip metadata (#327): a sole-content NOTE (`- {{embed:...}}`)
// carries its bullet marker so the Go serializer re-emits it. Default '' = bare.
export const EmbedNode = Node.create({
  name: 'embedNode',
  group: 'block',
  atom: true,
  selectable: true,
  draggable: false,

  addAttributes() {
    return {
      id: {
        default: null,
        parseHTML: (el) => el.getAttribute('data-id') || null,
        renderHTML: (attrs) => (attrs.id ? { 'data-id': attrs.id } : {})
      },
      uuid: {
        default: '',
        parseHTML: (el) => el.getAttribute('data-uuid') || '',
        renderHTML: (attrs) => (attrs.uuid ? { 'data-uuid': attrs.uuid } : {})
      },
      bullet: { default: '' }
    }
  },

  parseHTML() {
    return [{ tag: 'div[data-type="embed"]' }]
  },

  renderHTML({ HTMLAttributes }) {
    return ['div', mergeAttributes({ 'data-type': 'embed' }, HTMLAttributes)]
  }
})

// ---- BlockReferenceNode (inline, atomic) --------------------------------
// Renders Smart Graph `((uuid))` as an inline BlockReferenceChip NodeView
// (#85). Inline (sits inside a noteBlock's content); clicking the chip
// navigates to the referenced block. The `uuid` attr is the block UUID;
// the textual form `((uuid))` is reconstructed in clean_text on save.
export const BlockReferenceNode = Node.create({
  name: 'blockReferenceNode',
  group: 'inline',
  inline: true,
  atom: true,
  selectable: true,
  draggable: false,

  addAttributes() {
    return {
      uuid: {
        default: '',
        parseHTML: (el) => el.getAttribute('data-uuid') || '',
        renderHTML: (attrs) => (attrs.uuid ? { 'data-uuid': attrs.uuid } : {})
      }
    }
  },

  parseHTML() {
    return [{ tag: 'span[data-type="block-ref"]' }]
  },

  renderHTML({ HTMLAttributes }) {
    return [
      'span',
      mergeAttributes({ 'data-type': 'block-ref' }, HTMLAttributes)
    ]
  }
})

// ---- PageLinkNode (inline, atomic) --------------------------------------
// Renders a wiki / page link `[[target]]` (Obsidian-compatible) as an inline
// PageLinkChip NodeView (#545). `target` is a vault-relative path or basename;
// optional `heading` and `alias`. The textual form `[[target#heading|alias]]`
// is reconstructed in clean_text on save; the converter tokenizer parses it
// back. Clicking navigates to the resolved page via ResolvePageLink.
export const PageLinkNode = Node.create({
  name: 'pageLinkNode',
  group: 'inline',
  inline: true,
  atom: true,
  selectable: true,
  draggable: false,

  addAttributes() {
    return {
      target: {
        default: '',
        parseHTML: (el) => el.getAttribute('data-target') || '',
        renderHTML: (attrs) =>
          (attrs as { target?: string }).target
            ? { 'data-target': (attrs as { target?: string }).target }
            : {}
      },
      heading: {
        default: null as string | null,
        parseHTML: (el) => el.getAttribute('data-heading') || null,
        renderHTML: (attrs) => {
          const h = (attrs as { heading?: string | null }).heading
          return h ? { 'data-heading': h } : {}
        }
      },
      alias: {
        default: null as string | null,
        parseHTML: (el) => el.getAttribute('data-alias') || null,
        renderHTML: (attrs) => {
          const a = (attrs as { alias?: string | null }).alias
          return a ? { 'data-alias': a } : {}
        }
      }
    }
  },

  parseHTML() {
    return [{ tag: 'span[data-type="page-link"]' }]
  },

  renderHTML({ HTMLAttributes }) {
    return [
      'span',
      mergeAttributes({ 'data-type': 'page-link' }, HTMLAttributes)
    ]
  }
})

// ---- MathNode (inline + block, atomic) -----------------------------------
// Renders LaTeX math via KaTeX (#191). Two nodes share one attr (`latex`,
// the raw source): InlineMathNode (`$...$`, inline atomic) and BlockMathNode
// (`$$...$$`, block atomic). The raw LaTeX round-trips through clean_text;
// only the view differs. No official TipTap math package is used — Silt's own
// converter/NodeView pipeline makes a custom node the cleaner fit.
export const InlineMathNode = Node.create({
  name: 'inlineMathNode',
  group: 'inline',
  inline: true,
  atom: true,
  selectable: true,
  draggable: false,

  addAttributes() {
    return {
      latex: {
        default: '',
        parseHTML: (el) => el.getAttribute('data-latex') || '',
        renderHTML: (attrs) =>
          attrs.latex ? { 'data-latex': attrs.latex } : {}
      }
    }
  },

  parseHTML() {
    return [{ tag: 'span[data-type="math-inline"]' }]
  },

  renderHTML({ HTMLAttributes }) {
    return [
      'span',
      mergeAttributes({ 'data-type': 'math-inline' }, HTMLAttributes)
    ]
  },

  addInputRules() {
    return [
      // Inline `$...$` auto-trigger (#328): on the closing `$`, convert the
      // run into an inlineMathNode atom. Two regex guards kill the currency /
      // stray-`$` false positives that this kind of rule would otherwise
      // create — both are load-bearing:
      //
      //   1. Content `[^\s$]+` — no whitespace, no `$`. A space inside the
      //      pair breaks the match, so a currency run like `cost $5 and $3$`
      //      can never span the gap to swallow both amounts; the `[^\s$]+`
      //      run stops at the first space.
      //   2. Lookbehind `(?<![\p{L}\p{N}])` — the opening `$` must NOT follow
      //      a letter or digit. Rejects `a$b$c` (letter before the `$` ⇒ not a
      //      token boundary) and `5$ cash` (digit before). BOL passes.
      //
      // A lookbehind (not a capture group) keeps the boundary char OUT of
      // match[0]: TipTap sizes the replacement range from match[0].length, so
      // a captured prefix would delete the preceding space/punctuation.
      //
      // Trade-off: math containing literal spaces (e.g. `$\int_0^1 x\,dx$`)
      // does NOT trip this rule — use the `/math` slash command for those.
      // Block `$$...$$` is intentionally not handled here either (block-node-
      // mid-paragraph + delimiter round-trip issues; `/math` covers block).
      new InputRule({
        find: /(?<![\p{L}\p{N}])\$([^\s$]+)\$$/u,
        handler: (({
          state,
          range,
          match
        }: Parameters<NonNullable<InputRule['handler']>>[0]) => {
          const node = state.schema.nodes.inlineMathNode.create({
            latex: match[1]
          })
          if (!node) return null
          const tr = state.tr.replaceRangeWith(range.from, range.to, node)
          // Drop the cursor just past the inserted atom so typing continues
          // after the chip, not inside (it's atomic — there's no inside).
          tr.setSelection(
            new TextSelection(tr.doc.resolve(range.from + node.nodeSize))
          )
          tr.scrollIntoView()
          return tr
        }) as unknown as InputRule['handler']
      })
    ]
  }
})

export const BlockMathNode = Node.create({
  name: 'blockMathNode',
  group: 'block',
  atom: true,
  selectable: true,
  draggable: false,

  addAttributes() {
    return {
      id: {
        default: null,
        parseHTML: (el) => el.getAttribute('data-id') || null,
        renderHTML: (attrs) => (attrs.id ? { 'data-id': attrs.id } : {})
      },
      latex: {
        default: '',
        parseHTML: (el) => el.getAttribute('data-latex') || '',
        renderHTML: (attrs) =>
          attrs.latex ? { 'data-latex': attrs.latex } : {}
      },
      // `bullet` is round-trip metadata (#327): a sole-content NOTE
      // (`- $$x$$`) carries its bullet marker so the Go serializer re-emits
      // it. Default '' = bare (atomic blocks default to no bullet).
      bullet: { default: '' }
    }
  },

  parseHTML() {
    return [{ tag: 'div[data-type="math-block"]' }]
  },

  renderHTML({ HTMLAttributes }) {
    return [
      'div',
      mergeAttributes({ 'data-type': 'math-block' }, HTMLAttributes)
    ]
  }
})

// ---- MentionNode (inline, atomic) ----------------------------------------
// Renders an @-mention chip (`@[name]`) for inline owner references (#184).
// Inline + atomic like BlockReferenceNode; the `name` attr is the owner label
// and the textual form `@[name]` is reconstructed in clean_text on save. The
// suggestion list comes from the read-only DistinctOwners index projection —
// no mention state lives in SQLite.
export const MentionNode = Node.create({
  name: 'mentionNode',
  group: 'inline',
  inline: true,
  atom: true,
  selectable: true,
  draggable: false,

  addAttributes() {
    return {
      name: {
        default: '',
        parseHTML: (el) => el.getAttribute('data-name') || '',
        renderHTML: (attrs) => (attrs.name ? { 'data-name': attrs.name } : {})
      }
    }
  },

  parseHTML() {
    return [{ tag: 'span[data-type="mention"]' }]
  },

  renderHTML({ HTMLAttributes }) {
    return ['span', mergeAttributes({ 'data-type': 'mention' }, HTMLAttributes)]
  }
})

// ---- EmbedBlockNode (generic plugin-extensible block, #110 Phase 1) ---------
// A generic block-level atomic node that plugins specialize via attrs (type,
// src, caption, openable, pluginID, data). Covers attachments (#101), image
// embeds, link cards, and most custom-block use cases without requiring a
// static ProseMirror schema change per plugin.
//
// Serialization round-trip: the node serializes to a NOTE block whose clean_text
// is an HTML-comment marker `<!-- silt-embed: {json attrs} -->`, consistent with
// the existing `<!-- id: uuid @ date -->` convention. The Go parser preserves
// the marker as the NOTE's clean_text (it only strips the trailing id comment),
// so the on-disk file round-trips byte-for-byte.
export interface EmbedBlockAttrs {
  embedType: string // "attachment" | "image" | plugin-defined
  src: string // relative path / url / identifier
  caption?: string
  openable?: boolean
  pluginID?: string
  data?: Record<string, unknown> // arbitrary plugin-specific attrs
}

export const SiltEmbedMarker = 'silt-embed'

export const EmbedBlockNode = Node.create({
  name: 'embedBlock',
  group: 'block',
  atom: true,
  selectable: true,
  draggable: true,

  addAttributes() {
    return {
      embedType: {
        default: '',
        parseHTML: (el) => el.getAttribute('data-embed-type') || '',
        renderHTML: (attrs) =>
          attrs.embedType ? { 'data-embed-type': attrs.embedType } : {}
      },
      src: {
        default: '',
        parseHTML: (el) => el.getAttribute('data-src') || '',
        renderHTML: (attrs) => (attrs.src ? { 'data-src': attrs.src } : {})
      },
      caption: {
        default: '',
        parseHTML: (el) => el.getAttribute('data-caption') || '',
        renderHTML: (attrs) =>
          attrs.caption ? { 'data-caption': attrs.caption } : {}
      },
      openable: {
        default: false,
        parseHTML: (el) => el.getAttribute('data-openable') === 'true',
        renderHTML: (attrs) =>
          attrs.openable ? { 'data-openable': 'true' } : {}
      },
      pluginID: {
        default: '',
        parseHTML: (el) => el.getAttribute('data-plugin') || '',
        renderHTML: (attrs) =>
          attrs.pluginID ? { 'data-plugin': attrs.pluginID } : {}
      },
      // `bullet` is round-trip metadata (#327): a sole-content NOTE
      // (`- <!-- silt-embed: ... -->`) carries its bullet marker so the Go
      // serializer re-emits it. Default '' = bare.
      bullet: { default: '' }
    }
  },

  parseHTML() {
    return [{ tag: 'div[data-type="embed-block"]' }]
  },

  renderHTML({ HTMLAttributes }) {
    return [
      'div',
      mergeAttributes({ 'data-type': 'embed-block' }, HTMLAttributes)
    ]
  }
})
