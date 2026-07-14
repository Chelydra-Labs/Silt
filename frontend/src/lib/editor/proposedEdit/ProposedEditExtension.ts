// ProposedEdit — in-editor preview of a Writing Assistant proposal over an
// arbitrary editor selection range (#543, #548).
//
// The proposal lives entirely in the editor's VIEW layer (a ProseMirror
// DecorationSet): it never mutates the document, never fires `docChanged`,
// never touches autosave, and never writes to disk until the user accepts.
// Accept applies ONE ProseMirror transaction (one undo step) — the editor
// applies the change itself, so there is no focus-lock conflict with the
// backend block writer. Reject / Esc clears the decorations with a meta-only
// transaction (no history entry).
//
// Multi-paragraph proposals on block-spanning selections accept as multiple
// noteBlock nodes (one per paragraph), preserving structure (#548).
// Schema-incompatible proposals (the target context doesn't allow block
// nodes) are detected at preview time so the WA controller falls back to
// the panel-only apply path.
//
// This mirrors the spellcheck decoration pattern (SpellcheckExtension) and the
// ProseMirror guidance: decorations influence drawing, not the document.

import { Extension, type Command } from '@tiptap/core'
import {
  Plugin,
  PluginKey,
  type EditorState,
  type Transaction
} from '@tiptap/pm/state'
import { Decoration, DecorationSet, type EditorView } from '@tiptap/pm/view'
import {
  Fragment,
  Slice,
  type Mark,
  type Node as PMNode,
  type ResolvedPos,
  type Schema
} from '@tiptap/pm/model'
import { legacyTokenizeInline } from '../converters'

export interface ProposedEditOptions {
  /** Inclusive start PM position of the range being replaced. */
  from: number
  /** Exclusive end PM position of the range being replaced. */
  to: number
  /** Proposed replacement markdown (rendered as a preview; parsed to inline
   *  content on accept). */
  markdown: string
  /** Optional host callback fired after a successful accept (e.g. the Writing
   *  Assistant controller marking the proposal 'accepted'). */
  onAccept?: () => void
  /** Date for the block-identity comment of new noteBlocks created by a
   *  multi-block accept. Defaults to today. */
  fileDate?: string
}

interface ActiveProposal {
  from: number
  to: number
  markdown: string
  onAccept?: () => void
  fileDate: string
}

interface PluginState {
  proposal: ActiveProposal | null
  decos: DecorationSet
}

const key = new PluginKey<PluginState>('siltProposedEdit')
const SET_META = 'siltProposedEdit:set'
const CLEAR_META = 'siltProposedEdit:clear'

interface AcceptRejectCommands {
  acceptProposedEdit: () => boolean
  rejectProposedEdit: () => boolean
}

/** True when the proposed range has collapsed or inverted (the user deleted
 *  the underlying text) — the proposal is no longer meaningful, so auto-clear. */
function rangeCollapsed(from: number, to: number): boolean {
  return from >= to
}

/** Compute the visual strike range for the proposal. For multi-block proposals
 *  on isolating noteBlocks, the accept path expands to whole-block boundaries;
 *  the strike must match so the user sees exactly what will be replaced, not
 *  just their selection. */
function expandedStrikeRange(
  state: EditorState,
  proposal: ActiveProposal
): { from: number; to: number } {
  if (
    wouldUseMultiBlock(state, proposal.from, proposal.to, proposal.markdown)
  ) {
    const $from = state.doc.resolve(proposal.from)
    const $to = state.doc.resolve(proposal.to)
    return {
      from: $from.before($from.depth),
      to: $to.after($to.depth)
    }
  }
  return { from: proposal.from, to: proposal.to }
}

/** Build the decorations for an active proposal: a strike over the range that
 *  Accept will replace (expanded for multi-block) plus a non-editable preview
 *  widget with Accept/Reject controls at the end of the struck range. */
