import type { Editor } from 'svelte-tiptap'
import {
  ResolveExternalConflict,
  SaveFileBlocks
} from '../../../bindings/silt/app.js'
import type { ParsedBlock as BindingParsedBlock } from '../../../bindings/silt/backend/parser/models.js'
import { IPCErrorCode } from '../../generated/enums'
import { coerceIPCError } from '../ipcError'
import { measureFrameBudget } from '../perf/frame-budget'
import { docToBlocks } from './converters'
import type { ExternalConflictResolution } from './externalConflict'
import { pushNotification } from '../../notifications/store.svelte'
import { dispatch as dispatchPluginEvent } from '../../plugins/events'

/**
 * The save lifecycle phase, distinct from the dirty flag (#546).
 *
 * - `idle`    — on disk matches the editor; nothing to announce.
 * - `pending` — dirty, the debounce timer is running (NOT "Saving…").
 * - `saving`  — a write is actually in flight (`SaveFileBlocks` awaited).
 * - `saved`   — the write succeeded; held briefly for a "Saved" confirmation.
 * - `error`   — the write failed (fail-loud).
 *
 * The critical fix: `pending` (debounce) must never be shown as "Saving…";
 * only `saving` (in-flight IPC) is.
 */
export type SavePhase = 'idle' | 'pending' | 'saving' | 'saved' | 'error'

export interface SaveState {
  phase: SavePhase
  dirty: boolean
  error: string | null
  /** Set when `error` is the external-edit refusal (#1005): the save-error
   *  surface should offer keep-mine/theirs/both actions, not a retry. */
  externalConflict?: boolean
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

/** How long the transient "Saved" confirmation stays visible before idle. */
const SAVED_HOLD_MS = 2000
/** Minimum time the "Saving…" phase stays visible so sub-ms local writes still
 *  register as honest in-flight feedback (#546 anti-flicker). */
const SAVING_MIN_DISPLAY_MS = 300

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
  private savedHoldTimeout: ReturnType<typeof setTimeout> | null = null
  private savingFloorTimeout: ReturnType<typeof setTimeout> | null = null
  private lastEmitted: SaveState = {
    phase: 'idle',
    dirty: false,
    error: null
  }
  private pendingSave: Promise<void> | null = null
  private saveQueued = false
  private savingStartedAt = 0
  /** Bumps on each in-flight save so a deferred "saved" emit is ignored if a
   *  newer save has started (min-display floor must not race). */
  private saveGeneration = 0
  /** When set, trigger/flush/save no-op so a post-restore keystroke cannot
   *  write the pre-reload buffer. Cleared after setContent + markClean. */
  private writesHeld = false
  private deps: AutosaveDeps

  constructor(deps: AutosaveDeps) {
    this.deps = deps
  }

  /** Schedule a debounced save. Call on every editor transaction. */
  trigger(): void {
    if (this.writesHeld) return
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
    this.emit('pending', true, null)
  }

  /** Flush any pending save immediately. Call on unmount or page change. */
  async flush(): Promise<void> {
    if (this.writesHeld) return
    if (this.timeout) {
      clearTimeout(this.timeout)
      this.timeout = null
      void this.save()
    }
    while (this.pendingSave) {
      await this.pendingSave
    }
    this.clearSavingFloor()
  }

  /** Tear down all timers. Call on component destroy to prevent stale phase
   *  emissions after the editor is gone. */
  dispose(): void {
    if (this.timeout) {
      clearTimeout(this.timeout)
      this.timeout = null
    }
    this.clearSavingFloor()
    this.clearSavedHold()
    this.clearHoldTimer()
  }

  /** Mark the editor as clean (e.g. after loading new content). */
  markClean(): void {
    this.deps.onStateChange(false, null)
    this.emit('idle', false, null)
  }

