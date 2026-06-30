declare module 'typo-js' {
  /**
   * Minimal typing for typo-js (the unmaintained upstream ships no types and
   * there is no @types/typo-js). Covers only the surface Silt's spellcheck
   * wrapper uses: construct from preloaded .aff/.dic data, check, suggest.
   */
  export default class Typo {
    constructor(
      dictionary: string,
      affData?: string,
      wordsData?: string,
      settings?: Record<string, unknown>
    )
    check(word: string): boolean
    suggest(word: string): string[]
    readonly loaded: boolean
  }
}