function buildDecos(
  state: EditorState,
  proposal: ActiveProposal,
  commands: AcceptRejectCommands
): DecorationSet {
  const { from: strikeFrom, to: strikeTo } = expandedStrikeRange(
    state,
    proposal
  )
  const decos: Decoration[] = [
    Decoration.inline(strikeFrom, strikeTo, {
      class: 'silt-proposed-delete'
    })
  ]
  decos.push(
    Decoration.widget(strikeTo, () => renderPreviewWidget(proposal, commands), {
      side: 1,
      key: 'silt-proposed-edit-widget',
      stopEvent: () => true
    })
  )
  return DecorationSet.create(state.doc, decos)
}

/** Render the preview + Accept/Reject controls as a non-editable widget. */
function renderPreviewWidget(
  proposal: ActiveProposal,
  commands: AcceptRejectCommands
): HTMLElement {
  const wrap = document.createElement('div')
  wrap.className = 'silt-proposed-edit'
  wrap.setAttribute('contenteditable', 'false')
  wrap.setAttribute('role', 'group')
  wrap.setAttribute('aria-label', 'AI proposed edit')

  const preview = document.createElement('div')
  preview.className = 'silt-proposed-edit-preview'
  preview.setAttribute('role', 'status')
  preview.setAttribute('aria-live', 'polite')
  preview.textContent = proposal.markdown
  wrap.appendChild(preview)

  const controls = document.createElement('div')
  controls.className = 'silt-proposed-edit-controls'

  const acceptBtn = document.createElement('button')
  acceptBtn.type = 'button'
  acceptBtn.className = 'silt-proposed-edit-accept'
  acceptBtn.textContent = 'Accept'
  acceptBtn.title = 'Accept proposed edit (Ctrl+Enter)'
  acceptBtn.setAttribute('aria-label', 'Accept proposed edit')
  acceptBtn.onclick = (e) => {
    e.preventDefault()
    commands.acceptProposedEdit()
  }
  controls.appendChild(acceptBtn)

  const rejectBtn = document.createElement('button')
  rejectBtn.type = 'button'
  rejectBtn.className = 'silt-proposed-edit-reject'
  rejectBtn.textContent = 'Reject'
  rejectBtn.title = 'Reject proposed edit (Esc)'
  rejectBtn.setAttribute('aria-label', 'Reject proposed edit')
  rejectBtn.onclick = (e) => {
    e.preventDefault()
    commands.rejectProposedEdit()
  }
  controls.appendChild(rejectBtn)

  wrap.appendChild(controls)
  return wrap
}

/** Convert inline NodeJSON[] from legacyTokenizeInline into PMNode[]
 *  (text + atomic inline nodes), schema-aware. */
function inlineNodesFromJSON(
  schema: Schema,
  json: ReturnType<typeof legacyTokenizeInline>
): PMNode[] {
  const nodes: PMNode[] = []
  for (const j of json) {
    const node = j as {
      type: string
      text?: string
      marks?: { type: string; attrs?: Record<string, unknown> }[]
      attrs?: Record<string, unknown>
    }
    if (node.type === 'text') {
      const marks: Mark[] = (node.marks ?? [])
        .map((m) => {
          const factory = schema.marks[m.type]
          return factory ? factory.create(m.attrs) : null
        })
        .filter((m): m is Mark => m !== null)
      const text = schema.text(node.text ?? '', marks)
      if (text) nodes.push(text)
    } else {
      const factory = schema.nodes[node.type]
      if (factory) {
        const atom = factory.create(node.attrs)
        if (atom) nodes.push(atom)
      }
    }
  }
  return nodes
}

/** Parse proposed markdown into inline ProseMirror content for the accept
 *  transaction. Block-level structure is collapsed to inline (a selection
 *  within a Silt block is inline content); newlines collapse to spaces,
 *  matching the single-line block model the Go serializer uses. */
