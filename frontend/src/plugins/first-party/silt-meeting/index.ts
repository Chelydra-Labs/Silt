// silt-meeting plugin entry (#1105). Headless: its entire surface is the
// /meeting slash command, which prepends a collapsible "Meeting information"
// details section to the top of the current note, pre-seeded with labeled
// empty field lines (Subject, When, Location, Organizer, Attendees, Join
// link, Notes).
import type { Editor } from '@tiptap/core'
import type { Node as ProseMirrorNode } from '@tiptap/pm/model'
import type { PluginContext, PluginManifest, SiltPlugin } from '../../sdk'
import { freshId } from '../../../lib/editor/uniqueIdPlugin'

export const manifest: PluginManifest = {
  id: 'silt-meeting',
  name: 'Meeting Information',
  version: '1.0.0',
  author: 'Silt',
  description:
    'Prepend a collapsible Meeting information section with labeled fields via /meeting.',
  icon: 'event',
  // Documentation parity: first-party plugins are implicitly granted every
  // capability (seedFirstPartyGrants), so this declaration surfaces the
  // editor-schema need in the Plugins UI without gating anything.
  capabilities: { 'editor-schema': true }
}

// The seven labeled field lines, in insertion order.
export const MEETING_FIELDS = [
  'Subject:',
  'When:',
  'Location:',
  'Organizer:',
  'Attendees:',
  'Join link:',
  'Notes:'
] as const

// Build the Details > DetailsSummary + DetailsContent(field lines) tree the
// SiltDetailsExtensions schema expects — same shape as keymaps.ts
// insertDetails. The details node keeps `id: null` (UniqueBlockIds assigns
// top-level ids); the inner field noteBlocks mint ids up front because the
// appendTransaction walk never descends into detailsContent.
export function buildMeetingSection(
  schema: Editor['state']['schema'],
  today: string
): ProseMirrorNode | null {
  const { details, detailsSummary, detailsContent, noteBlock } = schema.nodes
  if (!details || !detailsSummary || !detailsContent || !noteBlock) return null
  const fields = MEETING_FIELDS.map((label) =>
    noteBlock.create(
      { id: freshId(), depth: 0, bullet: '', file_date: today },
      [schema.text(label)]
    )
  )
  // `open: true` below mirrors keymaps' insertDetails; the schema declares
  // the attr (persist: true), so the section lands expanded and the NodeView
  // auto-expands it. The serializer never writes `open` to disk — a reloaded
  // section is collapsed.
  return details.create({ id: null, open: true, file_date: today }, [
    detailsSummary.create({ id: null }, schema.text('Meeting information')),
    detailsContent.create({ id: null }, fields)
  ])
}

// Prepend the meeting section to the document start. The slash menu deletes
// the `/meeting` trigger text before invoking onSelect, and the trigger
// block is a noteBlock — so in a brand-new note (single empty block) the
// section REPLACES it and becomes the only block; otherwise it inserts
// above every existing block.
export function insertMeetingSection(editor: Editor): boolean {
  if (!editor || editor.isDestroyed) return false
  const section = buildMeetingSection(
    editor.state.schema,
    new Date().toISOString().slice(0, 10)
  )
  if (!section) return false

  const doc = editor.state.doc
  const only = doc.childCount === 1 ? doc.child(0) : null
  const isEmptyTrigger =
    only !== null &&
    only.type.name === 'noteBlock' &&
    (only.content.size === 0 || only.textContent.trim() === '')
  if (isEmptyTrigger && only) {
    editor.view.dispatch(editor.state.tr.replaceWith(0, only.nodeSize, section))
  } else {
    editor.commands.insertContentAt(0, section)
  }

  // Park the caret at the start of the first field line so typing and
  // pasting land inside the section, not below it. descendants() positions
  // are relative to the details node's content start, so +1 crosses the
  // details' own open token (the section is doc child 0 in both paths) and
  // +1 steps past the noteBlock's open token onto its first text position.
  const inserted = editor.state.doc.child(0)
  if (inserted.type.name === 'details') {
    let caret = -1
    inserted.descendants((node, pos) => {
      if (caret < 0 && node.type.name === 'noteBlock') {
        caret = pos + 2
        return false
      }
      return true
    })
    if (caret > 0) editor.commands.setTextSelection(caret)
  }
  editor.commands.focus()
  return true
}

export default {
  manifest,
  onVaultOpen(ctx: PluginContext) {
    // Register /meeting (host namespaces the id as silt-meeting:meeting).
    // The returned unregister is not captured: teardown also sweeps via
    // unregisterPluginSlashCommands, mirroring silt-attachments.
    ctx.registerSlashCommand({
      id: 'meeting',
      label: 'Meeting information',
      description:
        'Insert a Meeting information section at the top of the note',
      icon: 'event',
      onSelect: (editor, _pos) => {
        insertMeetingSection(editor as Editor)
      }
    })
  }
} satisfies SiltPlugin
