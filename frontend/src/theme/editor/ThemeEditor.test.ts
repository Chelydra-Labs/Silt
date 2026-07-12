// ThemeEditor shell: load seed, edit injects tokens, reset restores seed.

import { describe, expect, it, beforeEach, afterEach, vi } from 'vitest'
import { tick } from 'svelte'
import { render, screen, cleanup, fireEvent } from '@testing-library/svelte'

const mocks = vi.hoisted(() => ({
  injectTokens: vi.fn(),
  restoreActiveTheme: vi.fn(),
  refreshActiveTheme: vi.fn(),
  setStatus: vi.fn(),
  getThemeJSON: vi.fn(),
  saveCustomTheme: vi.fn(),
  pickImageFile: vi.fn(),
  prepareBackgroundAsset: vi.fn(),
  clearEditorStaging: vi.fn(() => Promise.resolve())
}))

vi.mock('../inject', () => ({ injectTokens: mocks.injectTokens }))
vi.mock('../store.svelte', () => ({
  getThemeJSON: mocks.getThemeJSON,
  saveCustomTheme: mocks.saveCustomTheme,
  pickImageFile: mocks.pickImageFile,
  prepareBackgroundAsset: mocks.prepareBackgroundAsset,
  clearEditorStaging: mocks.clearEditorStaging,
  restoreActiveTheme: mocks.restoreActiveTheme,
  refreshActiveTheme: mocks.refreshActiveTheme,
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
    mocks.refreshActiveTheme.mockReset()
    mocks.refreshActiveTheme.mockResolvedValue(undefined)
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
    // Immersive layout: section nav is a top tablist, not a left rail.
    expect(
      screen.getByRole('tablist', { name: /theme editor sections/i })
    ).toBeTruthy()
    expect(screen.getByRole('tab', { name: /simple/i })).toBeTruthy()
    expect(screen.getByRole('tab', { name: /surfaces/i })).toBeTruthy()
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

  it('surfaces the real IPC error when GetThemeJSON fails', async () => {
    const getThemeJSONFn = vi
      .fn()
      .mockRejectedValue(new Error('theme "gone" not found'))
    render(ThemeEditor, {
      props: {
        themeId: 'gone',
        sourceIsDisk: false,
        onClose: vi.fn(),
        getThemeJSONFn
      }
    })
    await tick()
    await tick()
    expect(screen.getByRole('alert')).toBeTruthy()
    expect(screen.getByText(/theme "gone" not found/i)).toBeTruthy()
    expect(screen.queryByText(/missing dark\/light modes/i)).toBeNull()
  })

  it('re-seeds from GetThemeJSON after a successful save', async () => {
    const savedJson = JSON.stringify({
      ...sampleDoc,
      id: 'user-saved',
      name: 'Saved Custom',
      modes: {
        ...sampleDoc.modes,
        dark: {
          ...sampleDoc.modes.dark,
          surfaces: {
            app: {
              bg: '#0e0f12',
              border: '#1c1d24',
              text: '#dee3e6',
              background: {
                image: 'url("user-saved.assets/photo.png")',
                size: 'cover',
                opacity: 1
              }
            }
          }
        }
      }
    })
    const getThemeJSONFn = vi.fn().mockResolvedValue(savedJson)
    mocks.saveCustomTheme.mockResolvedValue({
      info: { id: 'user-saved', name: 'Saved Custom', source: 'disk' },
      applied: true
    })

    render(ThemeEditor, {
      props: {
        themeId: 'cyber_forest',
        sourceIsDisk: false,
        onClose: vi.fn(),
        injectJson: sampleJson,
        getThemeJSONFn,
        saveCustomThemeFn: mocks.saveCustomTheme
      }
    })
    await tick()

    await fireEvent.click(
      screen.getByRole('button', { name: /save as new theme/i })
    )
    await tick()

    // Name prompt dialog (#531) — confirm with the suggested name.
    const nameDialog = screen.getByTestId('theme-save-name-dialog')
    expect(nameDialog).toBeTruthy()
    const nameInput = screen.getByTestId(
      'theme-save-name-dialog-input'
    ) as HTMLInputElement
    await fireEvent.input(nameInput, { target: { value: 'Saved Custom' } })
    await fireEvent.click(screen.getByTestId('theme-save-name-dialog-confirm'))
    await tick()
    await tick()

    expect(getThemeJSONFn).toHaveBeenCalledWith('user-saved')
    expect(mocks.refreshActiveTheme).toHaveBeenCalled()
    expect(screen.getByText(/Theme Editor · Saved Custom/)).toBeTruthy()
  })

  it('shows per-token Reset controls on Simple essentials', async () => {
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
      screen.getByRole('button', { name: /reset app background/i })
    ).toBeTruthy()
    expect(screen.getByRole('button', { name: /reset app text/i })).toBeTruthy()
    expect(screen.getByRole('button', { name: /reset accent/i })).toBeTruthy()
  })

  it('Surfaces advanced tab shows Surfaces heading and Reset group', async () => {
    render(ThemeEditor, {
      props: {
        themeId: 'cyber_forest',
        sourceIsDisk: false,
        onClose: vi.fn(),
        injectJson: sampleJson
      }
    })
    await tick()
    await fireEvent.click(screen.getByRole('tab', { name: /surfaces/i }))
    await tick()
    expect(screen.getByRole('heading', { name: /^surfaces$/i })).toBeTruthy()
    expect(screen.getByRole('button', { name: /reset group/i })).toBeTruthy()
    expect(
      screen.getByRole('button', { name: /reset background/i })
    ).toBeTruthy()
  })

  it('dirty leave opens confirm dialog; Stay keeps editor open', async () => {
    const onClose = vi.fn()
    render(ThemeEditor, {
      props: {
        themeId: 'cyber_forest',
        sourceIsDisk: false,
        onClose,
        injectJson: sampleJson
      }
    })
    await tick()
    const input = screen.getByLabelText(
      'App text color value'
    ) as HTMLInputElement
    await fireEvent.input(input, { target: { value: '#ffffff' } })
    await fireEvent.blur(input)
    await tick()

    // Header back control is labeled "Appearance" (arrow_back + text).
    await fireEvent.click(screen.getByRole('button', { name: /^appearance$/i }))
    await tick()
    expect(screen.getByTestId('theme-leave-dialog')).toBeTruthy()
    await fireEvent.click(screen.getByTestId('theme-leave-dialog-cancel'))
    await tick()
    expect(screen.queryByTestId('theme-leave-dialog')).toBeNull()
    expect(onClose).not.toHaveBeenCalled()
    expect(screen.getByRole('region', { name: /theme editor/i })).toBeTruthy()
  })

  it('leave confirm discards and closes', async () => {
    const onClose = vi.fn()
    render(ThemeEditor, {
      props: {
        themeId: 'cyber_forest',
        sourceIsDisk: false,
        onClose,
        injectJson: sampleJson
      }
    })
    await tick()
    const input = screen.getByLabelText(
      'App text color value'
    ) as HTMLInputElement
    await fireEvent.input(input, { target: { value: '#ffffff' } })
    await fireEvent.blur(input)
    await tick()

    await fireEvent.click(screen.getByRole('button', { name: /^appearance$/i }))
    await tick()
    await fireEvent.click(screen.getByTestId('theme-leave-dialog-confirm'))
    await tick()
    expect(onClose).toHaveBeenCalled()
    expect(mocks.restoreActiveTheme).toHaveBeenCalled()
  })

  it('Revert opens discard dialog and confirm resets edits', async () => {
    render(ThemeEditor, {
      props: {
        themeId: 'cyber_forest',
        sourceIsDisk: false,
        onClose: vi.fn(),
        injectJson: sampleJson
      }
    })
    await tick()
    const input = screen.getByLabelText(
      'App text color value'
    ) as HTMLInputElement
    await fireEvent.input(input, { target: { value: '#ffffff' } })
    await fireEvent.blur(input)
    await tick()
    expect(input.value).toBe('#ffffff')

    await fireEvent.click(screen.getByRole('button', { name: /revert/i }))
    await tick()
    expect(screen.getByTestId('theme-discard-dialog')).toBeTruthy()
    await fireEvent.click(screen.getByTestId('theme-discard-dialog-confirm'))
    await tick()
    expect(screen.queryByTestId('theme-discard-dialog')).toBeNull()
    const restored = screen.getByLabelText(
      'App text color value'
    ) as HTMLInputElement
    expect(restored.value).toBe('#dee3e6')
  })

  it('Color tab shows derived interaction disclosure with correct seed labels', async () => {
    render(ThemeEditor, {
      props: {
        themeId: 'cyber_forest',
        sourceIsDisk: false,
        onClose: vi.fn(),
        injectJson: sampleJson
      }
    })
    await tick()
    await fireEvent.click(screen.getByRole('tab', { name: /color/i }))
    await tick()
    expect(screen.getByText(/derived interaction/i)).toBeTruthy()
    // Expand details so fields are visible.
    const summary = screen.getByText(/derived interaction/i)
    await fireEvent.click(summary)
    await tick()
    expect(screen.getByText(/hover \(from app background\)/i)).toBeTruthy()
    expect(screen.getByText(/active \(from app background\)/i)).toBeTruthy()
    expect(screen.getByText(/text disabled \(from app text\)/i)).toBeTruthy()
    expect(
      screen.getAllByRole('button', { name: /unlocked|locked/i }).length
    ).toBeGreaterThanOrEqual(3)
    expect(
      screen.getAllByRole('button', { name: /reset to derived/i }).length
    ).toBeGreaterThanOrEqual(3)
  })

  it('derived lock toggle and reset-to-derived are operable', async () => {
    render(ThemeEditor, {
      props: {
        themeId: 'cyber_forest',
        sourceIsDisk: false,
        onClose: vi.fn(),
        injectJson: sampleJson
      }
    })
    await tick()
    await fireEvent.click(screen.getByRole('tab', { name: /color/i }))
    await tick()
    await fireEvent.click(screen.getByText(/derived interaction/i))
    await tick()

    const lockBtns = screen.getAllByRole('button', { name: /^unlocked$/i })
    expect(lockBtns.length).toBeGreaterThanOrEqual(1)
    await fireEvent.click(lockBtns[0])
    await tick()
    expect(
      screen.getAllByRole('button', { name: /^locked$/i }).length
    ).toBeGreaterThanOrEqual(1)

    const resetBtns = screen.getAllByRole('button', {
      name: /reset to derived/i
    })
    await fireEvent.click(resetBtns[0])
    await tick()
    // Reset unlocks that path again.
    expect(
      screen.getAllByRole('button', { name: /^unlocked$/i }).length
    ).toBeGreaterThanOrEqual(1)
  })
})
