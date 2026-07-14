// Caret contrast CI gate. Asserts the documented ≥4.5:1 contract between
// --color-editor-caret and --color-surface-editor for every first-class theme
// in both modes, using the same flatten-golden resolved values the runtime
// consumes. This test exists because a hand-rolled oklch→sRGB conversion
// silently disagreed with culori's wcagContrast, allowing a below-contract
// caret to ship with an incorrect verification number. The gate makes the
// contract self-enforcing so it cannot regress without a test failure.

import { describe, expect, it } from 'vitest'
import { readFileSync, readdirSync } from 'node:fs'
import { resolve, basename } from 'node:path'
import { contrastRatioWCAG } from './contrast'

const goldensDir = resolve(__dirname, '__fixtures__/flatten-goldens')
const goldenFiles = readdirSync(goldensDir).filter((f) => f.endsWith('.json'))

describe('caret contrast ≥4.5:1 for every theme × mode (CI gate)', () => {
  for (const file of goldenFiles) {
    const id = basename(file, '.json') // e.g. "cyber_forest.light"
    it(`${id}`, () => {
      const raw = readFileSync(resolve(goldensDir, file), 'utf8')
      const golden = JSON.parse(raw) as Record<string, string>
      const caret = golden['--color-editor-caret']
      const surface = golden['--color-surface-editor']
      expect(caret, 'caret token must exist').toBeDefined()
      expect(surface, 'surface-editor token must exist').toBeDefined()
      const ratio = contrastRatioWCAG(caret, surface)
      expect(
        ratio,
        `contrast ratio must be computable for ${id}`
      ).not.toBeNull()
      expect(
        ratio!,
        `${id}: caret ${caret} on surface ${surface} = ${ratio?.toFixed(3)}, need ≥4.5`
      ).toBeGreaterThanOrEqual(4.5)
    })
  }
})
