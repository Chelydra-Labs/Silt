// ThemeEditor shell: load seed, edit injects tokens, reset restores seed.

import { describe, expect, it, beforeEach, afterEach, vi } from 'vitest'
import { tick } from 'svelte'
import { render, screen, cleanup, fireEvent } from '@testing-library/svelte'

const mocks = vi.hoisted(() => ({
  injectTokens: vi.fn(),
  restoreActiveTheme: vi.fn(),
  setStatus: vi.fn(),
  getThemeJSON: vi.fn(),
  saveCustomTheme: vi.fn(),
  pickImageFile: vi.fn(),
  prepareBackgroundAsset: vi.fn()
}))

vi.mock('../inject', () => ({ injectTokens: mocks.injectTokens }))
vi.mock('../store.svelte', () => ({
  getThemeJSON: mocks.getThemeJSON,
  saveCustomTheme: mocks.saveCustomTheme,
  pickImageFile: mocks.pickImageFile,
  prepareBackgroundAsset: mocks.prepareBackgroundAsset,
  restoreActiveTheme: mocks.restoreActiveTheme,
  setStatus: mocks.setStatus
}))

import ThemeEditor from './ThemeEditor.svelte'
import type { ThemeDoc } from '../types'

const sampleDoc: ThemeDoc = {
  schema_version: '2.0.0',
  id: 'cyber_forest',
  name: 'Cyber Forest',
  modes: {
    dark: {
      surfaces: {
        app: { bg: '#0e0f12', border: '#1c1d24', text: '#dee3e6' }
      },
      hover: '#1b1d25',
      active: '#20232e',
      border_active: '#3e4153',
      border_focus: '#535770',
      text_muted: '#8b8b94',
      text_disabled: '#4b5563',
      accent: {
        primary: {
          start: '#2dd4bf',
          end: '#0d9488',
          glow: 'rgba(45, 212, 191, 0.15)'
        },
        secondary: {
          start: '#6366f1',
          end: '#a855f7',
          glow: 'rgba(99, 102, 241, 0.12)'
        }
      },
      status: { warn: '#e0b04a', danger: '#dd5a72', success: '#37b594' },
      error: { fg: '#e8728a', bg: '#171015', border: '#4a2a38' },
      radius: {
        sm: '4px',
        md: '8px',
        lg: '12px',
        xl: '16px',
        full: '9999px'
      }
    },
    light: {
      surfaces: {
        app: { bg: '#f8fafc', border: '#e2e8f0', text: '#0f172a' }
      },
      hover: '#e2e8f0',
      active: '#cbd5e1',
      border_active: '#94a3b8',
      border_focus: '#64748b',
      text_muted: '#4d5667',
      text_disabled: '#94a3b8',
      accent: {
        primary: {
          start: '#0d9488',
          end: '#115e59',
          glow: 'rgba(13, 148, 136, 0.12)'
        },
        secondary: {
          start: '#4f46e5',
          end: '#7c3aed',
          glow: 'rgba(79, 70, 229, 0.1)'
        }
      },
      status: { warn: '#b45309', danger: '#be123c', success: '#0f766e' },
      error: { fg: '#be123c', bg: '#fff1f2', border: '#fecdd3' }
    }
  }
}

const sampleJson = JSON.stringify(sampleDoc)

describe('ThemeEditor', () => {
  beforeEach(() => {
    mocks.injectTokens.mockReset()
    mocks.restoreActiveTheme.mockReset()
    mocks.setStatus.mockReset()
    mocks.getThemeJSON.mockReset()
    mocks.saveCustomTheme.mockReset()
    mocks.getThemeJSON.mockResolvedValue(sampleJson)
    // rAF in jsdom: run callbacks immediately so preview inject is testable.
    vi.stubGlobal('requestAnimationFrame', (cb: FrameRequestCallback) => {
      cb(0)
      return 1
    })
    vi.stubGlobal('cancelAnimationFrame', () => {})
  })

  afterEach(() => {
    cleanup()
    vi.unstubAllGlobals()
  })

  it('opens with theme name and simple essentials', async () => {
    render(ThemeEditor, {
      props: {
        themeId: 'cyber_forest',
        sourceIsDisk: false,
        onClose: vi.fn(),
        injectJson: sampleJson
      }
    })
    await tick()
    expect(screen.getByRole('region', { name: /theme editor/i })).toBeTruthy()
    expect(screen.getByText(/Theme Editor · Cyber Forest/)).toBeTruthy()
    expect(screen.getByLabelText('App background')).toBeTruthy()
    expect(screen.getByLabelText('App text')).toBeTruthy()
    expect(screen.getByLabelText('Accent')).toBeTruthy()
    expect(screen.getByLabelText('Body font')).toBeTruthy()
    expect(screen.getByLabelText('Corner radius')).toBeTruthy()
  })

  it('injects flattened tokens on open', async () => {
    render(ThemeEditor, {
      props: {
        themeId: 'cyber_forest',
        sourceIsDisk: false,
        onClose: vi.fn(),
        injectJson: sampleJson
      }
    })
    await tick()
    expect(mocks.injectTokens).toHaveBeenCalled()
    const last = mocks.injectTokens.mock.calls.at(-1)?.[0] as Record<
      string,
      string
    >
    expect(last['--color-surface-app']).toBe('#0e0f12')
  })

  it('editing app text re-injects tokens', async () => {
    render(ThemeEditor, {
      props: {
        themeId: 'cyber_forest',
        sourceIsDisk: false,
        onClose: vi.fn(),
        injectJson: sampleJson
      }
    })
    await tick()
    mocks.injectTokens.mockClear()

    const input = screen.getByLabelText(
      'App text color value'
    ) as HTMLInputElement
    await fireEvent.input(input, { target: { value: '#ffffff' } })
    await fireEvent.blur(input)
    await tick()

    expect(mocks.injectTokens).toHaveBeenCalled()
    const last = mocks.injectTokens.mock.calls.at(-1)?.[0] as Record<
      string,
      string
    >
    expect(last['--color-surface-app-text']).toBe('#ffffff')
  })

  it('Revert is disabled when clean and restoreActiveTheme runs on unmount', async () => {
    const { unmount } = render(ThemeEditor, {
      props: {
        themeId: 'cyber_forest',
        sourceIsDisk: false,
        onClose: vi.fn(),
        injectJson: sampleJson
      }
    })
    await tick()
    const revert = screen.getByRole('button', { name: /revert/i })
    expect(revert).toBeDisabled()
    unmount()
    expect(mocks.restoreActiveTheme).toHaveBeenCalled()
  })

  it('shows Save as new for non-disk seeds', async () => {
    render(ThemeEditor, {
      props: {
        themeId: 'cyber_forest',
        sourceIsDisk: false,
        onClose: vi.fn(),
        injectJson: sampleJson
      }
    })
    await tick()
    expect(
      screen.getByRole('button', { name: /save as new theme/i })
    ).toBeTruthy()
  })

  it('shows Save + Save as new for disk themes', async () => {
    render(ThemeEditor, {
      props: {
        themeId: 'user-custom',
        sourceIsDisk: true,
        onClose: vi.fn(),
        injectJson: JSON.stringify({ ...sampleDoc, id: 'user-custom' })
      }
    })
    await tick()
    expect(screen.getByRole('button', { name: /^save$/i })).toBeTruthy()
    expect(screen.getByRole('button', { name: /save as new/i })).toBeTruthy()
  })
})
