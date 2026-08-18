import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import {
  ALWAYS_CONFIRM_TOOLS,
  filterToolsForWritePolicy,
  isMutatingTool,
  MUTATING_TOOLS,
  previewForMutation,
  readAgentWritesMode,
  shouldStageTool
} from './write-policy'
import type { AgentToolDef } from './tool-registry'

vi.mock('../../shared/ai-chat/availability', () => ({
  readAIFeatures: vi.fn(() => ({
    enabled: true,
    rag_enabled: true,
    summaries_enabled: false,
    agent_writes: 'confirm'
  }))
}))

import { readAIFeatures } from '../../shared/ai-chat/availability'

const readAIFeaturesMock = vi.mocked(readAIFeatures)

beforeEach(() => {
  readAIFeaturesMock.mockReturnValue({
    enabled: true,
    rag_enabled: true,
    summaries_enabled: false,
    agent_writes: 'confirm'
  })
})

afterEach(() => {
  vi.clearAllMocks()
})

describe('write-policy', () => {
  it('classifies mutators', () => {
    expect(isMutatingTool('create_note')).toBe(true)
    expect(isMutatingTool('search_notes')).toBe(false)
    expect(MUTATING_TOOLS.has('extract_and_save')).toBe(true)
    expect(ALWAYS_CONFIRM_TOOLS.has('rename_tag')).toBe(true)
    expect(ALWAYS_CONFIRM_TOOLS.has('restore_page_version')).toBe(true)
  })

  it('shouldStageTool matrix', () => {
    expect(shouldStageTool('create_note', 'read_only')).toBe(false)
    expect(shouldStageTool('create_note', 'confirm')).toBe(true)
    expect(shouldStageTool('create_note', 'auto')).toBe(false)
    expect(shouldStageTool('rename_tag', 'auto')).toBe(true)
    expect(shouldStageTool('extract_and_save', 'auto')).toBe(true)
    expect(shouldStageTool('restore_page_version', 'auto')).toBe(true)
    expect(shouldStageTool('search_notes', 'confirm')).toBe(false)
  })

  it('filterToolsForWritePolicy omits mutators only in read_only', () => {
    const tools = [
      { name: 'search_notes' },
      { name: 'create_note' },
      { name: 'rename_tag' }
    ] as AgentToolDef[]
    expect(
      filterToolsForWritePolicy(tools, 'confirm').map((t) => t.name)
    ).toEqual(['search_notes', 'create_note', 'rename_tag'])
    expect(
      filterToolsForWritePolicy(tools, 'read_only').map((t) => t.name)
    ).toEqual(['search_notes'])
  })

  it('readAgentWritesMode reads features', () => {
    readAIFeaturesMock.mockReturnValue({
      enabled: true,
      rag_enabled: false,
      summaries_enabled: false,
      agent_writes: 'auto'
    })
    expect(readAgentWritesMode()).toBe('auto')
  })

  it('previewForMutation summarizes common tools', () => {
    expect(
      previewForMutation('create_note', {
        notebook: 'Work',
        page: 'Ideas'
      }).summary
    ).toContain('Work/Ideas')
    expect(
      previewForMutation('update_block', { block_id: 'abc' }).summary
    ).toContain('abc')
    expect(isMutatingTool('restore_page_version')).toBe(true)
    expect(
      previewForMutation('restore_page_version', {
        notebook: 'Work',
        section: 'Journal',
        page: 'Daily',
        version_id: 'v-old'
      }).summary
    ).toContain('Work/Journal/Daily')
  })
})
