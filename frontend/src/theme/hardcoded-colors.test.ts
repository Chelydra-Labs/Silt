// Regression guard against hardcoded dark colors that break light mode (#260).
// The theme engine swaps CSS custom properties on :root; any hardcoded
// rgba/hex that doesn't reference a --color-* token is invisible to theme
// switching and produces a mixed dark/light appearance. This test scans the
// source files for known-offensive patterns so they never creep back.

import { describe, it, expect } from 'vitest'
import { readFileSync, readdirSync } from 'node:fs'
import { resolve, relative } from 'node:path'

const frontendSrc = resolve(__dirname, '..')

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

// Recursively collect every .svelte and .ts file under frontend/src. The
// narrower scope (frontend src only) is intentional: schema-v2 +
// DisallowUnknownFields makes non-frontend v1-token drift structurally
// impossible, so the repo-wide Go scan that existed in v1
// (theme_migration_invariant_test.go) was deliberately retired.
// Test files are excluded from the hardcoded-color scan below because they
// legitimately reference the forbidden patterns as fixtures (this file
// defines FORBIDDEN_PATTERNS itself).
function walkSrcFiles(dir: string = frontendSrc): string[] {
  const out: string[] = []
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    const full = resolve(dir, entry.name)
    if (entry.isDirectory()) {
      out.push(...walkSrcFiles(full))
    } else if (
      entry.isFile() &&
      (/\.svelte$/.test(entry.name) || /\.ts$/.test(entry.name))
    ) {
      out.push(full)
    }
  }
  return out
}

describe('hardcoded dark color guard (#260)', () => {
  for (const file of walkSrcFiles()) {
    // Skip test files — they legitimately reference forbidden patterns as
    // fixtures (this file defines FORBIDDEN_PATTERNS itself).
    if (/\.test\.[jt]s$/.test(file)) continue
    const rel = relative(frontendSrc, file)
    it(`${rel} has no hardcoded dark rgba/hex colors`, () => {
      const lines = readFileSync(file, 'utf-8').split('\n')
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
    // The themeable error family (--color-error*) is engine-emitted now. The
    // one legitimate place a hex may appear is the @theme block, which
    // declares the startup fallback (overwritten at runtime by the injector)
    // for every v2 token — including --color-error. A second static
    // declaration in the regular CSS rules, or the leftover bare --error
    // task-priority pink, would re-introduce a fixed color that wins in every
    // theme (#386). Strip @theme from the search so the startup fallback is
    // allowed but rule-level statics stay forbidden.
    const themeBlockRe = indexText.match(/@theme\s*\{[\s\S]*?\n\}/)
    const nonThemeCss = themeBlockRe
      ? indexText.replace(themeBlockRe[0], '')
      : indexText
    expect(
      nonThemeCss,
      'static Material-3 error declarations must live only in @theme'
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

// Theme System v2 drift guard. The migration to v2 surface-zone tokens removed
// ~49 files' worth of dead v1 utilities (the void/surface/panel background
// fills, the border-muted/border-zinc edges, and the void text colour). Those
// utilities reference custom properties the engine no longer emits, so Tailwind
// v4 generates no rule for them and the element falls through to transparent —
// the root cause of the transparency / lost-chrome bugs. These two guards
// ensure neither the dead CSS variables nor the dead utility classes creep
// back in.
describe('theme v2 drift guard (#386)', () => {
  const indexLines = readLines('index.css')
  const indexText = indexLines.join('\n')

  // Extract the @theme block — its declarations are what drive Tailwind v4
  // utility generation. A dead v1 token name re-declared here would
  // regenerate the matching dead utility from its --color-* variable.
  const themeBlockMatch = indexText.match(/@theme\s*\{([\s\S]*?)\n\}/)
  const themeBlock = themeBlockMatch ? themeBlockMatch[1] : ''

  // Dead v1 CSS variable names. The bare --color-surface / --color-panel
  // entries carry a trailing `:` so they do NOT match the valid v2 zone names
  // (--color-surface-app, --color-panel-border, …). --color-chrome is a prefix
  // match: any --color-chrome-* residue is dead.
  const DEAD_DECLARATIONS = [
    '--color-void:',
    '--color-surface:',
    '--color-surface-raised:',
    '--color-panel:',
    '--color-border-muted:',
    '--color-border-zinc:',
    '--color-chrome',
    '--color-background:',
    '--color-on-surface:'
  ]

  it('@theme block declares no dead v1 token names', () => {
    const violations = DEAD_DECLARATIONS.filter((name) =>
      new RegExp(name.replace(/[:]/g, '\\$&')).test(themeBlock)
    )
    expect(
      violations,
      `@theme re-declares dead v1 tokens: ${violations.join(', ')}`
    ).toEqual([])
  })

  it('@theme declares every v2 surface zone (sanity)', () => {
    // If the @theme extraction silently broke (unmatched braces), the previous
    // assertion would pass vacuously. Pinning the known-good zone names here
    // guarantees themeBlock actually contains the declarations.
    expect(themeBlock.length, '@theme block must be extracted').toBeGreaterThan(
      0
    )
    for (const zone of [
      '--color-surface-app',
      '--color-surface-sidebar',
      '--color-surface-editor',
      '--color-surface-panel',
      '--color-surface-card',
      '--color-surface-modal',
      '--color-surface-popover'
    ]) {
      expect(themeBlock, `${zone} must be declared in @theme`).toContain(zone)
    }
  })

  it('no .svelte/.ts file under src/ uses dead v1 utility classes', () => {
    // Negative lookaheads on `surface` and `panel` exclude the valid v2 zone
    // names (surface-app, panel-border, …). surface-raised, border-muted, and
    // border-zinc have no v2 namesakes so they need no lookahead.
    const deadUtility =
      /\b(bg|text|border)-(?:void|surface-raised|border-muted|border-zinc|surface(?!-)|panel(?!-))\b/
    const files = walkSrcFiles()
    const violations: string[] = []
    for (const file of files) {
      const lines = readFileSync(file, 'utf-8').split('\n')
      lines.forEach((line, i) => {
        if (deadUtility.test(line)) {
          violations.push(
            `${relative(frontendSrc, file)}:${i + 1}: ${line.trim()}`
          )
        }
      })
    }
    expect(
      violations,
      `dead v1 utility classes found:\n${violations.join('\n')}`
    ).toEqual([])
  })
})