  /** Drop a queued debounce and invalidate any in-flight save so it cannot
   *  land as truth after an external reload (MCP/agent restore). */
  cancelPending(): void {
    if (this.timeout) {
      clearTimeout(this.timeout)
      this.timeout = null
    }
    this.saveQueued = false
    this.saveGeneration++
    this.clearSavingFloor()
    this.clearSavedHold()
  }

  private holdTimer: ReturnType<typeof setTimeout> | null = null

  /** Suppress new saves until releaseWrites (armed external reload). */
  holdWrites(timeoutMs = 8000): void {
    this.writesHeld = true
    this.cancelPending()
    this.clearHoldTimer()
    this.holdTimer = setTimeout(() => {
      this.holdTimer = null
      this.releaseWrites()
    }, timeoutMs)
  }

  releaseWrites(): void {
    this.clearHoldTimer()
    this.writesHeld = false
  }

  areWritesHeld(): boolean {
    return this.writesHeld
  }

  private clearHoldTimer(): void {
    if (this.holdTimer) {
      clearTimeout(this.holdTimer)
      this.holdTimer = null
    }
  }

  private async save(): Promise<void> {
    if (this.writesHeld) return
    if (this.pendingSave) {
      this.saveQueued = true
      return
    }
    const editor = this.deps.getEditor()
    if (!editor || editor.isDestroyed) return
    const updatedBlocks = measureFrameBudget('tiptap-transaction', () =>
      docToBlocks(editor.getJSON())
    )
    // The write is now genuinely in flight: surface the honest 'saving' phase,
    // distinct from the 'pending' debounce above (#546).
    this.savingStartedAt = Date.now()
    const saveGen = ++this.saveGeneration
    this.emit('saving', true, null)
    this.pendingSave = (async () => {
      try {
        await SaveFileBlocks(
          this.deps.getNotebook(),
          this.deps.getSection(),
          this.deps.getPage(),
          // The editor's ParsedBlock is a slim view of the wire type; the
          // extra fields (pinned, progress, created_at, …) are omitempty in
          // Go and the Wails v3 generator marks them required, so bridge via
          // unknown at this IPC boundary.
          updatedBlocks as unknown as BindingParsedBlock[]
        )
        if (this.saveGeneration !== saveGen) return
        this.deps.onStateChange(false, null)
        // Min-display floor is non-blocking: flush/pendingSave complete after
        // IPC so unmount is not delayed; UI still holds "Saving…" briefly.
        this.scheduleSavedAfterFloor(saveGen)
        dispatchPluginEvent('editor:save', {
          notebook: this.deps.getNotebook(),
          section: this.deps.getSection(),
          page: this.deps.getPage()
        })
      } catch (e) {
        if (this.saveGeneration !== saveGen) return
        const coerced = coerceIPCError(e)
        if (coerced.code === IPCErrorCode.CodePageWriteStale) {
          this.deps.onStateChange(false, null)
          this.emit('idle', false, null)
          return
        }
        if (coerced.code === IPCErrorCode.CodeExternalEditConflict) {
          // Refused, not failed: the on-disk version must survive until the
          // user picks a resolution. Stay in the error phase (the surface
          // shows the three actions) and skip the Retry toast — a retry would
          // just re-refuse (#1005).
          console.warn('AutosaveManager: external edit conflict:', e)
          this.deps.onStateChange(true, coerced.message)
          this.emit('error', true, coerced.message, true)
          return
        }
        const msg = e instanceof Error ? e.message : String(e)
        console.error('AutosaveManager: SaveFileBlocks failed:', e)
        // Errors skip the min-display floor — fail-loud immediately.
        this.deps.onStateChange(true, msg)
        this.emit('error', true, msg)
        pushNotification({
          kind: 'error',
          message: `Save failed: ${msg}`,
          action: { label: 'Retry', run: () => this.save() }
        })
      }
      if (this.saveGeneration !== saveGen) return
      // onUpdate fires on both success and failure paths: the parent needs
      // current blocks for rendering regardless of persistence status. The
      // dirty flag tracks save state; a failed save leaves dirty=true so
      // the next trigger re-attempts and re-converges.
      this.deps.onUpdate(updatedBlocks)
    })()
    try {
      await this.pendingSave
    } finally {
      this.pendingSave = null
      if (this.saveQueued) {
        this.saveQueued = false
        void this.save()
      }
    }
  }

