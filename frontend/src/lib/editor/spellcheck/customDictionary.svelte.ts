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

/**
 * One-slot pending queue for the mutating actions (#822). A second action
 * called mid-flight is captured here and run when the in-flight one resolves,
 * instead of being silently dropped. A third call overwrites this slot
 * (last-write-wins) — the right semantics for a dictionary, where only the
 * user's most recent intent matters.
 */
let pendingAction: (() => Promise<void>) | null = null

/**
 * Shared executor for add/remove/import/export: owns the `busy` flag and
 * drains the pending slot from `finally`. Body errors flow through
 * friendlyPackError into `error` (never swallowed). On release, if a second
 * action was queued, it is re-invoked here — it re-enters the public method,
 * re-checks `busy` (now false), and runs to completion, so the user's intent
 * is preserved rather than dropped.
 */
async function runAction(body: () => Promise<void>): Promise<void> {
  busy = true
  try {
    await body()
  } catch (e) {
    error = friendlyPackError(e)
  } finally {
    busy = false
    const next = pendingAction
    pendingAction = null
    if (next) void next()
  }
}

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

  /**
   * Load the resolved list from the backend. Called on card open. Display-only:
   * deliberately does NOT mirror into the live config (unlike add/remove/
   * importFile), because an external edit to config.yaml emits config:changed,
   * which the hot-reload handler already uses to refresh settings.config for
   * the spellcheck effect. Busy-guarded (skipped, not queued) so a concurrent
   * reload can't clobber an in-flight mutation's result.
   */
  async load(): Promise<void> {
    // A mutation may be in flight; it already updates `words`, so skip a
    // concurrent reload that could clobber the just-applied result.
    if (busy) return
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
    if (!w) return
    if (busy) {
      // Capture the resolved word, not the raw arg: the no-arg path reads
      // newWord at call time, which may change before the queue drains.
      pendingAction = () => customDictionary.add(w)
      return
    }
    error = null
    status = null
    await runAction(async () => {
      words = await AddCustomDictionaryWord(w)
      // Go self-writes suppress config:changed, so mirror the resolved list
      // into the live config so the spellcheck $effect re-checks immediately.
      mirrorCustomDictionary(words)
      if (!word) newWord = ''
    })
  },

  /** Remove a word via the IPC. */
  async remove(word: string): Promise<void> {
    if (busy) {
      pendingAction = () => customDictionary.remove(word)
      return
    }
    error = null
    status = null
    await runAction(async () => {
      words = await RemoveCustomDictionaryWord(word)
      mirrorCustomDictionary(words)
    })
  },

  /** Export via native save dialog. */
  async exportFile(): Promise<void> {
    if (busy) {
      pendingAction = () => customDictionary.exportFile()
      return
    }
    error = null
    status = null
    if (words.length === 0) {
      status = 'Nothing to export — your dictionary is empty.'
      return
    }
    await runAction(async () => {
      const path = await PickCustomDictionaryExportPath()
      if (!path) return
      await ExportCustomDictionary(path)
      status = 'Dictionary exported.'
    })
  },

  /** Import via native open dialog; merges into vault dictionary. */
  async importFile(): Promise<void> {
    if (busy) {
      pendingAction = () => customDictionary.importFile()
      return
    }
    error = null
    status = null
    await runAction(async () => {
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
    })
  }
}

/** Test-only: reset all reactive state and the pending slot. */
export function _resetForTests(): void {
  words = []
  filter = ''
  newWord = ''
  loading = false
  busy = false
  error = null
  status = null
  pendingAction = null
}
