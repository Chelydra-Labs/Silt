// Guard against arbitrary Tailwind sizing/type/shadow/grid values in the
// settings UI (#520). Prefer named tokens from index.css @theme
// (text-type-*, text-icon-*, grid-cols-settings-theme, shadow-accent-glow, …).
// Modeled on hardcoded-colors.test.ts.

import { describe, it, expect } from 'vitest'
import { readFileSync, readdirSync } from 'node:fs'
import { resolve, relative, join } from 'node:path'

const settingsDir = resolve(__dirname, '../components/settings')

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

describe('settings UI arbitrary-value guard (#520)', () => {
  const files = walkSvelte(settingsDir)
  expect(files.length).toBeGreaterThan(5)

  for (const file of files) {
    const rel = relative(settingsDir, file).replace(/\\/g, '/')
    it(`${rel} has no forbidden arbitrary type/size/shadow/grid values`, () => {
      const text = readFileSync(file, 'utf-8')
      const violations: string[] = []
      const lines = text.split('\n')
      for (let i = 0; i < lines.length; i++) {
        const line = lines[i]
        // Skip pure comments
        if (/^\s*\/\//.test(line) || /^\s*\*/.test(line)) continue
        for (const { name, re } of FORBIDDEN) {
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
