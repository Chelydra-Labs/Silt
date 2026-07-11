// Guard against arbitrary Tailwind sizing/type/shadow/grid values in chrome UI
// (#520 settings, #523 repo-wide). Prefer named tokens from index.css @theme
// (text-type-*, text-icon-*, text-display-*, grid-cols-*, shadow-accent-glow, …).
// Modeled on hardcoded-colors.test.ts.

import { describe, it, expect } from 'vitest'
import { readFileSync, readdirSync } from 'node:fs'
import { resolve, relative, join } from 'node:path'

const srcRoot = resolve(__dirname, '..')

// Forbidden patterns in component class strings / style attributes.
const FORBIDDEN: { name: string; re: RegExp }[] = [
  { name: 'arbitrary text size text-[Npx]', re: /text-\[\d+(\.\d+)?px\]/ },
  { name: 'arbitrary grid-cols-[…]', re: /grid-cols-\[[^\]]+\]/ },
  { name: 'arbitrary shadow-[…]', re: /shadow-\[[^\]]+\]/ },
  {
    name: 'arbitrary spacing w/h/gap/p/m-[Npx]',
    re: /(?:^|[\s"'`])(?:w|h|min-w|min-h|max-w|max-h|gap|p|px|py|pt|pr|pb|pl|m|mx|my|mt|mr|mb|ml|top|right|bottom|left|inset)-\[\d+(\.\d+)?px\]/
  }
]

function walkSvelte(dir: string): string[] {
  const out: string[] = []
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    const full = join(dir, entry.name)
    if (entry.isDirectory()) out.push(...walkSvelte(full))
    else if (entry.isFile() && entry.name.endsWith('.svelte')) out.push(full)
  }
  return out
}

describe('chrome UI arbitrary-value guard (#523)', () => {
  const files = walkSvelte(srcRoot)
  expect(files.length).toBeGreaterThan(20)

  for (const file of files) {
    const rel = relative(srcRoot, file).replace(/\\/g, '/')
    it(`${rel} has no forbidden arbitrary type/size/shadow/grid values`, () => {
      const text = readFileSync(file, 'utf-8')
      const violations: string[] = []
      const lines = text.split('\n')
      for (let i = 0; i < lines.length; i++) {
        const line = lines[i]
        // Skip pure comments
        if (/^\s*\/\//.test(line) || /^\s*\*/.test(line)) continue
        for (const { name, re } of FORBIDDEN) {
          // Reset lastIndex for global-safe reuse
          re.lastIndex = 0
          if (re.test(line)) {
            violations.push(`  line ${i + 1} (${name}): ${line.trim()}`)
          }
        }
      }
      expect(
        violations,
        `Arbitrary values found — use design tokens (DESIGN.md §2.1.1):\n${violations.join('\n')}`
      ).toEqual([])
    })
  }
})
