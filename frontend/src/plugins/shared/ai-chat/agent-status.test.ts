import { describe, expect, it } from 'vitest'
import { agentStatusMessage, toolStatusLabel } from './agent-status'

describe('agent status labels', () => {
  it('maps known tools to friendly labels', () => {
    expect(toolStatusLabel('search_notes')).toBe('Searching notes')
    expect(toolStatusLabel('rename_tag')).toBe('Renaming a tag')
  })

  it('falls back for unknown tools', () => {
    expect(toolStatusLabel('custom_tool')).toBe('Running custom tool')
  })

  it('formats activity messages', () => {
    expect(agentStatusMessage('thinking')).toBe('Thinking…')
    expect(agentStatusMessage('running_tool', 'search_notes')).toBe(
      'Searching notes…'
    )
    expect(agentStatusMessage('waiting_confirmation')).toMatch(/confirmation/)
    expect(agentStatusMessage('done')).toBe('Done')
  })
})
