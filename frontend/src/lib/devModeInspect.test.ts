import { beforeEach, describe, expect, it, vi } from 'vitest'

const openDevTools = vi.fn()
vi.mock('../../bindings/silt/app.js', () => ({
  OpenDevTools: (...args: unknown[]) => openDevTools(...args)
}))

vi.mock('../settings/store.svelte', () => ({
  settings: {
    config: { ui: { open_devtools_on_startup: false } }
  }
}))

import { settings } from '../settings/store.svelte'
import { isDevMode, openInspect } from './devModeInspect'

describe('devModeInspect', () => {
  beforeEach(() => {
    openDevTools.mockReset()
    ;(
      settings as { config: { ui: { open_devtools_on_startup?: boolean } } }
    ).config.ui.open_devtools_on_startup = false
  })

  it('isDevMode follows vault flag only', () => {
    expect(isDevMode()).toBe(false)
    ;(
      settings as { config: { ui: { open_devtools_on_startup?: boolean } } }
    ).config.ui.open_devtools_on_startup = true
    expect(isDevMode()).toBe(true)
  })

  it('isDevMode ignores sessionStorage spoof', () => {
    try {
      sessionStorage.setItem('silt_debug', '1')
    } catch {
      /* ignore */
    }
    expect(isDevMode()).toBe(false)
  })

  it('openInspect returns ok on success', async () => {
    openDevTools.mockResolvedValue(undefined)
    await expect(openInspect()).resolves.toEqual({ ok: true })
  })

  it('openInspect returns error payload on failure', async () => {
    openDevTools.mockRejectedValue(new Error('nope'))
    await expect(openInspect()).resolves.toEqual({
      ok: false,
      error: 'nope'
    })
  })
})
