import { afterEach, describe, expect, it, vi } from 'vitest'
import { renderKatex } from './useKatex'

describe('useKatex with the installed KaTeX renderer', () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('renders a raw percent in a text argument', async () => {
    const result = await renderKatex(String.raw`\text{100% complete}`, false)

    expect(result.error).toBeNull()
    expect(result.html).toContain('katex')
    expect(result.html).toContain('100%')
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

  it.each([
    ['inline', false, 'katex'],
    ['block', true, 'katex-display']
  ])(
    'renders %s math through the application renderer',
    async (_, displayMode, expectedClass) => {
      const result = await renderKatex(String.raw`\frac{a}{b}`, displayMode)

      expect(result.error).toBeNull()
      expect(result.html).toContain(expectedClass)
      expect(result.html).toContain('application/x-tex')
    }
  )
})
