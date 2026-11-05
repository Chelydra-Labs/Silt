// Clipboard payload reader for the context-menu Paste action.
//
// The native Ctrl+V path gets `text/html` straight off the ClipboardEvent, but
// a menu paste has no event to read: it has to ask the platform for the current
// clipboard contents. There are three sources, tried in order:
//
//   1. `navigator.clipboard.read()` — the Async Clipboard API. Works under
//      WebView2 (Windows), where Wails' permission handler allows it.
//   2. The Go `ReadClipboardHTML` binding — required on Linux, because Wails'
//      GTK permission handler hard-denies `clipboard.read()` there, so the
//      webview can never see clipboard HTML by itself. The binding lands in
//      PR-5; this reader imports it lazily and treats its absence as "no HTML"
//      so this seam can ship first.
//   3. `navigator.clipboard.readText()` — always available to the app and the
//      only path for text-only clipboards.
//
// Every step is best-effort: permission denials, missing APIs and failed blob
// reads all degrade to the next source, and the function never throws. The
// caller (pasteFromClipboard) owns the user-visible failure toast.

export interface ClipboardPayload {
  /** Clipboard HTML, or '' when no source could provide it. */
  html: string
  /** Clipboard plain text, or '' when unavailable. */
  text: string
}

/**
 * Platform clipboards hand back CRLF line endings and, occasionally, NUL
 * padding. Both confuse the HTML parser and the line splitter downstream
 * (siltClipboardTextParser counts `\n`), so normalize once at the seam.
 */
function normalizePayload(value: string): string {
  return value.replace(/\r\n?/g, '\n').replaceAll('\u0000', '')
}

function clipboardApi(): Clipboard | undefined {
  return typeof navigator === 'undefined' ? undefined : navigator.clipboard
}

/**
 * Async Clipboard API HTML. Returns '' when the API is missing, the clipboard
 * holds no `text/html` item, or the read is denied (the Linux case).
 */
async function readHtmlFromNavigator(): Promise<string> {
  const clipboard = clipboardApi()
  if (!clipboard || typeof clipboard.read !== 'function') return ''

  let items: ClipboardItems
  try {
    items = await clipboard.read()
  } catch {
    return ''
  }

  for (const item of items) {
    if (!item.types?.includes('text/html')) continue
    try {
      const blob = await item.getType('text/html')
      return await blob.text()
    } catch {
      // Advertised the type but the read failed; keep looking at other items.
      continue
    }
  }
  return ''
}

/**
 * HTML from the Go clipboard binding. Absent-safe by design: the binding does
 * not exist until PR-5, so an undefined export means "this platform has no Go
 * HTML source yet", not an error.
 */
async function readHtmlFromBinding(): Promise<string> {
  try {
    const app = (await import('$silt-app')) as {
      ReadClipboardHTML?: () => Promise<string>
    }
    if (typeof app.ReadClipboardHTML !== 'function') return ''
    const html = await app.ReadClipboardHTML()
    return typeof html === 'string' ? html : ''
  } catch {
    return ''
  }
}

async function readTextFromNavigator(): Promise<string> {
  const clipboard = clipboardApi()
  if (!clipboard || typeof clipboard.readText !== 'function') return ''
  try {
    return await clipboard.readText()
  } catch {
    return ''
  }
}

/**
 * Read both clipboard flavours for a menu paste.
 *
 * HTML is sourced from the first provider that yields it. Text is always
 * requested as well: when the HTML exceeds the paste size cap the caller must
 * fall back to text, and a text read that skipped because HTML succeeded would
 * leave that fallback with nothing to insert.
 */
export async function readClipboardPayload(): Promise<ClipboardPayload> {
  const html = (await readHtmlFromNavigator()) || (await readHtmlFromBinding())
  const text = await readTextFromNavigator()
  return { html: normalizePayload(html), text: normalizePayload(text) }
}