function proposedMarkdownToSlice(schema: Schema, markdown: string): Slice {
  const singleLine = markdown.replace(/\r?\n/g, ' ')
  const json = legacyTokenizeInline(singleLine)
  return new Slice(Fragment.from(inlineNodesFromJSON(schema, json)), 0, 0)
}

/** Split multi-paragraph markdown into paragraphs (double-newline breaks).
 *  Single newlines within a paragraph are preserved as inline breaks (the Go
 *  serializer collapses single \n→space for prose types). */
function splitParagraphs(markdown: string): string[] {
  return markdown
    .split(/\n\s*\n/)
    .map((p) => p.trim())
    .filter(Boolean)
}

/** Parse multi-paragraph markdown into a Slice of noteBlock nodes (one per
 *  paragraph). Used when the selection spans blocks AND the proposal has
 *  multiple paragraphs — each paragraph becomes its own noteBlock,
 *  preserving structure instead of flattening to one line (#548).
 *
 *  The slice uses openStart=openEnd=0 (complete block nodes). Silt's
 *  noteBlock has isolating:true, so partial-block merging via openStart=1
 *  is not possible — the accept command expands the selection to whole-block
 *  boundaries before calling this.
 *
 *  New blocks inherit the outline attrs (depth/bullet/quote/align) of the
 *  first replaced noteBlock so an AI rewrite preserves indentation and list
 *  marker instead of flattening nested/quoted/plain blocks to top-level
 *  bullets. */
function proposedMarkdownToBlockSlice(
  schema: Schema,
  markdown: string,
  fileDate: string,
  inheritAttrs?: {
    depth: number
    bullet: string
    quote: string
    align: string
  }
): Slice | null {
  const blockType = schema.nodes.noteBlock
  if (!blockType) return null
  const paragraphs = splitParagraphs(markdown)
  if (paragraphs.length === 0) return null
  const depth = inheritAttrs?.depth ?? 0
  const bullet = inheritAttrs?.bullet ?? ''
  const quote = inheritAttrs?.quote ?? ''
  const align = inheritAttrs?.align ?? 'left'
  const blockNodes: PMNode[] = paragraphs.map((para) => {
    const inlineJSON = legacyTokenizeInline(para)
    const content = inlineNodesFromJSON(schema, inlineJSON)
    return blockType.create(
      {
        id: crypto.randomUUID(),
        depth,
        bullet,
        quote,
        align,
        file_date: fileDate
      },
      content
    )
  })
  return new Slice(Fragment.from(blockNodes), 0, 0)
}

/** True when the selection spans block boundaries AND the markdown has
 *  multiple paragraphs — the multi-block accept path should be used. */
function wouldUseMultiBlock(
  state: EditorState,
  from: number,
  to: number,
  markdown: string
): boolean {
  if (!/\n\s*\n/.test(markdown)) return false
  const $from = state.doc.resolve(from)
  const $to = state.doc.resolve(to)
  return $from.parent !== $to.parent
}

/** True when `pos` sits anywhere inside a GFM table cell or header. The table
 *  cell content model is `block+` (so it accepts noteBlock), but the table
 *  serializer flattens block children to inline text — multi-paragraph
 *  proposals must fall back to the panel-only apply path there. */
function withinTableCell($pos: ResolvedPos): boolean {
  for (let d = $pos.depth; d > 0; d--) {
    const name = $pos.node(d).type.name
    if (name === 'tableCell' || name === 'tableHeader') return true
  }
  return false
}

/** Check if the target position's content model allows noteBlock nodes
 *  (schema-incompatible proposals fall back to the panel-only path). */
