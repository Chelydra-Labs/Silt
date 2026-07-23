import Typo from 'typo-js'
import {
  EnsureLanguagePack,
  GetLanguagePackContent,
  EnsureDomainPack,
  GetDomainPackWords
} from '../../../../bindings/silt/app.js'
import { dictionaryStatus, friendlyPackError } from './dictionaryStatus.svelte'

/**
 * typo-js wrapper + custom/domain dictionary layers (#196, #336, #337).
 *
 * Language packs: bundled en-US loads from /dictionaries/en-US/ via fetch;
 * other languages EnsureLanguagePack then GetLanguagePackContent (user-global
 * cache). Domain packs and custom words are Set layers — typo-js has no
 * public addWord.
 *
 * Note text never leaves the machine; only optional dictionary *assets* are
 * downloaded when the user selects a non-bundled pack.
 */

let dict: Typo | null = null
let loadPromise: Promise<Typo> | null = null
/** Language tag of the installed `dict` (empty when none). */
let loadedLang = ''
/** Language tag of the in-flight load (empty when idle). */
let inflightLang = ''
/** Monotonic generation so superseded loads never touch module state. */
let loadSeq = 0

/** Custom words (lowercased) from editor.custom_dictionary. */
const customWords = new Set<string>()

/** Domain pack words (lowercased) from enabled spellcheck_domains. */
const domainWords = new Set<string>()

/** Session-only ignores (the "Ignore" menu action). Cleared on reload. */
const sessionIgnores = new Set<string>()

/** Word → correctly-spelled cache so unchanged tokens skip Hunspell. */
const cache = new Map<string, boolean>()

/** @deprecated Prefer dictionaryStatus.loadError (reactive). */
export function getDictionaryLoadError(): string | null {
  return dictionaryStatus.loadError
}

/** Load (once per language) and return the Typo instance for `lang`. */
export function loadDictionary(lang: string): Promise<Typo> {
  // Already installed and idle — reuse.
  if (dict && loadedLang === lang && !inflightLang) {
    return Promise.resolve(dict)
  }
  // Same language already loading — join the in-flight promise.
  if (loadPromise && inflightLang === lang) {
    return loadPromise
  }
  // Installed language requested again while a *different* pack is loading:
  // cancel the switch and keep the good dict (caller wants the current one).
  if (dict && loadedLang === lang && inflightLang && inflightLang !== lang) {
    loadSeq++
    inflightLang = ''
    loadPromise = null
    return Promise.resolve(dict)
  }

  const seq = ++loadSeq
  const requestedLang = lang
  inflightLang = lang
  dictionaryStatus.setLoadError(null)
  loadPromise = (async () => {
    try {
      let aff: string
      let dic: string
      if (requestedLang === 'en-US') {
        const base = `/dictionaries/${requestedLang}`
        const [affRes, dicRes] = await Promise.all([
          fetch(`${base}/index.aff`),
          fetch(`${base}/index.dic`)
        ])
        if (!affRes.ok || !dicRes.ok) {
          throw new Error(
            `Could not load the built-in English dictionary (${affRes.status}/${dicRes.status}).`
          )
        }
        aff = await affRes.text()
        dic = await dicRes.text()
      } else {
        await EnsureLanguagePack(requestedLang)
        const content = await GetLanguagePackContent(requestedLang)
        aff = content.aff
        dic = content.dic
        if (!aff?.trim() || !dic?.trim()) {
          throw new Error(
            `Language pack "${requestedLang}" is empty. Download it again from Settings.`
          )
        }
      }
      // A newer loadDictionary call won the race — do not clobber its state.
      if (seq !== loadSeq) {
        return new Typo(requestedLang, aff, dic)
      }
      dict = new Typo(requestedLang, aff, dic)
      loadedLang = requestedLang
      inflightLang = ''
      loadPromise = null
      cache.clear()
      dictionaryStatus.setLoadError(null)
      return dict
    } catch (err) {
      // Only the active generation may report errors / clear in-flight markers.
      if (seq !== loadSeq) {
        throw err instanceof Error ? err : new Error(String(err))
      }
      loadPromise = null
      inflightLang = ''
      // Keep the last-good dict on a failed language switch so spellcheck does
      // not go silent; only wipe when there was nothing installed.
      if (!dict) {
        loadedLang = ''
      }
      const msg = friendlyPackError(err)
      dictionaryStatus.setLoadError(msg)
      // eslint-disable-next-line no-console
      console.warn(
        `[silt] spellcheck dictionary "${requestedLang}" failed to load:`,
        err
      )
      throw err instanceof Error ? err : new Error(msg)
    }
  })()
  return loadPromise
}

/** True once a dictionary has finished loading and is installed. */
export function isDictionaryLoaded(): boolean {
  return dict !== null && dict.loaded
}

