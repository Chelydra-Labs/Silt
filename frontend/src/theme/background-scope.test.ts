// Guards the editor-zone background overlay: its scope (#261 regression) and
// the per-zone background-image/size contract. The decorative overlay must be
// scoped to the page writing surface, not the full viewport. When it lived on
// `body::before` with `position: fixed; inset: 0`, it painted the titlebar,
// sidebar, and toolbars instead of just the paper/page area. In v2 the overlay
// is driven by per-zone `--silt-bg-editor-*` tokens (image/size/position/…)
// consumed by `.silt-texture-surface::before`; the regression guard and the
// token contract are both asserted here.

import { describe, it, expect } from 'vitest'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'

const frontendSrc = resolve(__dirname, '..')

function readFile(relPath: string): string {
  return readFileSync(resolve(frontendSrc, relPath), 'utf-8')
}

describe('background scope guard (#261)', () => {
  it('index.css does not apply the overlay to body::before with position:fixed', () => {
    const css = readFile('index.css')

    // The overlay must not be a fixed full-viewport body::before layer.
    expect(css).not.toMatch(/body::before[^}]*position:\s*fixed/s)
  })

  it('index.css overlay uses sticky positioning (not absolute)', () => {
    const css = readFile('index.css')

    // The .silt-texture-surface::before must use position: sticky so the
    // background stays pinned within the scroll viewport on long pages.
    // position: absolute would scroll out of view below the fold.
    expect(css).toMatch(/silt-texture-surface::before[^}]*position:\s*sticky/s)
    expect(css).not.toMatch(
      /silt-texture-surface::before[^}]*position:\s*absolute/s
    )
  })

  it('VirtualScrollContainer applies the surface class to its scroll container', () => {
    const svelte = readFile('components/VirtualScrollContainer.svelte')

    // The page scroll container must carry the surface class so the overlay
    // is scoped to the writing area, not the full app.
    expect(svelte).toMatch(/silt-texture-surface/)
  })

  it('index.css does not use a blanket child selector for content lifting', () => {
    const css = readFile('index.css')

    // A `.silt-texture-surface > *` rule would create a stacking context on
    // every direct child, trapping the editor's fixed-position overlays
    // (slash menu, color picker, link modal). Content must be lifted by a
    // single positioned wrapper in the component instead.
    expect(css).not.toMatch(/silt-texture-surface\s*>\s*\*/)
  })

  it('VirtualScrollContainer lifts content via a positioned wrapper', () => {
    const svelte = readFile('components/VirtualScrollContainer.svelte')

    // The scroll content (nav, header, editor) must sit inside a single
    // wrapper that establishes the stacking lift (relative z-[1]) so the
    // overlay paints below the content without trapping each child in its
    // own stacking context.
    expect(svelte).toMatch(/relative z-\[1\][^"]*flex flex-col/)
  })

  it('overlay ::before uses viewport units for height/margin (not %)', () => {
    const css = readFile('index.css')

    // CSS spec resolves margin-bottom percentages against the container's
    // WIDTH, not height. So "height:100%; margin-bottom:-100%" over-cancels
    // on wide containers, pulling the content wrapper above the visible area
    // and hiding all text. Viewport units (vh) are real lengths that cancel
    // exactly regardless of aspect ratio.
    // Check the actual property declarations (not the explanatory comment).
    expect(css).toMatch(/^\s+height:\s*100vh;/m)
    expect(css).toMatch(/^\s+margin-bottom:\s*-100vh;/m)
  })

  it('editor-zone overlay consumes the per-zone background tokens', () => {
    const css = readFile('index.css')

    // The overlay rule is the consumer of the theme-emitted per-zone
    // background tokens (themes opt in via --silt-bg-editor-*; the rule
    // provides safe fallbacks so non-textured themes see no layer). Each
    // token referenced here is part of the v2 background contract — pinning
    // them guards against silently renaming a token without updating the CSS.
    const overlayRule =
      /silt-texture-surface::before\s*\{[^}]*\}/s.exec(css)?.[0] ?? ''
    expect(
      overlayRule,
      '.silt-texture-surface::before rule must exist'
    ).toBeTruthy()
    for (const token of [
      '--silt-bg-editor-display',
      '--silt-bg-editor-image',
      '--silt-bg-editor-size',
      '--silt-bg-editor-position',
      '--silt-bg-editor-opacity',
      '--silt-bg-editor-blend'
    ]) {
      expect(overlayRule, `${token} must drive the overlay`).toContain(token)
    }
  })

  it('editor-zone overlay declares background-image and background-size', () => {
    const css = readFile('index.css')

    // The overlay must expose both the image source and the sizing strategy
    // (tile via background-repeat:repeat + a sized background, or cover via
    // --silt-bg-editor-size:cover). Dropping either declaration would leave
    // themes unable to paint or scale the editor background at all.
    const overlayRule =
      /silt-texture-surface::before\s*\{[^}]*\}/s.exec(css)?.[0] ?? ''
    expect(
      overlayRule,
      '.silt-texture-surface::before rule must exist'
    ).toBeTruthy()
    expect(overlayRule, 'background-image must be declared').toMatch(
      /background-image:\s*var\(--silt-bg-editor-image/
    )
    expect(overlayRule, 'background-size must be declared').toMatch(
      /background-size:\s*var\(--silt-bg-editor-size/
    )
  })
})
