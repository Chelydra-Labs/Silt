// Plugin surface manager tests (#117, #158).
import { describe, expect, it, beforeEach, vi } from 'vitest'

// Mock the grants module so the registry-internal gate can be controlled
// per-test without hitting wailsjs IPC (#158).
vi.mock('./grants.svelte', () => ({
  isGranted: vi.fn(() => true),
  initGrants: vi.fn(),
  refreshGrants: vi.fn(),
  resetGrantsForTests: vi.fn(),
  setGrantsForTests: vi.fn()
}))

import {
  registerSurface,
  unregisterSurface,
  unregisterPluginSurfaces,
  getSurfaces,
  onSurfacesChanged,
  resetSurfacesForTests
} from './surfaces'
import { isGranted } from './grants.svelte'

describe('plugin surface manager (#117, #158)', () => {
  beforeEach(() => {
    resetSurfacesForTests()
    vi.mocked(isGranted).mockReturnValue(true)
  })

  it('registers and retrieves surfaces by kind', () => {
    registerSurface({
      id: 'p:panel1',
      pluginID: 'p',
      kind: 'sidebar-panel',
      label: 'Panel 1',
      html: '<div>hi</div>'
    })
    registerSurface({
      id: 'p:status',
      pluginID: 'p',
      kind: 'status-bar-item',
      label: 'Status',
      html: '<span>ok</span>'
    })
    expect(getSurfaces()).toHaveLength(2)
    expect(getSurfaces('sidebar-panel')).toHaveLength(1)
    expect(getSurfaces('status-bar-item')).toHaveLength(1)
  })

  it('unregister removes a single surface', () => {
    registerSurface({
      id: 'p:x',
      pluginID: 'p',
      kind: 'sidebar-panel',
      label: 'X',
      html: '<div/>'
    })
    unregisterSurface('p:x')
    expect(getSurfaces()).toHaveLength(0)
  })

  it('unregisterPluginSurfaces removes all surfaces for a plugin', () => {
    registerSurface({
      id: 'p:a',
      pluginID: 'p',
      kind: 'sidebar-panel',
      label: 'A',
      html: '<div/>'
    })
    registerSurface({
      id: 'p:b',
      pluginID: 'p',
      kind: 'modal',
      label: 'B',
      html: '<div/>'
    })
    registerSurface({
      id: 'q:c',
      pluginID: 'q',
      kind: 'sidebar-panel',
      label: 'C',
      html: '<div/>'
    })
    unregisterPluginSurfaces('p')
    expect(getSurfaces()).toHaveLength(1)
    expect(getSurfaces()[0].pluginID).toBe('q')
  })

  it('notifies listeners on register/unregister', () => {
    const calls: number[] = []
    const off = onSurfacesChanged(() => calls.push(1))
    registerSurface({
      id: 'p:x',
      pluginID: 'p',
      kind: 'sidebar-panel',
      label: 'X',
      html: '<div/>'
    })
    unregisterSurface('p:x')
    expect(calls.length).toBeGreaterThanOrEqual(2)
    off()
  })

  it('rejects a surface without id, pluginID, or html', () => {
    expect(() =>
      registerSurface({
        id: '',
        pluginID: 'p',
        kind: 'sidebar-panel',
        label: 'X',
        html: '<x/>'
      })
    ).toThrow()
    expect(() =>
      registerSurface({
        id: 'x',
        pluginID: '',
        kind: 'sidebar-panel',
        label: 'X',
        html: '<x/>'
      })
    ).toThrow()
  })

  // --- #158: registry-internal capability gate -------------------------------

  it('refuses surfaces without ui-surface grant', () => {
    vi.mocked(isGranted).mockReturnValue(false)
    const off = registerSurface({
      id: 'ungranted:panel',
      pluginID: 'ungranted',
      kind: 'sidebar-panel',
      label: 'Blocked',
      html: '<div/>'
    })
    off()
    expect(getSurfaces()).toHaveLength(0)
  })

  // --- #221: first-party compiled-component path + chrome-rendered onClick ---

  it('registers a first-party note-banner surface by component (no html)', () => {
    const Cmp = {} // a compiled Svelte component stand-in
    registerSurface({
      id: 'silt-ai-summary:banner',
      pluginID: 'silt-ai-summary',
      kind: 'note-banner',
      label: 'AI summary',
      component: Cmp
    })
    const banners = getSurfaces('note-banner')
    expect(banners).toHaveLength(1)
    expect(banners[0].component).toBe(Cmp)
    expect(banners[0].html).toBeUndefined()
  })

  it('accepts a chrome-rendered status-bar item with label/icon + onClick (no html/component)', () => {
    const onClick = vi.fn()
    registerSurface({
      id: 'silt-ai-summary:reopen',
      pluginID: 'silt-ai-summary',
      kind: 'status-bar-item',
      label: 'Show AI summary',
      icon: 'auto_awesome',
      onClick
    })
    const items = getSurfaces('status-bar-item')
    expect(items).toHaveLength(1)
    expect(items[0].onClick).toBe(onClick)
  })

  it('still requires html or component for content-rendering kinds', () => {
    expect(() =>
      registerSurface({
        id: 'p:bare',
        pluginID: 'p',
        kind: 'note-banner',
        label: 'Bare'
      })
    ).toThrow(/html or component/)
    // A bare status-bar item is fine (chrome-rendered from label/icon).
    expect(() =>
      registerSurface({
        id: 'p:chip',
        pluginID: 'p',
        kind: 'status-bar-item',
        label: 'Chip'
      })
    ).not.toThrow()
  })
})