/**
 * Language tag of the installed dictionary. Empty while nothing is loaded.
 * Does not report in-flight targets — those are not yet checking words.
 */
export function getActiveLanguage(): string {
  return loadedLang
}

/**
 * Reset the dictionary to the unloaded state (dict = null). Called when
 * spellcheck is toggled OFF so checkWord returns true for everything.
 */
export function resetDictionary(): void {
  loadSeq++ // invalidate any in-flight load
  dict = null
  loadPromise = null
  loadedLang = ''
  inflightLang = ''
  cache.clear()
  dictionaryStatus.clear()
}

/**
 * Replace the active custom-word set. Called when the config loads / changes
 * and when a word is added/removed via IPC.
 */
export function setCustomWords(words: string[]): void {
  customWords.clear()
  for (const w of words) {
    const lower = w.trim().toLowerCase()
    if (lower) customWords.add(lower)
  }
  cache.clear()
}

/**
 * Replace the active domain-word set (union of all enabled domain packs).
 */
export function setDomainWords(words: string[]): void {
  domainWords.clear()
  for (const w of words) {
    const lower = w.trim().toLowerCase()
    if (lower) domainWords.add(lower)
  }
  cache.clear()
}

/** True if word is in the active domain Set (for tests / diagnostics). */
export function hasDomainWord(word: string): boolean {
  return domainWords.has(word.trim().toLowerCase())
}

/**
 * Load all enabled domain packs and apply their word lists. Bundled packs
 * resolve without network; downloadable packs call EnsureDomainPack first.
 * Failures for individual packs are collected and rethrown after partial apply
 * so the UI can surface them (fail loudly).
 */
export async function loadDomainPacks(domainIds: string[]): Promise<void> {
  const ids = domainIds ?? []
  const all: string[] = []
  const errors: string[] = []
  for (const id of ids) {
    try {
      // Bundled packs (software-terms) are a no-op Ensure; all packs load via
      // the same IPC so there is one source of truth (//go:embed on the backend).
      await EnsureDomainPack(id)
      const words = await GetDomainPackWords(id)
      all.push(...(words ?? []))
    } catch (e) {
      errors.push(`${id}: ${e instanceof Error ? e.message : String(e)}`)
    }
  }
  setDomainWords(all)
  if (errors.length > 0) {
    const msg = friendlyPackError(
      new Error(`Could not load some word lists (${errors.join('; ')})`)
    )
    dictionaryStatus.setDomainError(msg)
    throw new Error(msg)
  }
  dictionaryStatus.setDomainError(null)
}

/** Parse a personal-dict / cspell-style word list (mirrors Go ParseWordList). */
export function parseWordListText(text: string): string[] {
  const seen = new Set<string>()
  const out: string[] = []
  for (const raw of text.split('\n')) {
    let line = raw.trim()
    if (!line || line.startsWith('#')) continue
    if (line.startsWith('cspell-tools:')) continue
    const hash = line.indexOf('#')
    if (hash >= 0) line = line.slice(0, hash).trim()
    const word = line.split(/\s+/)[0]?.toLowerCase()
    if (!word || seen.has(word)) continue
    seen.add(word)
    out.push(word)
  }
  return out
}

/** Whether `word` is known-correct (custom OR domain OR session OR Hunspell). */
export function checkWord(word: string): boolean {
  if (!dict) return true // not loaded yet — don't flag (avoids a false wave)
  const lower = word.toLowerCase()
  if (
    customWords.has(lower) ||
    domainWords.has(lower) ||
    sessionIgnores.has(lower)
  ) {
    return true
  }
  // Cache by the exact token: Hunspell is case-sensitive for proper nouns
  // (Rockford ✓, rockford ✗). Lowercasing before check was flagging place
  // names that are already in the dictionary under their title case.
  const cached = cache.get(word)
  if (cached !== undefined) return cached
  const result = dict.check(word)
  cache.set(word, result)
  return result
}

/** Ignore a word for the current session only (the "Ignore" menu action). */
export function ignoreWordSession(word: string): void {
  const lower = word.trim().toLowerCase()
  if (lower) {
    sessionIgnores.add(lower)
    // Exact-case cache entries for this token must drop too.
    for (const key of cache.keys()) {
      if (key.toLowerCase() === lower) cache.delete(key)
    }
  }
}

/** Top-N Hunspell suggestions for a misspelled word (empty if none). */
export function suggest(word: string, limit = 5): string[] {
  if (!dict) return []
  // Pass the token as written so casing fixes (rockford → Rockford) surface.
  // Drop only exact self-matches (no-op replace). Keep case variants — those
  // are real corrections for proper nouns stored in title case.
  const suggestions = dict.suggest(word).filter((s) => s !== word)
  return suggestions.slice(0, limit)
}
