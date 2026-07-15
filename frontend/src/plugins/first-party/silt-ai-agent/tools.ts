// P0 agent tool registration (#597–#601).
//
// Wires the five Phase-4 tools into the registry. Called from onVaultOpen so
// the catalog is populated before the agent loop runs; onVaultClose/onShutdown
// call clearTools() to tear down. registerTool stores defs in a module-scoped
// Map keyed by name, so re-registering on the next vault open replaces cleanly.

import type { AgentToolDef } from './tool-registry'
import { registerTool } from './tool-registry'
import { getBacklinksToolDef, handleGetBacklinks } from './tools/get_backlinks'
import { createNoteToolDef, handleCreateNote } from './tools/create_note'
import { queryTasksToolDef, handleQueryTasks } from './tools/query_tasks'
import { readBlocksToolDef, handleReadBlocks } from './tools/read_blocks'
import { searchNotesToolDef, handleSearchNotes } from './tools/search_notes'

const P0_TOOLS: AgentToolDef[] = [
  { ...searchNotesToolDef, handler: handleSearchNotes },
  { ...readBlocksToolDef, handler: handleReadBlocks },
  { ...getBacklinksToolDef, handler: handleGetBacklinks },
  { ...queryTasksToolDef, handler: handleQueryTasks },
  { ...createNoteToolDef, handler: handleCreateNote }
]

/** Register all Phase-4 P0 tools (idempotent — re-registers by name). */
export function registerP0Tools(): void {
  for (const tool of P0_TOOLS) registerTool(tool)
}
