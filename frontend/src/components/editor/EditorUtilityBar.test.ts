// Component tests for EditorUtilityBar (#202) — simplified to act as a container
// for FormatToolbar since control actions have relocated to TabStrip.

import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, cleanup } from '@testing-library/svelte'
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
})
