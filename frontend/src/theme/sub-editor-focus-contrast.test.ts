// Sub-editor focus indicator contrast CI gate. Asserts the ≥3:1 WCAG
// non-text contrast contract between the focused writing-surface treatment
// (--color-accent-primary-start) and the modal background
// (--color-surface-modal) for every first-class theme in both modes, using
// the same flatten-golden resolved values the runtime consumes. Mirrors the
// caret-contrast gate: the contract becomes self-enforcing, so a theme that
// drops the focus indicator below the visibility bar fails CI instead of
// shipping.

import { describe, expect, it } from 'vitest'
import { readFileSync, readdirSync } from 'node:fs'
import { resolve, basename } from 'node:path'
import { contrastRatioWCAG } from './contrast'

const goldensDir = resolve(__dirname, '__fixtures__/flatten-goldens')
const goldenFiles = readdirSync(goldensDir).filter((f) => f.endsWith('.json'))

describe('sub-editor focus indicator contrast ≥3:1 for every theme × mode (CI gate)', () => {
  for (const file of goldenFiles) {
    const id = basename(file, '.json') // e.g. "cyber_forest.light"
    it(`${id}`, () => {
      const raw = readFileSync(resolve(goldensDir, file), 'utf8')
      const golden = JSON.parse(raw) as Record<string, string>
      const accent = golden['--color-accent-primary-start']
      const surface = golden['--color-surface-modal']
      expect(accent, 'accent token must exist').toBeDefined()
      expect(surface, 'surface-modal token must exist').toBeDefined()
      const ratio = contrastRatioWCAG(accent, surface)
      expect(
        ratio,
        `contrast ratio must be computable for ${id}`
      ).not.toBeNull()
      expect(
        ratio!,
        `${id}: accent ${accent} on surface ${surface} = ${ratio?.toFixed(3)}, need ≥3`
      ).toBeGreaterThanOrEqual(3)
    })
  }
})
