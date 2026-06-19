// Plugin surface manager tests (#117).
import { describe, expect, it, beforeEach } from 'vitest'
import {
  registerSurface,
  unregisterSurface,
  unregisterPluginSurfaces,
  getSurfaces,
  onSurfacesChanged,
  resetSurfacesForTests
} from './surfaces'

describe('plugin surface manager (#117)', () => {
  beforeEach(() => {
    resetSurfacesForTests()
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
})
