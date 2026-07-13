import { describe, expect, it } from 'vitest'
import { ACTION_CATALOG, enabledActions, isActionEnabled } from './catalog'
import { resolveSettings } from './settings'

describe('action catalog', () => {
  it('ships the spike set', () => {
    const ids = ACTION_CATALOG.map((a) => a.id)
    expect(ids).toEqual([
      'draft-expand',
      'rewrite-succinct',
      'improve-clarity',
      'extract-tasks',
      'suggest-tags',
      'suggest-related'
    ])
  })

  it('filters disabled actions', () => {
    const settings = resolveSettings({
      actions_enabled: { 'suggest-related': false }
    })
    expect(isActionEnabled(settings, 'suggest-related')).toBe(false)
    expect(enabledActions(settings).map((a) => a.id)).not.toContain(
      'suggest-related'
    )
  })
})
