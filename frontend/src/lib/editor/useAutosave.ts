import type { Editor } from 'svelte-tiptap'
import { SaveFileBlocks } from '../../../wailsjs/go/main/App.js'
import { measureFrameBudget } from '../perf/frame-budget'
import { docToBlocks } from './converters'
import { pushNotification } from '../../notifications/store.svelte'
import { dispatch as dispatchPluginEvent } from '../../plugins/events'

export interface SaveState {
  dirty: boolean
  error: string | null
}

export interface AutosaveDeps {
  getEditor: () => Editor | null
  getNotebook: () => string
  getSection: () => string
  getPage: () => string
  getDelay: () => number
  onUpdate: (blocks: import('./types').ParsedBlock[]) => void
  onStateChange: (dirty: boolean, error: string | null) => void
  onSaveStateChange?: (state: SaveState) => void
}

/**
 * Manages debounced autosave for a TipTap editor page. The component creates
 * one instance per page and calls trigger() on every editor transaction.
 *
 * The component passes onStateChange to update its own $state variables
 * for template reactivity.
 *
 * Usage:
 *   const autosave = new AutosaveManager({ getEditor, notebook, section, page, ... })
 *   // on editor transaction:
 *   autosave.trigger()
 *   // on unmount:
 *   await autosave.flush()
 */
export class AutosaveManager {
  private timeout: ReturnType<typeof setTimeout> | null = null
  private lastEmitted: SaveState = { dirty: false, error: null }
  private deps: AutosaveDeps

  constructor(deps: AutosaveDeps) {
    this.deps = deps
  }

  /** Schedule a debounced save. Call on every editor transaction. */
  trigger(): void {
    this.markDirty()
    if (this.timeout) {
      clearTimeout(this.timeout)
      this.timeout = null
    }
    const delay = Math.max(this.deps.getDelay(), 50)
    this.timeout = setTimeout(() => {
      this.timeout = null
      void this.save()
    }, delay)
  }

  /** Mark the editor as dirty (e.g. on editor transaction). */
  markDirty(): void {
    this.deps.onStateChange(true, null)
    this.emitSaveState(true, null)
  }

  /** Flush any pending save immediately. Call on unmount or page change. */
  flush(): Promise<void> {
    if (this.timeout) {
      clearTimeout(this.timeout)
      this.timeout = null
      return this.save()
    }
    return Promise.resolve()
  }

  /** Mark the editor as clean (e.g. after loading new content). */
  markClean(): void {
    this.deps.onStateChange(false, null)
    this.emitSaveState(false, null)
  }

  private async save(): Promise<void> {
    const editor = this.deps.getEditor()
    if (!editor || editor.isDestroyed) return
    const updatedBlocks = measureFrameBudget('tiptap-transaction', () =>
      docToBlocks(editor.getJSON())
    )
    try {
      await SaveFileBlocks(
        this.deps.getNotebook(),
        this.deps.getSection(),
        this.deps.getPage(),
        updatedBlocks
      )
      this.deps.onStateChange(false, null)
      this.emitSaveState(false, null)
      dispatchPluginEvent('editor:save', {
        notebook: this.deps.getNotebook(),
        section: this.deps.getSection(),
        page: this.deps.getPage()
      })
    } catch (e) {
      const msg = e instanceof Error ? e.message : String(e)
      console.error('AutosaveManager: SaveFileBlocks failed:', e)
      this.deps.onStateChange(false, msg)
      this.emitSaveState(false, msg)
      pushNotification({
        kind: 'error',
        message: `Save failed: ${msg}`,
        action: { label: 'Retry', run: () => this.save() }
      })
    }
    this.deps.onUpdate(updatedBlocks)
  }

  private emitSaveState(dirty: boolean, error: string | null): void {
    const next: SaveState = { dirty, error }
    if (
      next.dirty !== this.lastEmitted.dirty ||
      next.error !== this.lastEmitted.error
    ) {
      this.lastEmitted = next
      this.deps.onSaveStateChange?.(next)
    }
  }
}
