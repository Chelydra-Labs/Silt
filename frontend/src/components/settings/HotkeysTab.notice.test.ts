// Gating coverage for the v1 default-keymap migration notice (#868) inside the
// HotkeysTab mount. The presentational child (HotkeysDefaultsNotice.svelte)
// is covered by HotkeysDefaultsNotice.test.ts; this file pins the derivation
// `v1NoticeDismissed = !tips.includes(V1_NOTICE) || tips.includes(V1_ACK)`
// at the parent, where the dismissed_tips state actually drives the render.
//
// Mock pattern per AGENTS.md: vi.hoisted + createAppIpcMocks over the
// `$silt-app` alias (the alias resolves to the same file production imports
// via the relative bindings path). The whole settings store is mocked with a
// plain object so each test can drive `settings.config.ui.dismissed_tips`
// into the relevant state before render (same pattern as GeneralTab.test.ts).

import { describe, expect, it, beforeEach, afterEach, vi } from 'vitest'
import { tick } from 'svelte'
import { render, screen, cleanup, fireEvent } from '@testing-library/svelte'

// Plain store stand-in the component reads `settings.config` from. Re-assigned
// per test (the component's $effect reads it on mount to build `draft`).
const mocks = vi.hoisted(() => ({
  settings: {
    config: null as {
      ui: { dismissed_tips: string[] }
      hotkeys: Record<string, string>
    } | null,
    loading: false,
    saving: false,
    error: '',
    dirty: false,
    pendingExternal: false
  },
  saveConfig: vi.fn(async () => true),
  reloadFromBackend: vi.fn(async () => {}),
  appendDismissedTip: vi.fn(async () => true)
}))

const appMocks = vi.hoisted(() => createAppIpcMocks({}))
vi.mock('$silt-app', () => appMocks)
vi.mock('../../settings/store.svelte', () => ({
  settings: mocks.settings,
  saveConfig: mocks.saveConfig,
  reloadFromBackend: mocks.reloadFromBackend,
  appendDismissedTip: mocks.appendDismissedTip
}))

import HotkeysTab from './HotkeysTab.svelte'

const V1_NOTICE = 'hotkeys_defaults_v1_notice'
const V1_ACK = 'hotkeys_defaults_v1_ack'

function setDismissedTips(tips: string[]): void {
  mocks.settings.config = {
    ui: { dismissed_tips: tips },
    hotkeys: { open_search: 'Ctrl+P' }
  }
}

// The component builds `draft` from settings.config inside an $effect that
// runs after the initial paint; flush microtasks so the {:else} branch (where
// the notice lives) has rendered.
async function flush(): Promise<void> {
  await tick()
  await new Promise((r) => setTimeout(r, 0))
}

describe('HotkeysTab v1-defaults notice gating (#868)', () => {
  beforeEach(() => {
    mocks.settings.dirty = false
    mocks.settings.pendingExternal = false
    mocks.settings.error = ''
    mocks.appendDismissedTip.mockReset()
    mocks.appendDismissedTip.mockResolvedValue(true)
  })

  afterEach(() => {
    mocks.settings.config = null
    cleanup()
  })

  it('shows the notice when the v1 stamp is present and ack is absent', async () => {
    setDismissedTips([V1_NOTICE])
    render(HotkeysTab)
    await flush()
    expect(
      await screen.findByTestId('hotkeys-defaults-notice')
    ).toBeInTheDocument()
  })

  it('hides the notice once the v1 ack stamp is also present', async () => {
    setDismissedTips([V1_NOTICE, V1_ACK])
    render(HotkeysTab)
    await flush()
    expect(screen.queryByTestId('hotkeys-defaults-notice')).toBeNull()
  })

  it('hides the notice when the v1 stamp is absent (no migration happened)', async () => {
    setDismissedTips([])
    render(HotkeysTab)
    await flush()
    expect(screen.queryByTestId('hotkeys-defaults-notice')).toBeNull()
  })

  it('stays hidden when only the ack stamp is present (no prior notice)', async () => {
    setDismissedTips([V1_ACK])
    render(HotkeysTab)
    await flush()
    expect(screen.queryByTestId('hotkeys-defaults-notice')).toBeNull()
  })

  it('clicking Got it dismisses via appendDismissedTip(hotkeys_defaults_v1_ack)', async () => {
    setDismissedTips([V1_NOTICE])
    render(HotkeysTab)
    await flush()
    await fireEvent.click(
      screen.getByRole('button', { name: 'Dismiss notice' })
    )
    expect(mocks.appendDismissedTip).toHaveBeenCalledWith(V1_ACK)
    expect(mocks.appendDismissedTip).toHaveBeenCalledTimes(1)
  })
})
