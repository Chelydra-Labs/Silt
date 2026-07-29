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
      // Resolve to the installed dict when present so superseded callers stay
      // consistent with getActiveLanguage() / checkWord(); only build an
      // orphan Typo when nothing is installed yet.
      if (seq !== loadSeq) {
        return dict ?? new Typo(requestedLang, aff, dic)
      }
      dict = new Typo(requestedLang, aff, dic)
      loadedLang = requestedLang
      inflightLang = ''
      loadPromise = null
      cache.clear()
      dictionaryStatus.setLoadError(null)
      return dict
    } catch (err) {
      // Superseded generation: do not surface errors (UI may have already
      // switched away). Hand back the installed dict when available.
      if (seq !== loadSeq) {
        if (dict) return dict
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
    // Exact-case cache entries for this token must drop too. Snapshot keys
    // first — mutating a Map while iterating its keys is fragile.
    for (const key of Array.from(cache.keys())) {
      if (key.toLowerCase() === lower) cache.delete(key)
    }
  }
}

// --- Diacritic restoration (#815) ----------------------------------------
// When a token is flagged misspelled, restore common Latin diacritics onto
// the typed ASCII token and validate each candidate via checkWord (which
// unions Hunspell + custom + domain). This surfaces accepted accented forms
// (café, naïve, Wärtsilä) for ASCII approximations, without ever inventing an
// accent the active word set does not accept. Ligatures (æ, œ, ß) and atomic
// letters (ø, ð, þ, ł, đ) are deliberately excluded — they are not
// single-diacritic restorations and would only coin non-words.
const ACCENT_VARIANTS: Record<string, string[]> = {
  a: ['à', 'á', 'â', 'ã', 'ä', 'å'],
  e: ['è', 'é', 'ê', 'ë'],
  i: ['ì', 'í', 'î', 'ï'],
  o: ['ò', 'ó', 'ô', 'õ', 'ö'],
  u: ['ù', 'ú', 'û', 'ü'],
  c: ['ç'],
  n: ['ñ'],
  y: ['ý', 'ÿ']
}

/** Accented variants of `ch`, matching its case so proper nouns keep their
 *  capitalization (Wartsila → Wärtsilä, Cafe → Café). checkWord lowercases for
 *  the custom/domain Set lookup, so case only affects the surfaced form. */
function accentVariants(ch: string): string[] {
  const variants = ACCENT_VARIANTS[ch.toLowerCase()]
  if (!variants) return []
  return ch === ch.toLowerCase()
    ? variants
    : variants.map((v) => v.toUpperCase())
}

function replaceChar(word: string, idx: number, ch: string): string {
  return word.slice(0, idx) + ch + word.slice(idx + 1)
}

/**
 * Validated accent-restored forms of `word`. Tier 1 tries a single diacritic
 * (covers café, naïve, façade, piñata); Tier 2 tries two (résumé, Wärtsilä,
 * Noël) only when Tier 1 finds nothing, and is skipped once the word has more
 * than five accent-eligible slots so the candidate set stays bounded. Every
 * candidate is checkWord-validated before it is offered.
 */
function diacriticSuggestions(word: string): string[] {
  const positions: number[] = []
  for (let i = 0; i < word.length; i++) {
    if (accentVariants(word[i]).length > 0) positions.push(i)
  }
  if (positions.length === 0) return []

  const validate = (candidates: string[]): string[] => {
    const hits: string[] = []
    const seen = new Set<string>()
    for (const c of candidates) {
      if (c === word || seen.has(c)) continue
      seen.add(c)
      if (checkWord(c)) hits.push(c)
    }
    return hits
  }

  // Tier 1: one accent per candidate.
  const tier1: string[] = []
  for (const i of positions) {
    for (const v of accentVariants(word[i])) tier1.push(replaceChar(word, i, v))
  }
  const tier1Hits = validate(tier1)
  if (tier1Hits.length > 0) return tier1Hits

  // Tier 2: two accents (bounded — skip long words to avoid combinatorial blowup).
  if (positions.length > 5) return []
  const tier2: string[] = []
  for (let p = 0; p < positions.length; p++) {
    for (let q = p + 1; q < positions.length; q++) {
      const ip = positions[p]
      const iq = positions[q]
      for (const vp of accentVariants(word[ip])) {
        for (const vq of accentVariants(word[iq])) {
          tier2.push(replaceChar(replaceChar(word, ip, vp), iq, vq))
        }
      }
    }
  }
  return validate(tier2)
}

/** Top-N suggestions for a misspelled word (empty if none). */
export function suggest(word: string, limit = 5): string[] {
  if (!dict) return []
  // Accent-restored forms rank above edit-distance noise: an exact ASCII-fold
  // match to an accepted accented word is the correction the user wants.
  // Hunspell's own suggestions follow (casing fixes like rockford → Rockford).
  // The token is passed as written; only exact self-matches are dropped so
  // case-variant corrections for proper nouns are still surfaced.
  const merged = [...diacriticSuggestions(word), ...dict.suggest(word)]
  const seen = new Set<string>([word])
  const out: string[] = []
  for (const s of merged) {
    if (seen.has(s)) continue
    seen.add(s)
    out.push(s)
    if (out.length >= limit) break
  }
  return out
}
