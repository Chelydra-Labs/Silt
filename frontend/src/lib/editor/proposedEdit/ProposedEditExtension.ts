// ProposedEdit — in-editor preview of a Writing Assistant proposal over an
// arbitrary editor selection range (#543).
//
// The proposal lives entirely in the editor's VIEW layer (a ProseMirror
// DecorationSet): it never mutates the document, never fires `docChanged`,
// never touches autosave, and never writes to disk until the user accepts.
// Accept applies ONE ProseMirror transaction (one undo step) — the editor
// applies the change itself, so there is no focus-lock conflict with the
// backend block writer. Reject / Esc clears the decorations with a meta-only
// transaction (no history entry).
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
}

interface ActiveProposal {
  from: number
  to: number
  markdown: string
  onAccept?: () => void
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

/** Build the decorations for an active proposal: a strike over the original
 *  range plus a non-editable preview widget with Accept/Reject controls. */
function buildDecos(
  doc: PMNode,
  proposal: ActiveProposal,
  commands: AcceptRejectCommands
): DecorationSet {
  const decos: Decoration[] = [
    Decoration.inline(proposal.from, proposal.to, {
      class: 'silt-proposed-delete'
    })
  ]
  decos.push(
    Decoration.widget(
      proposal.to,
      () => renderPreviewWidget(proposal, commands),
      { side: 1, key: 'silt-proposed-edit-widget', stopEvent: () => true }
    )
  )
  return DecorationSet.create(doc, decos)
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

/** Parse proposed markdown into inline ProseMirror content for the accept
 *  transaction. Block-level structure is collapsed to inline (a selection
 *  within a Silt block is inline content); newlines collapse to spaces,
 *  matching the single-line block model the Go serializer uses. */
function proposedMarkdownToSlice(schema: Schema, markdown: string): Slice {
  const singleLine = markdown.replace(/\r?\n/g, ' ')
  const json = legacyTokenizeInline(singleLine)
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
  return new Slice(Fragment.from(nodes), 0, 0)
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
                decos: buildDecos(newState.doc, set, commands)
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
        ({ tr, dispatch }) => {
          if (rangeCollapsed(opts.from, opts.to)) return false
          if (dispatch) {
            dispatch(tr.setMeta(SET_META, { ...opts } as ActiveProposal))
          }
          return true
        },
      acceptProposedEdit:
        (): Command =>
        ({ state, dispatch }) => {
          const st = key.getState(state)
          if (!st?.proposal) return false
          const { from, to, markdown, onAccept } = st.proposal
          // Build the replacement inline content and apply ONE transaction.
          // The editor applies the change itself, sidestepping the focus lock
          // that blocks the backend MutateBlock path on a focused editor.
          const slice = proposedMarkdownToSlice(state.schema, markdown)
          const apply = state.tr.replace(from, to, slice)
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
