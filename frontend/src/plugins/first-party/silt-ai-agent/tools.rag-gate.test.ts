import { beforeEach, describe, expect, it, vi } from 'vitest'
import { clearTools, getTools } from './tool-registry'
import {
  RAG_TOOL_NAMES,
  registerP0Tools,
  registerP1Tools,
  registerP2Tools
} from './tools'

const { mockAvailability } = vi.hoisted(() => ({
  mockAvailability: {
    ragEnabled: false,
    aiEnabled: true,
    summariesEnabled: false,
    chatReady: true,
    embedReady: false,
    drawerAvailable: true,
    features: { enabled: true, rag_enabled: false, summaries_enabled: false }
  }
}))

vi.mock('../../shared/ai-chat/availability', () => ({
  getAIAvailability: () => mockAvailability
}))

// Handlers import heavy deps; mock tool modules used only for registration names.
vi.mock('./tools/search_notes', () => ({
  searchNotesToolDef: {
    name: 'search_notes',
    description: 'd',
    parameters: { type: 'object', properties: {} }
  },
  handleSearchNotes: vi.fn()
}))
vi.mock('./tools/read_blocks', () => ({
  readBlocksToolDef: {
    name: 'read_blocks',
    description: 'd',
    parameters: { type: 'object', properties: {} }
  },
  handleReadBlocks: vi.fn()
}))
vi.mock('./tools/get_backlinks', () => ({
  getBacklinksToolDef: {
    name: 'get_backlinks',
    description: 'd',
    parameters: { type: 'object', properties: {} }
  },
  handleGetBacklinks: vi.fn()
}))
vi.mock('./tools/query_tasks', () => ({
  queryTasksToolDef: {
    name: 'query_tasks',
    description: 'd',
    parameters: { type: 'object', properties: {} }
  },
  handleQueryTasks: vi.fn()
}))
vi.mock('./tools/create_note', () => ({
  createNoteToolDef: {
    name: 'create_note',
    description: 'd',
    parameters: { type: 'object', properties: {} }
  },
  handleCreateNote: vi.fn()
}))
vi.mock('./tools/get_related_notes', () => ({
  getRelatedNotesToolDef: {
    name: 'get_related_notes',
    description: 'd',
    parameters: { type: 'object', properties: {} }
  },
  handleGetRelatedNotes: vi.fn()
}))
vi.mock('./tools/update_block', () => ({
  updateBlockToolDef: {
    name: 'update_block',
    description: 'd',
    parameters: { type: 'object', properties: {} }
  },
  handleUpdateBlock: vi.fn()
}))
vi.mock('./tools/tag_management', () => ({
  listTagsTool: {
    name: 'list_tags',
    description: 'd',
    parameters: { type: 'object', properties: {} },
    handler: vi.fn()
  },
  findUntaggedTool: {
    name: 'find_untagged',
    description: 'd',
    parameters: { type: 'object', properties: {} },
    handler: vi.fn()
  },
  renameTagTool: {
    name: 'rename_tag',
    description: 'd',
    parameters: { type: 'object', properties: {} },
    handler: vi.fn(),
    commit: vi.fn()
  },
  handleRenameTag: vi.fn(),
  commitRenameTag: vi.fn(),
  renameTagToolDef: {
    name: 'rename_tag',
    description: 'd',
    parameters: { type: 'object', properties: {} }
  }
}))
vi.mock('./tools/get_vault_statistics', () => ({
  getVaultStatisticsToolDef: {
    name: 'get_vault_statistics',
    description: 'd',
    parameters: { type: 'object', properties: {} }
  },
  handleGetVaultStatistics: vi.fn()
}))
vi.mock('./tools/suggest_link_targets', () => ({
  suggestLinkTargetsToolDef: {
    name: 'suggest_link_targets',
    description: 'd',
    parameters: { type: 'object', properties: {} }
  },
  handleSuggestLinkTargets: vi.fn()
}))
vi.mock('./tools/extract_and_save', () => ({
  extractAndSaveToolDef: {
    name: 'extract_and_save',
    description: 'd',
    parameters: { type: 'object', properties: {} }
  },
  handleExtractAndSave: vi.fn()
}))

describe('RAG tool gating (#632)', () => {
  beforeEach(() => {
    clearTools()
    mockAvailability.ragEnabled = false
  })

  it('omits embedding tools when RAG is off', () => {
    registerP0Tools()
    registerP1Tools()
    registerP2Tools()
    const names = new Set(getTools().map((t) => t.name))
    for (const name of RAG_TOOL_NAMES) {
      expect(names.has(name)).toBe(false)
    }
    expect(names.has('read_blocks')).toBe(true)
    expect(names.has('query_tasks')).toBe(true)
  })

  it('registers embedding tools when RAG is on', () => {
    mockAvailability.ragEnabled = true
    registerP0Tools()
    registerP1Tools()
    registerP2Tools()
    const names = new Set(getTools().map((t) => t.name))
    for (const name of RAG_TOOL_NAMES) {
      expect(names.has(name)).toBe(true)
    }
  })
})
