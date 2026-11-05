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
//      webview can never see clipboard HTML by itself. It reports "no HTML" on
//      Windows/macOS (where the webview path above is the real source) and
//      errors on the opt-in GTK3 build; the reader treats a missing export or a
//      failed read as "no HTML".
//   3. `navigator.clipboard.readText()` — always available to the app and the
//      only path for text-only clipboards.
//
// Every step is best-effort: permission denials, missing APIs and failed blob
// reads all degrade to the next source, and the function never throws. Failures
// are reported through `unavailable`, so the caller (pasteFromClipboard) can
// toast a denied clipboard while staying silent for a readable-but-empty one.

import type { PastePayload } from './paste/pastePayload'

export interface ClipboardPayload extends PastePayload {
  /**
   * True only when every source failed (API missing, permission denied, blob
   * read error). A clipboard that was read successfully but holds nothing
   * stays `false` — that is an empty clipboard, not a broken one.
   */
  unavailable: boolean
}

/** Outcome of one source. `ok` = the read ran; `value` may still be empty. */
interface SourceRead {
  value: string
  ok: boolean
}

const FAILED: SourceRead = { value: '', ok: false }

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
 * Async Clipboard API HTML. `ok` is false when the API is missing, the read is
 * denied (the Linux case), or every item that advertised `text/html` failed to
 * yield a blob. A successful read with no `text/html` item is `ok` with ''.
 */
async function readHtmlFromNavigator(): Promise<SourceRead> {
  const clipboard = clipboardApi()
  if (!clipboard || typeof clipboard.read !== 'function') return FAILED

  let items: ClipboardItems
  try {
    items = await clipboard.read()
  } catch {
    return FAILED
  }

  let sawHtml = false
  for (const item of items) {
    if (!item.types?.includes('text/html')) continue
    sawHtml = true
    try {
      const blob = await item.getType('text/html')
      return { value: await blob.text(), ok: true }
    } catch {
      // Advertised the type but the read failed; keep looking at other items.
      continue
    }
  }
  return { value: '', ok: !sawHtml }
}

/**
 * HTML from the Go clipboard binding. Absent-safe by design: platforms and
 * builds without a native HTML source (GTK3, non-Linux) either omit the export
 * or report "no HTML", so an undefined export means "this build has no Go HTML
 * source", not an error.
 */
async function readHtmlFromBinding(): Promise<SourceRead> {
  try {
    const app = (await import('$silt-app')) as {
      ReadClipboardHTML?: () => Promise<string>
    }
    if (typeof app.ReadClipboardHTML !== 'function') return FAILED
    const html = await app.ReadClipboardHTML()
    return { value: typeof html === 'string' ? html : '', ok: true }
  } catch {
    return FAILED
  }
}

async function readTextFromNavigator(): Promise<SourceRead> {
  const clipboard = clipboardApi()
  if (!clipboard || typeof clipboard.readText !== 'function') return FAILED
  try {
    return { value: await clipboard.readText(), ok: true }
  } catch {
    return FAILED
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
  const fromNavigator = await readHtmlFromNavigator()
  const fromBinding = fromNavigator.value ? FAILED : await readHtmlFromBinding()
  const fromText = await readTextFromNavigator()

  return {
    html: normalizePayload(fromNavigator.value || fromBinding.value),
    text: normalizePayload(fromText.value),
    unavailable: !fromNavigator.ok && !fromBinding.ok && !fromText.ok
  }
}
