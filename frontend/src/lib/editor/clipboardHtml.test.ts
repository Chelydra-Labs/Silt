import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

// The Go binding does not exist until PR-5, so the reader must treat a missing
// export as "no HTML source". A getter lets each test switch between absent,
// resolving and throwing without re-mocking the module.
const bindingState = vi.hoisted(() => ({
  read: undefined as undefined | (() => Promise<string>)
}))

vi.mock('$silt-app', () => ({
  get ReadClipboardHTML() {
    return bindingState.read
  }
}))

import { PASTE_HTML_SIZE_CAP } from './paste/normalizePastedHTML'
import { readClipboardPayload } from './clipboardHtml'

function setClipboard(value: unknown): void {
  Object.defineProperty(navigator, 'clipboard', {
    value,
    configurable: true,
    writable: true
  })
}

/** Minimal ClipboardItem stand-in: `types` + `getType` is all the reader uses. */
function item(data: Record<string, string>): ClipboardItem {
  return {
    types: Object.keys(data),
    getType: async (type: string) => new Blob([data[type] ?? ''], { type })
  } as unknown as ClipboardItem
}

function readMock(items: ClipboardItem[]): ReturnType<typeof vi.fn> {
  return vi.fn().mockResolvedValue(items)
}

beforeEach(() => {
  bindingState.read = undefined
  setClipboard(undefined)
})

afterEach(() => {
  vi.clearAllMocks()
  setClipboard(undefined)
})

describe('readClipboardPayload', () => {
  it('prefers the Async Clipboard API HTML over the Go binding', async () => {
    const read = readMock([
      item({ 'text/plain': 'plain body', 'text/html': '<b>rich</b> body' })
    ])
    const readText = vi.fn().mockResolvedValue('plain body')
    setClipboard({ read, readText })
    bindingState.read = vi.fn().mockResolvedValue('<i>from binding</i>')

    await expect(readClipboardPayload()).resolves.toEqual({
      html: '<b>rich</b> body',
      text: 'plain body'
    })
    expect(readText).toHaveBeenCalledOnce()
    expect(bindingState.read).not.toHaveBeenCalled()
  })

  it('falls back to the Go binding when read() is denied (Linux)', async () => {
    const read = vi.fn().mockRejectedValue(new Error('NotAllowedError'))
    setClipboard({ read, readText: vi.fn().mockResolvedValue('plain') })
    bindingState.read = vi.fn().mockResolvedValue('<h2>From Go</h2>')

    await expect(readClipboardPayload()).resolves.toEqual({
      html: '<h2>From Go</h2>',
      text: 'plain'
    })
  })

  it('falls back to text when the binding export is absent (pre-PR-5)', async () => {
    // bindingState.read stays undefined: importing the module must not throw.
    setClipboard({
      read: vi.fn().mockResolvedValue([]),
      readText: vi.fn().mockResolvedValue('only text')
    })

    await expect(readClipboardPayload()).resolves.toEqual({
      html: '',
      text: 'only text'
    })
  })

  it('falls back to text when the binding throws', async () => {
    setClipboard({
      readText: vi.fn().mockResolvedValue('text after binding failure')
    })
    bindingState.read = vi.fn().mockRejectedValue(new Error('binding boom'))

    await expect(readClipboardPayload()).resolves.toEqual({
      html: '',
      text: 'text after binding failure'
    })
  })

  it('falls back to text when the Async Clipboard read API is missing', async () => {
    // Older webviews expose only readText/writeText.
    setClipboard({ readText: vi.fn().mockResolvedValue('legacy webview text') })

    await expect(readClipboardPayload()).resolves.toEqual({
      html: '',
      text: 'legacy webview text'
    })
  })

  it('ignores clipboard items that carry no text/html type', async () => {
    const read = readMock([
      item({ 'image/png': 'not-an-image-value' }),
      item({ 'text/plain': 'plain only' })
    ])
    const readText = vi.fn().mockResolvedValue('plain only')
    setClipboard({ read, readText })

    await expect(readClipboardPayload()).resolves.toEqual({
      html: '',
      text: 'plain only'
    })
  })

  it('keeps looking when a text/html item fails to produce a blob', async () => {
    const broken = {
      types: ['text/html'],
      getType: vi.fn().mockRejectedValue(new Error('blob read failed'))
    } as unknown as ClipboardItem
    setClipboard({
      read: readMock([broken]),
      readText: vi.fn().mockResolvedValue('')
    })
    bindingState.read = vi.fn().mockResolvedValue('<p>via binding</p>')

    await expect(readClipboardPayload()).resolves.toEqual({
      html: '<p>via binding</p>',
      text: ''
    })
  })

  it('returns empty strings when the clipboard API is unavailable', async () => {
    await expect(readClipboardPayload()).resolves.toEqual({
      html: '',
      text: ''
    })
  })

  it('never throws when every source denies access', async () => {
    setClipboard({
      read: vi.fn().mockRejectedValue(new Error('denied')),
      readText: vi.fn().mockRejectedValue(new Error('denied'))
    })
    bindingState.read = vi.fn().mockRejectedValue(new Error('denied'))

    await expect(readClipboardPayload()).resolves.toEqual({
      html: '',
      text: ''
    })
  })

  it('normalizes CRLF line endings and strips NUL padding', async () => {
    const read = readMock([
      item({ 'text/html': '<p>one</p>\r\n<p>two\u0000</p>' })
    ])
    setClipboard({
      read,
      readText: vi.fn().mockResolvedValue('one\r\ntwo\u0000\rthree')
    })

    await expect(readClipboardPayload()).resolves.toEqual({
      html: '<p>one</p>\n<p>two</p>',
      text: 'one\ntwo\nthree'
    })
  })

  it('still returns text for a payload larger than the paste cap', async () => {
    // The caller needs the text alternative when HTML is over the cap; the
    // reader must not skip the text read just because HTML came back.
    const huge = `<p>${'x'.repeat(PASTE_HTML_SIZE_CAP + 1)}</p>`
    setClipboard({
      read: readMock([item({ 'text/html': huge })]),
      readText: vi.fn().mockResolvedValue('fallback')
    })

    const payload = await readClipboardPayload()
    expect(payload.html.length).toBeGreaterThan(PASTE_HTML_SIZE_CAP)
    expect(payload.text).toBe('fallback')
  })
})
