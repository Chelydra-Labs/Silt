// Regression guard: the decorative texture overlay must be scoped to the
// page writing surface, not the full viewport (#261). When the texture was
// on `body::before` with `position: fixed; inset: 0`, it textured the
// titlebar, sidebar, and toolbars instead of just the paper/page area.

import { describe, it, expect } from 'vitest'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'

const frontendSrc = resolve(__dirname, '..')

function readFile(relPath: string): string {
  return readFileSync(resolve(frontendSrc, relPath), 'utf-8')
}

describe('texture scope guard (#261)', () => {
  it('index.css does not apply the texture to body::before with position:fixed', () => {
    const css = readFile('index.css')

    // The texture must not be a fixed full-viewport body::before overlay.
    // We check for the combination of body::before and position: fixed in
    // the texture-related section.
    expect(css).not.toMatch(/body::before[^}]*position:\s*fixed/s)
  })

  it('VirtualScrollContainer applies a texture surface class', () => {
    const svelte = readFile('components/VirtualScrollContainer.svelte')

    // The page scroll container must carry a texture surface class so the
    // texture overlay is scoped to the writing area, not the full app.
    expect(svelte).toMatch(/silt-texture-surface/)
  })
})
