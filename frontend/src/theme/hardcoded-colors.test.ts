// Regression guard against hardcoded dark colors that break light mode (#260).
// The theme engine swaps CSS custom properties on :root; any hardcoded
// rgba/hex that doesn't reference a --color-* token is invisible to theme
// switching and produces a mixed dark/light appearance. This test scans the
// source files for known-offensive patterns so they never creep back.

import { describe, it, expect } from 'vitest'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'

const frontendSrc = resolve(__dirname, '..')

const FILES_TO_CHECK = [
  'index.css',
  'components/Sidebar.svelte',
  'components/TipTapEditor.svelte',
  'components/settings/SettingsShell.svelte',
  'components/settings/VaultActionModal.svelte',
  'components/settings/VaultArchiveModal.svelte'
]

// Hardcoded dark colors that should never appear outside of CSS custom
// property fallback values (var(--token, <fallback>) is acceptable; a
// bare rgba(22, 22, 25, ...) or #131a18 is not).
const FORBIDDEN_PATTERNS: RegExp[] = [
  /rgba?\(\s*22\s*,\s*22\s*,\s*25/,
  /#131a18/i
]

// A line is exempt if the forbidden color appears as a CSS var() fallback
// (e.g. `var(--color-surface-app, #131a18)`) — these are safe because the token
// always wins when the theme engine injects it.
const FALLBACK_PATTERN = /var\(--[a-z-]+,\s*$/

function readLines(relPath: string): string[] {
  const abs = resolve(frontendSrc, relPath)
  return readFileSync(abs, 'utf-8').split('\n')
}

describe('hardcoded dark color guard (#260)', () => {
  for (const file of FILES_TO_CHECK) {
    it(`${file} has no hardcoded dark rgba/hex colors`, () => {
      const lines = readLines(file)
      const violations: string[] = []

      for (let i = 0; i < lines.length; i++) {
        const line = lines[i]
        for (const pattern of FORBIDDEN_PATTERNS) {
          if (!pattern.test(line)) continue
          // Skip if this is a CSS var() fallback value.
          const beforeMatch = line.slice(0, line.search(pattern))
          if (FALLBACK_PATTERN.test(beforeMatch)) continue
          violations.push(`  line ${i + 1}: ${line.trim()}`)
        }
      }

      expect(
        violations,
        `Hardcoded dark colors found:\n${violations.join('\n')}`
      ).toEqual([])
    })
  }
})

// Theme System v2 guards (#386, #390). The error family and editor tokens are
// now themeable — emitted by the engine — so index.css must consume them via
// var() and must not carry the legacy static Material-3 error pink (#ffb4ab)
// that won regardless of the active theme. These assertions pin that the
// consolidation is not silently reverted.
describe('themeable error + editor tokens (#386, #390)', () => {
  const indexLines = readLines('index.css')
  const indexText = indexLines.join('\n')

  it('index.css has no static Material-3 error pinks (#ffb4ab / #f43f5e family)', () => {
    // The themeable error family (--color-error*) is engine-emitted now; the
    // CSS must only consume it via var(). Any static --color-error hex
    // declaration — or the leftover bare --error task-priority pinks that
    // reopened bug #386 — re-introduces a fixed color that wins in every
    // theme. All of them must be gone.
    expect(
      indexText,
      'static Material-3 error declarations must be removed'
    ).not.toMatch(/(--color-error|--error)\s*:\s*#/i)
    expect(indexText).not.toMatch(/#ffb4ab/i)
  })

  it('index.css consumes the themeable error family via var()', () => {
    for (const token of [
      '--color-error',
      '--color-error-bg',
      '--color-error-border'
    ]) {
      expect(
        indexText,
        `${token} must be consumed via var() so the active theme wins`
      ).toMatch(
        new RegExp(`var\\(${token.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')}`)
      )
    }
  })

  it('index.css consumes the editor tokens via var()', () => {
    for (const token of ['--color-editor-caret', '--color-editor-selection']) {
      expect(
        indexText,
        `${token} must be consumed via var() so the editor canvas is themed`
      ).toMatch(
        new RegExp(`var\\(${token.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')}`)
      )
    }
  })
})
