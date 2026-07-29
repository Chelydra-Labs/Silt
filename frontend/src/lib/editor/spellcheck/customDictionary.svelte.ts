import {
  GetCustomDictionary,
  AddCustomDictionaryWord,
  RemoveCustomDictionaryWord,
  PickCustomDictionaryExportPath,
  PickCustomDictionaryImportFile,
  ExportCustomDictionary,
  ImportCustomDictionary
} from '../../../../bindings/silt/app.js'
import { mirrorCustomDictionary } from '../../../settings/store.svelte'
import { friendlyPackError } from './dictionaryStatus.svelte'

/**
 * Reactive custom-spellcheck-dictionary store (#196, #338). Backs the
 * Settings → General "Custom dictionary" card (view/add/remove/import/export)
 * and the editor Add-to-dictionary action.
 */

let words = $state<string[]>([])
let filter = $state('')
let newWord = $state('')
let loading = $state(false)
let busy = $state(false)
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
  get busy() {
    return busy
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
      error = friendlyPackError(e)
    } finally {
      loading = false
    }
  },

  /** Add the current newWord (or a passed-in word) via the IPC. */
  async add(word?: string): Promise<void> {
    const w = (word ?? newWord).trim()
    if (!w || busy) return
    error = null
    status = null
    busy = true
    try {
      words = await AddCustomDictionaryWord(w)
      // Go self-writes suppress config:changed, so mirror the resolved list
      // into the live config so the spellcheck $effect re-checks immediately.
      mirrorCustomDictionary(words)
      if (!word) newWord = ''
    } catch (e) {
      error = friendlyPackError(e)
    } finally {
      busy = false
    }
  },

  /** Remove a word via the IPC. */
  async remove(word: string): Promise<void> {
    if (busy) return
    error = null
    status = null
    busy = true
    try {
      words = await RemoveCustomDictionaryWord(word)
      mirrorCustomDictionary(words)
    } catch (e) {
      error = friendlyPackError(e)
    } finally {
      busy = false
    }
  },

  /** Export via native save dialog. */
  async exportFile(): Promise<void> {
    if (busy) return
    error = null
    status = null
    if (words.length === 0) {
      status = 'Nothing to export — your dictionary is empty.'
      return
    }
    busy = true
    try {
      const path = await PickCustomDictionaryExportPath()
      if (!path) return
      await ExportCustomDictionary(path)
      status = 'Dictionary exported.'
    } catch (e) {
      error = friendlyPackError(e)
    } finally {
      busy = false
    }
  },

  /** Import via native open dialog; merges into vault dictionary. */
  async importFile(): Promise<void> {
    if (busy) return
    error = null
    status = null
    busy = true
    try {
      const path = await PickCustomDictionaryImportFile()
      if (!path) return
      const summary = await ImportCustomDictionary(path)
      words = await GetCustomDictionary()
      mirrorCustomDictionary(words)
      if (summary.added === 0 && summary.skipped > 0) {
        status = `No new words — ${summary.skipped} were already in your dictionary.`
      } else if (summary.skipped > 0) {
        status = `Added ${summary.added} words (${summary.skipped} already present).`
      } else {
        status = `Added ${summary.added} words.`
      }
    } catch (e) {
      error = friendlyPackError(e)
    } finally {
      busy = false
    }
  }
}
