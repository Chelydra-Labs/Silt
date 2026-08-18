// Agent tool registration — P0 (#597–#601), P1 (#602–#604), and P2 (#606–#608).
//
// Wires the Phase-4 + Phase-6 + Phase-7 tools into the registry. Called from
// onVaultOpen so the catalog is populated before the agent loop runs;
// onVaultClose/onShutdown call clearTools() to tear down. registerTool stores
// defs in a module-scoped Map keyed by name, so re-registering on the next
// vault open replaces cleanly.

import type { PluginContext } from '../../sdk'
import type { AgentToolDef } from './tool-registry'
import { registerTool, unregisterTool } from './tool-registry'
import { getAIAvailability } from '../../shared/ai-chat/availability'
import { reconcileAgentEmbedIndex } from './embed_lifecycle'
import { getBacklinksToolDef, handleGetBacklinks } from './tools/get_backlinks'
import { createNoteToolDef, handleCreateNote } from './tools/create_note'
import { createTaskToolDef, handleCreateTask } from './tools/create_task'
import { queryTasksToolDef, handleQueryTasks } from './tools/query_tasks'
import { readBlocksToolDef, handleReadBlocks } from './tools/read_blocks'
import { searchNotesToolDef, handleSearchNotes } from './tools/search_notes'
import {
  getRelatedNotesToolDef,
  handleGetRelatedNotes
} from './tools/get_related_notes'
import { updateBlockToolDef, handleUpdateBlock } from './tools/update_block'
import { updateTaskToolDef, handleUpdateTask } from './tools/update_task'
import {
  listTagsTool,
  findUntaggedTool,
  renameTagTool,
  handleRenameTag,
  commitRenameTag,
  renameTagToolDef
} from './tools/tag_management'
import {
  getVaultStatisticsToolDef,
  handleGetVaultStatistics
} from './tools/get_vault_statistics'
import {
  suggestLinkTargetsToolDef,
  handleSuggestLinkTargets
} from './tools/suggest_link_targets'
import {
  extractAndSaveToolDef,
  handleExtractAndSave,
  commitExtractAndSave
} from './tools/extract_and_save'
import {
  searchProductDocsToolDef,
  handleSearchProductDocs
} from './tools/search_product_docs'
import {
  listPageVersionsToolDef,
  handleListPageVersions
} from './tools/list_page_versions'
import {
  getPageVersionToolDef,
  handleGetPageVersion
} from './tools/get_page_version'
import {
  restorePageVersionToolDef,
  handleRestorePageVersion
} from './tools/restore_page_version'

/** Tools that need embeddings / RAG; omitted from the catalog when RAG is off. */
export const RAG_TOOL_NAMES = new Set([
  'search_notes',
  'get_related_notes',
  'suggest_link_targets'
])

// Mutators register commit = handler so dispatch can stage without running the
// write; materializeToolMessage calls commit after user confirm (#924).
// extract_and_save / rename_tag stage inside their handlers (frozen payload).
const P0_TOOLS: AgentToolDef[] = [
  { ...searchNotesToolDef, handler: handleSearchNotes },
  { ...searchProductDocsToolDef, handler: handleSearchProductDocs },
  { ...readBlocksToolDef, handler: handleReadBlocks },
  { ...getBacklinksToolDef, handler: handleGetBacklinks },
  { ...queryTasksToolDef, handler: handleQueryTasks },
  {
    ...createNoteToolDef,
    handler: handleCreateNote,
    commit: handleCreateNote
  },
  {
    ...createTaskToolDef,
    handler: handleCreateTask,
    commit: handleCreateTask
  }
]

/** P1 tools (#602–#604). rename_tag stages in its handler; commit applies. */
const P1_TOOLS: AgentToolDef[] = [
  { ...getRelatedNotesToolDef, handler: handleGetRelatedNotes },
  {
    ...updateBlockToolDef,
    handler: handleUpdateBlock,
    commit: handleUpdateBlock
  },
  {
    ...updateTaskToolDef,
    handler: handleUpdateTask,
    commit: handleUpdateTask
  },
  listTagsTool,
  findUntaggedTool,
  { ...renameTagToolDef, handler: handleRenameTag, commit: commitRenameTag },
  { ...listPageVersionsToolDef, handler: handleListPageVersions },
  { ...getPageVersionToolDef, handler: handleGetPageVersion },
  {
    ...restorePageVersionToolDef,
    handler: handleRestorePageVersion,
    commit: handleRestorePageVersion
  }
]

/** P2 tools (#606–#608). extract_and_save always confirms; commit writes. */
const P2_TOOLS: AgentToolDef[] = [
  { ...getVaultStatisticsToolDef, handler: handleGetVaultStatistics },
  { ...suggestLinkTargetsToolDef, handler: handleSuggestLinkTargets },
  {
    ...extractAndSaveToolDef,
    handler: handleExtractAndSave,
    commit: commitExtractAndSave
  }
]

function registerFiltered(tools: AgentToolDef[]): void {
  const ragOn = getAIAvailability().ragEnabled
  for (const tool of tools) {
    if (!ragOn && RAG_TOOL_NAMES.has(tool.name)) {
      unregisterTool(tool.name)
      continue
    }
    registerTool(tool)
  }
}

/** Register all Phase-4 P0 tools (idempotent — re-registers by name). */
export function registerP0Tools(): void {
  registerFiltered(P0_TOOLS)
}

/** Register all Phase-6 P1 tools (idempotent — re-registers by name). */
export function registerP1Tools(): void {
  registerFiltered(P1_TOOLS)
}

/** Register all Phase-7 P2 tools (idempotent — re-registers by name). */
export function registerP2Tools(): void {
  registerFiltered(P2_TOOLS)
}

/**
 * Re-apply RAG gating against the current ai.features flags. Safe to call
 * when the agent plugin is carried over across loadPlugins without re-init
 * (e.g. master AI stays on while Semantic search flips).
 * Pass `ctx` when available so the agent embed index can start/stop with RAG.
 */
export function reconcileAgentTools(ctx?: PluginContext): void {
  registerP0Tools()
  registerP1Tools()
  registerP2Tools()
  if (ctx) reconcileAgentEmbedIndex(ctx)
}

// Re-export so callers (e.g. tests, index wiring) don't need the per-tool
// module paths. The alias-tagged export keeps listTagsTool/findUntaggedTool
// visible here too, even though they are referenced via the array above.
export { listTagsTool, findUntaggedTool, renameTagTool }
