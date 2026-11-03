import { afterEach, describe, expect, it, vi } from 'vitest'
import { renderKatex } from './useKatex'

describe('useKatex with the installed KaTeX renderer', () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('renders an escaped percent in a text argument as valid KaTeX', async () => {
    const result = await renderKatex(String.raw`\text{100\% complete}`, false)

    expect(result.error).toBeNull()
    const output = document.createElement('div')
    output.innerHTML = result.html
    expect(output.querySelector('.katex')).not.toBeNull()
    expect(output.querySelector('.katex-error')).toBeNull()
    expect(output.textContent?.replace(/\u00a0/g, ' ')).toContain(
      '100% complete'
    )
  })

  it('renders an escaped percent in block mode as valid KaTeX', async () => {
    const result = await renderKatex(String.raw`\text{100\% complete}`, true)

    expect(result.error).toBeNull()
    const output = document.createElement('div')
    output.innerHTML = result.html
    expect(output.querySelector('.katex-display')).not.toBeNull()
    expect(output.querySelector('.katex-error')).toBeNull()
    expect(output.textContent?.replace(/\u00a0/g, ' ')).toContain(
      '100% complete'
    )
  })

  it('preserves KaTeX error markup for a raw percent that comments out the closing brace', async () => {
    const result = await renderKatex(String.raw`\text{100% complete}`, false)

    expect(result.error).toBeNull()
    const output = document.createElement('div')
    output.innerHTML = result.html
    expect(output.querySelector('.katex-error')).not.toBeNull()
    expect(output.querySelector('.katex .mtext')).toBeNull()
  })

  it('warns about an unavailable glyph metric without throwing', async () => {
    const warning = vi.spyOn(console, 'warn').mockImplementation(() => {})

    const result = await renderKatex('🧌', false)

    expect(result.error).toBeNull()
    expect(result.html).toContain('katex')
    expect(warning).toHaveBeenCalledWith(
      expect.stringContaining('No character metrics')
    )
  })

  it('warns about an unavailable glyph metric in block mode without throwing', async () => {
    const warning = vi.spyOn(console, 'warn').mockImplementation(() => {})

    const result = await renderKatex('🧌', true)

    expect(result.error).toBeNull()
    const output = document.createElement('div')
    output.innerHTML = result.html
    expect(output.querySelector('.katex-display')).not.toBeNull()
    expect(output.querySelector('.katex-error')).toBeNull()
    expect(warning).toHaveBeenCalledWith(
      expect.stringContaining('No character metrics')
    )
  })

  it.each([
    ['inline', false, 'katex'],
    ['block', true, 'katex-display']
  ])(
    'renders %s math through the application renderer',
    async (_, displayMode, expectedClass) => {
      const result = await renderKatex(String.raw`\frac{a}{b}`, displayMode)

      expect(result.error).toBeNull()
      const output = document.createElement('div')
      output.innerHTML = result.html
      expect(output.querySelector(`.${expectedClass}`)).not.toBeNull()
      expect(output.querySelector('.katex-error')).toBeNull()
      expect(
        output.querySelector('annotation[encoding="application/x-tex"]')
      ).not.toBeNull()
    }
  )
})
