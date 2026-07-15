// Agent tool registration — P0 (#597–#601) and P1 (#602–#604).
//
// Wires the Phase-4 + Phase-6 tools into the registry. Called from
// onVaultOpen so the catalog is populated before the agent loop runs;
// onVaultClose/onShutdown call clearTools() to tear down. registerTool stores
// defs in a module-scoped Map keyed by name, so re-registering on the next
// vault open replaces cleanly.

import type { AgentToolDef } from './tool-registry'
import { registerTool } from './tool-registry'
import { getBacklinksToolDef, handleGetBacklinks } from './tools/get_backlinks'
import { createNoteToolDef, handleCreateNote } from './tools/create_note'
import { queryTasksToolDef, handleQueryTasks } from './tools/query_tasks'
import { readBlocksToolDef, handleReadBlocks } from './tools/read_blocks'
import { searchNotesToolDef, handleSearchNotes } from './tools/search_notes'
import {
  getRelatedNotesToolDef,
  handleGetRelatedNotes
} from './tools/get_related_notes'
import { updateBlockToolDef, handleUpdateBlock } from './tools/update_block'
import {
  listTagsTool,
  findUntaggedTool,
  renameTagTool,
  handleRenameTag,
  commitRenameTag,
  renameTagToolDef
} from './tools/tag_management'

const P0_TOOLS: AgentToolDef[] = [
  { ...searchNotesToolDef, handler: handleSearchNotes },
  { ...readBlocksToolDef, handler: handleReadBlocks },
  { ...getBacklinksToolDef, handler: handleGetBacklinks },
  { ...queryTasksToolDef, handler: handleQueryTasks },
  { ...createNoteToolDef, handler: handleCreateNote }
]

/** P1 tools (#602–#604). rename_tag is staged and carries its commit half. */
const P1_TOOLS: AgentToolDef[] = [
  { ...getRelatedNotesToolDef, handler: handleGetRelatedNotes },
  { ...updateBlockToolDef, handler: handleUpdateBlock },
  listTagsTool,
  findUntaggedTool,
  // rename_tag is also exported with handler/commit wired; the explicit form
  // below documents the staged-tool contract inline for readers of this file.
  { ...renameTagToolDef, handler: handleRenameTag, commit: commitRenameTag }
]

/** Register all Phase-4 P0 tools (idempotent — re-registers by name). */
export function registerP0Tools(): void {
  for (const tool of P0_TOOLS) registerTool(tool)
}

/** Register all Phase-6 P1 tools (idempotent — re-registers by name). */
export function registerP1Tools(): void {
  for (const tool of P1_TOOLS) registerTool(tool)
}

// Re-export so callers (e.g. tests, index wiring) don't need the per-tool
// module paths. The alias-tagged export keeps listTagsTool/findUntaggedTool
// visible here too, even though they are referenced via the array above.
export { listTagsTool, findUntaggedTool, renameTagTool }
