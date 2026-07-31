// Component tests for EditorUtilityBar (#202) — simplified to act as a container
// for FormatToolbar since control actions have relocated to TabStrip.

import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, cleanup, fireEvent, screen } from '@testing-library/svelte'
import type { Editor } from 'svelte-tiptap'

// Hoisted mock state — vi.mock factories are hoisted above imports, so any
// mutable refs they capture must live inside vi.hoisted.
const mocks = vi.hoisted(() => ({
  config: {
    ui: {
      show_format_toolbar: true as boolean,
      formatting: { color_enabled: true as boolean }
    }
  }
}))

vi.mock('../../settings/store.svelte', () => ({
  settings: mocks
}))
vi.mock('../../lib/systemTheme.svelte', () => ({
  isSystemDark: vi.fn(() => false)
}))

// Use the .stub.svelte companion to avoid pulling in the full TipTap dependency tree.
vi.mock('./FormatToolbar.svelte', async () => {
  const mod = await import('./FormatToolbar.stub.svelte')
  return { default: mod.default }
})

import EditorUtilityBar from './EditorUtilityBar.svelte'

beforeEach(() => {
  cleanup()
  mocks.config = {
    ui: { show_format_toolbar: true, formatting: { color_enabled: true } }
  }
})

describe('EditorUtilityBar (#202 — simplified)', () => {
  it('renders FormatToolbar with correct props', () => {
    const marks = new Set<string>(['bold', 'italic'])
    render(EditorUtilityBar, {
      props: {
        editor: { isDestroyed: false } as unknown as Editor,
        activeMarks: marks
      }
    })
    const ft = document.querySelector('[data-testid="format-toolbar-stub"]')
    expect(ft).toBeTruthy()
    expect(ft?.getAttribute('data-editor')).toBe('present')
    expect(ft?.getAttribute('data-active-marks')).toBe('bold,italic')
    expect(ft?.getAttribute('data-is-dark')).toBe('false')
    expect(ft?.getAttribute('data-color-enabled')).toBe('true')
  })

  it('passes color_enabled: false through to FormatToolbar', () => {
    mocks.config.ui.formatting.color_enabled = false
    render(EditorUtilityBar, {
      props: {
        editor: null,
        activeMarks: new Set<string>()
      }
    })
    const ft = document.querySelector('[data-testid="format-toolbar-stub"]')
    expect(ft?.getAttribute('data-color-enabled')).toBe('false')
  })

  it('dispatches a source-qualified nonce-bearing page tasks event', async () => {
    const listener = vi.fn()
    window.addEventListener('silt:open-tasks-for-page', listener)
    render(EditorUtilityBar, {
      props: {
        editor: null,
        activeMarks: new Set<string>(),
        pageLocator: {
          source: 'linked:meetings',
          notebook: 'Team',
          section: 'Meetings',
          page: 'Weekly sync'
        }
      }
    })

    await fireEvent.click(
      screen.getByRole('button', { name: 'Open tasks on this page' })
    )

    expect(listener).toHaveBeenCalledTimes(1)
    const event = listener.mock.calls[0][0] as CustomEvent
    expect(event.detail).toEqual({
      source: 'linked:meetings',
      notebook: 'Team',
      section: 'Meetings',
      page: 'Weekly sync',
      nonce: expect.any(String)
    })
    expect(event.detail.nonce).not.toBe('')
    window.removeEventListener('silt:open-tasks-for-page', listener)
  })

  it('omits the page action without a real locator', () => {
    render(EditorUtilityBar, {
      props: { editor: null, activeMarks: new Set<string>() }
    })
    expect(
      screen.queryByRole('button', { name: 'Open tasks on this page' })
    ).toBeNull()
  })

  it('renders FormatToolbar when showFormatting is true', () => {
    render(EditorUtilityBar, {
      props: {
        editor: null,
        activeMarks: new Set<string>(),
        showFormatting: true
      }
    })
    expect(
      document.querySelector('[data-testid="format-toolbar-stub"]')
    ).toBeTruthy()
  })

  it('omits FormatToolbar when showFormatting is false but keeps Page Tasks', () => {
    render(EditorUtilityBar, {
      props: {
        editor: null,
        activeMarks: new Set<string>(),
        showFormatting: false,
        pageLocator: {
          source: 'vault',
          notebook: 'NB',
          section: '',
          page: 'PG'
        }
      }
    })
    expect(
      document.querySelector('[data-testid="format-toolbar-stub"]')
    ).toBeNull()
    expect(
      screen.getByRole('button', { name: 'Open tasks on this page' })
    ).toBeInTheDocument()
  })

  it('does not host page zoom controls (they live in the bottom status pill)', () => {
    render(EditorUtilityBar, {
      props: {
        editor: null,
        activeMarks: new Set<string>(),
        showFormatting: false
      }
    })
    expect(screen.queryByRole('group', { name: 'Page zoom' })).toBeNull()
    expect(screen.queryByRole('button', { name: 'Zoom in' })).toBeNull()
  })
})