  /** After a successful write, keep phase at 'saving' until SAVING_MIN_DISPLAY_MS
   *  has elapsed, then emit 'saved'. Generation-guarded so a newer save wins. */
  private scheduleSavedAfterFloor(saveGen: number): void {
    const elapsed = Date.now() - this.savingStartedAt
    const remain = Math.max(0, SAVING_MIN_DISPLAY_MS - elapsed)
    const finish = () => {
      if (this.saveGeneration !== saveGen) return
      if (this.lastEmitted.phase !== 'saving') return
      this.emit('saved', false, null)
      this.armSavedHold()
    }
    if (remain === 0) {
      finish()
      return
    }
    this.savingFloorTimeout = setTimeout(() => {
      this.savingFloorTimeout = null
      finish()
    }, remain)
  }

  private clearSavingFloor(): void {
    if (this.savingFloorTimeout) {
      clearTimeout(this.savingFloorTimeout)
      this.savingFloorTimeout = null
    }
  }

  /** Hold the "Saved" confirmation for a beat, then revert to idle if idle. */
  private armSavedHold(): void {
    this.clearSavedHold()
    this.savedHoldTimeout = setTimeout(() => {
      this.savedHoldTimeout = null
      // Only revert if no new edit superseded the saved state.
      if (this.lastEmitted.phase === 'saved') {
        this.emit('idle', false, null)
      }
    }, SAVED_HOLD_MS)
  }

  private clearSavedHold(): void {
    if (this.savedHoldTimeout) {
      clearTimeout(this.savedHoldTimeout)
      this.savedHoldTimeout = null
    }
  }

  private emit(
    phase: SavePhase,
    dirty: boolean,
    error: string | null,
    externalConflict = false
  ): void {
    // A new non-saved phase supersedes any pending saved→idle revert.
    if (phase !== 'saved') this.clearSavedHold()
    // externalConflict is omitted when false so non-conflict states keep
    // their exact historical shape (tests + tab-state consumers).
    const next: SaveState = { phase, dirty, error }
    if (externalConflict) next.externalConflict = true
    if (
      next.phase !== this.lastEmitted.phase ||
      next.dirty !== this.lastEmitted.dirty ||
      next.error !== this.lastEmitted.error ||
      next.externalConflict !== this.lastEmitted.externalConflict
    ) {
      this.lastEmitted = next
      this.deps.onSaveStateChange?.(next)
    }
  }

  /**
   * Resolve an external-edit refusal (#1005). The backend preserves the
   * losing version as a `.conflict-*` sibling and settles the winner.
   *
   * keep_mine / keep_both: the backend replays the refused save, so the
   * buffer matches disk again → markClean. keep_theirs: the buffer is
   * discarded in favor of the on-disk version — writes are held and the
   * caller's armed reload (fresh FetchPageBlocks) replaces the content;
   * markClean clears the refusal surface in the meantime.
   */
  async resolveExternalConflict(
    resolution: ExternalConflictResolution
  ): Promise<void> {
    if (resolution === 'keep_theirs') {
      // Held before the IPC: the backend's block:changed triggers a reload
      // that must not race a stale debounce firing mid-resolution.
      this.holdWrites()
    }
    try {
      await ResolveExternalConflict(
        this.deps.getNotebook(),
        this.deps.getSection(),
        this.deps.getPage(),
        resolution
      )
    } catch (e) {
      if (resolution === 'keep_theirs') this.releaseWrites()
      throw e
    }
    this.markClean()
  }
}
