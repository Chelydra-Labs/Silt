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
let currentLang = ''

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
  if (dict && currentLang === lang) return Promise.resolve(dict)
  if (loadPromise && currentLang === lang) return loadPromise
  currentLang = lang
  dictionaryStatus.setLoadError(null)
  loadPromise = (async () => {
    try {
      let aff: string
      let dic: string
      if (lang === 'en-US') {
        const base = `/dictionaries/${lang}`
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
        await EnsureLanguagePack(lang)
        const content = await GetLanguagePackContent(lang)
        aff = content.aff
        dic = content.dic
        if (!aff?.trim() || !dic?.trim()) {
          throw new Error(
            `Language pack "${lang}" is empty. Download it again from Settings.`
          )
        }
      }
      dict = new Typo(lang, aff, dic)
      cache.clear()
      dictionaryStatus.setLoadError(null)
      return dict
    } catch (err) {
      loadPromise = null
      currentLang = ''
      dict = null
      const msg = friendlyPackError(err)
      dictionaryStatus.setLoadError(msg)
      // eslint-disable-next-line no-console
      console.warn(
        `[silt] spellcheck dictionary "${lang}" failed to load:`,
        err
      )
      throw err instanceof Error ? err : new Error(msg)
    }
  })()
  return loadPromise
}

/** True once the dictionary for the current language has finished loading. */
export function isDictionaryLoaded(): boolean {
  return dict !== null && dict.loaded
}

/**
 * Reset the dictionary to the unloaded state (dict = null). Called when
 * spellcheck is toggled OFF so checkWord returns true for everything.
 */
export function resetDictionary(): void {
  dict = null
  loadPromise = null
  currentLang = ''
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
      if (id === 'software-terms') {
        const res = await fetch('/dictionaries/supplements/software-terms.txt')
        if (!res.ok) {
          throw new Error(`HTTP ${res.status}`)
        }
        const text = await res.text()
        all.push(...parseWordListText(text))
      } else {
        await EnsureDomainPack(id)
        const words = await GetDomainPackWords(id)
        all.push(...(words ?? []))
      }
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
  const cached = cache.get(lower)
  if (cached !== undefined) return cached
  const result = dict.check(lower)
  cache.set(lower, result)
  return result
}

/** Ignore a word for the current session only (the "Ignore" menu action). */
export function ignoreWordSession(word: string): void {
  const lower = word.trim().toLowerCase()
  if (lower) {
    sessionIgnores.add(lower)
    cache.delete(lower)
  }
}

/** Top-N Hunspell suggestions for a misspelled word (empty if none). */
export function suggest(word: string, limit = 5): string[] {
  if (!dict) return []
  const suggestions = dict.suggest(word.toLowerCase())
  return suggestions.slice(0, limit)
}
