import {
  GetCustomDictionary,
  AddCustomDictionaryWord,
  RemoveCustomDictionaryWord,
  PickCustomDictionaryExportPath,
  PickCustomDictionaryImportFile,
  ExportCustomDictionary,
  ImportCustomDictionary
} from '../../../../bindings/silt/app.js'

/**
 * Reactive custom-spellcheck-dictionary store (#196, #338). Backs the
 * Settings → General "Custom dictionary" card (view/add/remove/import/export)
 * and the editor Add-to-dictionary action.
 */

let words = $state<string[]>([])
let filter = $state('')
let newWord = $state('')
let loading = $state(false)
let error = $state<string | null>(null)
let status = $state<string | null>(null)

export const customDictionary = {
  get words() {
    return words
  },
  get filter() {
    return filter
  },
  set filter(v: string) {
    filter = v
  },
  get newWord() {
    return newWord
  },
  set newWord(v: string) {
    newWord = v
  },
  get loading() {
    return loading
  },
  get error() {
    return error
  },
  get status() {
    return status
  },
  /** Filtered view for the management-card list. */
  get filtered() {
    const f = filter.trim().toLowerCase()
    if (!f) return words
    return words.filter((w) => w.toLowerCase().includes(f))
  },

  /** Load the resolved list from the backend. Called on card open. */
  async load(): Promise<void> {
    loading = true
    error = null
    try {
      words = await GetCustomDictionary()
    } catch (e) {
      error = String(e)
    } finally {
      loading = false
    }
  },

  /** Add the current newWord (or a passed-in word) via the IPC. */
  async add(word?: string): Promise<void> {
    const w = (word ?? newWord).trim()
    if (!w) return
    error = null
    status = null
    try {
      words = await AddCustomDictionaryWord(w)
      if (!word) newWord = ''
    } catch (e) {
      error = String(e)
    }
  },

  /** Remove a word via the IPC. */
  async remove(word: string): Promise<void> {
    error = null
    status = null
    try {
      words = await RemoveCustomDictionaryWord(word)
    } catch (e) {
      error = String(e)
    }
  },

  /** Export via native save dialog. */
  async exportFile(): Promise<void> {
    error = null
    status = null
    try {
      const path = await PickCustomDictionaryExportPath()
      if (!path) return
      await ExportCustomDictionary(path)
      status = 'Dictionary exported.'
    } catch (e) {
      error = String(e)
    }
  },

  /** Import via native open dialog; merges into vault dictionary. */
  async importFile(): Promise<void> {
    error = null
    status = null
    try {
      const path = await PickCustomDictionaryImportFile()
      if (!path) return
      const summary = await ImportCustomDictionary(path)
      words = await GetCustomDictionary()
      status = `Imported ${summary.added} words; ${summary.skipped} already present.`
    } catch (e) {
      error = String(e)
    }
  }
}