function multiBlockAllowed(
  state: EditorState,
  from: number,
  to: number
): boolean {
  const blockType = state.schema.nodes.noteBlock
  if (!blockType) return false
  const $from = state.doc.resolve(from)
  const $to = state.doc.resolve(to)
  // Table cells accept noteBlock (block+) but can't round-trip block children
  // through the GFM table serializer, so multi-paragraph proposals there must
  // fall back to the panel-only apply path.
  if (withinTableCell($from) || withinTableCell($to)) return false
  const range = $from.blockRange($to)
  if (!range) return false
  return range.parent.type.contentMatch.matchType(blockType) !== null
}

function todayDate(): string {
  return new Date().toISOString().slice(0, 10)
}

export const ProposedEdit = Extension.create({
  name: 'siltProposedEdit',

  addProseMirrorPlugins() {
    const editor = this.editor
    const commands: AcceptRejectCommands = {
      acceptProposedEdit: () => editor.commands.acceptProposedEdit(),
      rejectProposedEdit: () => editor.commands.rejectProposedEdit()
    }
    return [
      new Plugin<PluginState>({
        key,
        state: {
          init: () => ({ proposal: null, decos: DecorationSet.empty }),
          apply(
            tr: Transaction,
            prev: PluginState,
            _oldState: EditorState,
            newState: EditorState
          ): PluginState {
            const set = tr.getMeta(SET_META) as ActiveProposal | undefined
            if (set) {
              if (rangeCollapsed(set.from, set.to)) {
                return { proposal: null, decos: DecorationSet.empty }
              }
              return {
                proposal: set,
                decos: buildDecos(newState, set, commands)
              }
            }
            if (tr.getMeta(CLEAR_META)) {
              return { proposal: null, decos: DecorationSet.empty }
            }
            if (prev.proposal && tr.docChanged) {
              // Map the range + decorations through the edit so the preview
              // tracks the original text. If the range collapsed (the user
              // deleted it), auto-dismiss.
              const from = tr.mapping.map(prev.proposal.from)
              const to = tr.mapping.map(prev.proposal.to)
              const decos = prev.decos.map(tr.mapping, tr.doc)
              if (rangeCollapsed(from, to)) {
                return { proposal: null, decos: DecorationSet.empty }
              }
              return { proposal: { ...prev.proposal, from, to }, decos }
            }
            return prev
          }
        },
        props: {
          decorations(state: EditorState): DecorationSet | undefined {
            return key.getState(state)?.decos
          },
          handleKeyDown(view: EditorView, event: KeyboardEvent): boolean {
            const st = key.getState(view.state)
            if (!st?.proposal) return false
            // Ctrl/Cmd+Enter accepts; Esc rejects. The Writing Assistant
            // drawer also listens for Esc (it discards then closes) — both
            // dismissals are coherent.
            if ((event.ctrlKey || event.metaKey) && event.key === 'Enter') {
              editor.commands.acceptProposedEdit()
              return true
            }
            if (event.key === 'Escape') {
              editor.commands.rejectProposedEdit()
              return true
            }
            return false
          }
        }
      })
    ]
  },

  addCommands() {
    return {
      setProposedEdit:
        (opts: ProposedEditOptions): Command =>
        ({ state, tr, dispatch }) => {
          if (rangeCollapsed(opts.from, opts.to)) return false
          // Empty / whitespace-only proposals are not previewable (#543).
          if (!opts.markdown?.trim()) return false
          // Schema-incompatible dry-run (#548): if the multi-block path
          // would be used, verify the target allows noteBlock. If not,
          // return false so the WA controller falls back to the panel-only
          // apply path (no silent drop).
          if (
            wouldUseMultiBlock(state, opts.from, opts.to, opts.markdown) &&
            !multiBlockAllowed(state, opts.from, opts.to)
          ) {
            return false
          }
          if (dispatch) {
            dispatch(
              tr.setMeta(SET_META, {
                from: opts.from,
                to: opts.to,
                markdown: opts.markdown,
                onAccept: opts.onAccept,
                fileDate: opts.fileDate ?? todayDate()
              } as ActiveProposal)
            )
          }
          return true
        },
      acceptProposedEdit:
        (): Command =>
        ({ state, dispatch }) => {
          const st = key.getState(state)
          if (!st?.proposal) return false
          const { from, to, markdown, onAccept, fileDate } = st.proposal
          // Empty content must not wipe the selection (plan: accept no-ops).
          if (!markdown?.trim()) return false
          // Build the replacement content and apply ONE transaction.
          // The editor applies the change itself, sidestepping the focus lock
          // that blocks the backend MutateBlock path on a focused editor.
          let slice: Slice
          let replaceFrom = from
          let replaceTo = to
          if (wouldUseMultiBlock(state, from, to, markdown)) {
            try {
              // Multi-paragraph proposal on a block-spanning selection: each
              // paragraph becomes its own noteBlock (#548). noteBlock has
              // isolating:true, so we expand the selection to cover the ENTIRE
              // selected blocks (from before the first to after the last) and
              // use openStart=openEnd=0 for a clean whole-block replacement.
              const $from = state.doc.resolve(from)
              const $to = state.doc.resolve(to)
              replaceFrom = $from.before($from.depth)
              replaceTo = $to.after($to.depth)
              // Inherit outline attrs from the first replaced noteBlock so the
              // rewrite keeps the original indentation/list marker/quote rather
              // than resetting every block to a top-level bullet.
              const firstBlock = $from.parent
              const inherit =
                firstBlock.type.name === 'noteBlock'
                  ? {
                      depth: (firstBlock.attrs.depth as number) ?? 0,
                      bullet: (firstBlock.attrs.bullet as string) ?? '',
                      quote: (firstBlock.attrs.quote as string) ?? '',
                      align: (firstBlock.attrs.align as string) ?? 'left'
                    }
                  : undefined
              const blockSlice = proposedMarkdownToBlockSlice(
                state.schema,
                markdown,
                fileDate,
                inherit
              )
              if (!blockSlice) throw new Error('no noteBlock in schema')
              slice = blockSlice
            } catch {
              // Safety net (#548 harden): the doc may have changed between
              // preview and accept (positions stale, block structure shifted,
              // schema mismatch). Fall back to the inline path, which is
              // always safe for any valid range.
              slice = proposedMarkdownToSlice(state.schema, markdown)
              replaceFrom = from
              replaceTo = to
            }
          } else {
            slice = proposedMarkdownToSlice(state.schema, markdown)
          }
          const apply = state.tr.replace(replaceFrom, replaceTo, slice)
          // Clear the proposal in the same transaction (meta-only; no extra
          // doc step). addToHistory defaults on so accept is one undo step.
          apply.setMeta(CLEAR_META, true)
          dispatch?.(apply)
          onAccept?.()
          return true
        },
      rejectProposedEdit:
        (): Command =>
        ({ state, tr, dispatch }) => {
          const st = key.getState(state)
          if (!st?.proposal) return false
          if (dispatch) {
            // Meta-only: no doc steps, so reject adds no undo history entry.
            dispatch(tr.setMeta(CLEAR_META, true))
          }
          return true
        }
    }
  }
})

/** Read-only accessor: is a proposed edit currently shown in `editor`? */
export function hasProposedEdit(editor: { state: EditorState }): boolean {
  return !!key.getState(editor.state)?.proposal
}

/** Read-only accessor: the active proposal's range, if any. */
export function getProposedEditRange(editor: {
  state: EditorState
}): { from: number; to: number } | null {
  const st = key.getState(editor.state)
  return st?.proposal ? { from: st.proposal.from, to: st.proposal.to } : null
}

// Declare the commands on the TipTap CommandManager type so callers get types.
declare module '@tiptap/core' {
  interface Commands<ReturnType> {
    siltProposedEdit: {
      setProposedEdit: (opts: ProposedEditOptions) => ReturnType
      acceptProposedEdit: () => ReturnType
      rejectProposedEdit: () => ReturnType
    }
  }
}
