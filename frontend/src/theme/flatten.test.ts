// FE Flatten mirror of backend/themes Theme.Flatten — critical tokens for
// live theme-editor preview.

import { describe, expect, it } from 'vitest'
import { flattenTheme } from './flatten'
import type { ThemeDoc } from './types'

const sample: ThemeDoc = {
  schema_version: '2.0.0',
  id: 'test',
  name: 'Test',
  modes: {
    dark: {
      surfaces: {
        app: { bg: '#0e0f12', border: '#1c1d24', text: '#dee3e6' },
        editor: { bg: '#111216', border: '#21232c', text: '#c0c4cc' }
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
  },
  typography: {
    font_family: "'Plus Jakarta Sans', sans-serif"
  }
}

describe('flattenTheme', () => {
  it('emits --color-surface-app and related app surface keys', () => {
    const tokens = flattenTheme(sample, 'dark')
    expect(tokens['--color-surface-app']).toBe('#0e0f12')
    expect(tokens['--color-surface-app-border']).toBe('#1c1d24')
    expect(tokens['--color-surface-app-text']).toBe('#dee3e6')
    expect(tokens['--color-surface-app-text-muted']).toBe(
      'var(--color-text-muted)'
    )
  })

  it('inherits unset zones via var() chains', () => {
    const tokens = flattenTheme(sample, 'dark')
    expect(tokens['--color-surface-sidebar']).toBe('var(--color-surface-app)')
    expect(tokens['--color-surface-panel']).toBe('var(--color-surface-app)')
    expect(tokens['--color-surface-card']).toBe('var(--color-surface-panel)')
  })

  it('emits accents, status, error, interaction, geometry', () => {
    const tokens = flattenTheme(sample, 'dark')
    expect(tokens['--color-accent-primary-start']).toBe('#2dd4bf')
    expect(tokens['--color-status-warn']).toBe('#e0b04a')
    expect(tokens['--color-error']).toBe('#e8728a')
    expect(tokens['--color-hover']).toBe('#1b1d25')
    expect(tokens['--radius-md']).toBe('8px')
    expect(tokens['--color-text-muted']).toBe('#8b8b94')
    expect(tokens['--color-text-primary']).toBe('var(--color-surface-app-text)')
  })

  it('emits editor defaults when editor block omitted', () => {
    const tokens = flattenTheme(sample, 'dark')
    expect(tokens['--color-editor-caret']).toBe(
      'var(--color-accent-primary-start)'
    )
  })

  it('emits concrete editor zone when authored', () => {
    const tokens = flattenTheme(sample, 'dark')
    expect(tokens['--color-surface-editor']).toBe('#111216')
    expect(tokens['--color-surface-editor-text']).toBe('#c0c4cc')
  })

  it('emits typography when present', () => {
    const tokens = flattenTheme(sample, 'dark')
    expect(tokens['--font-body']).toBe("'Plus Jakarta Sans', sans-serif")
  })

  it('emits background tokens when a zone declares background', () => {
    const withBg: ThemeDoc = structuredClone(sample)
    withBg.modes.dark.surfaces.app.background = {
      image: 'url("data:image/png;base64,xx")',
      size: 'cover',
      opacity: 0.4,
      blend: 'soft-light',
      position: 'center',
      scrim: '#0e0f12'
    }
    const tokens = flattenTheme(withBg, 'dark')
    expect(tokens['--silt-bg-app-display']).toBe('block')
    expect(tokens['--silt-bg-app-image']).toBe(
      'url("data:image/png;base64,xx")'
    )
    expect(tokens['--silt-bg-app-size']).toBe('cover')
    expect(tokens['--silt-bg-app-opacity']).toBe('0.4')
    expect(tokens['--silt-bg-app-blend']).toBe('soft-light')
    expect(tokens['--silt-bg-app-scrim']).toBe('#0e0f12')
  })

  it('always emits opacity (default 0) when background is present', () => {
    const withBg: ThemeDoc = structuredClone(sample)
    withBg.modes.dark.surfaces.app.background = {
      image: 'url("data:image/png;base64,xx")'
    }
    const tokens = flattenTheme(withBg, 'dark')
    expect(tokens['--silt-bg-app-opacity']).toBe('0')
  })

  it('switches light mode tokens', () => {
    const tokens = flattenTheme(sample, 'light')
    expect(tokens['--color-surface-app']).toBe('#f8fafc')
    expect(tokens['--color-accent-primary-start']).toBe('#0d9488')
  })
})
