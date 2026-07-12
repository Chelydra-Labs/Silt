/**
 * Reactive spellcheck load status for Settings + editor chrome.
 * dictionary.ts writes here; Svelte components read loadError reactively.
 */

let loadError = $state<string | null>(null)
let domainError = $state<string | null>(null)

export const dictionaryStatus = {
  get loadError() {
    return loadError
  },
  get domainError() {
    return domainError
  },
  setLoadError(msg: string | null) {
    loadError = msg
  },
  setDomainError(msg: string | null) {
    domainError = msg
  },
  clear() {
    loadError = null
    domainError = null
  }
}

/** Map raw IPC/network errors to short user-facing sentences. */
export function friendlyPackError(err: unknown): string {
  const raw = err instanceof Error ? err.message : String(err)
  const lower = raw.toLowerCase()
  if (
    lower.includes('network') ||
    lower.includes('timeout') ||
    lower.includes('connection') ||
    lower.includes('fetch failed') ||
    lower.includes('eof')
  ) {
    return 'Download failed — check your network and try again.'
  }
  if (lower.includes('http 4') || lower.includes('http 5')) {
    return 'Dictionary server returned an error. Try again later.'
  }
  if (lower.includes('exceeds') || lower.includes('limit')) {
    return 'File is too large to import or download.'
  }
  if (lower.includes('vault not loaded')) {
    return 'Open a vault before changing spellcheck dictionaries.'
  }
  if (lower.includes('unknown language') || lower.includes('unknown domain')) {
    return 'That dictionary is not available in this version of Silt.'
  }
  if (lower.includes('not installed') || lower.includes('empty')) {
    return 'Dictionary is not ready. Download it again or pick another language.'
  }
  // Keep short; full detail stays in console.
  if (raw.length > 160) return raw.slice(0, 157) + '…'
  return raw || 'Something went wrong with spellcheck dictionaries.'
}
