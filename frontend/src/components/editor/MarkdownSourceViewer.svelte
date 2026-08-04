<script lang="ts">
  import { onDestroy, onMount } from 'svelte'
  import type { ParsedBlock } from '../../lib/editor/types'
  import { themeState } from '../../theme/store.svelte'
  import {
    highlightMarkdown,
    tokensToShikiTheme
  } from '../../lib/editor/useMarkdownHighlighter'
  import {
    fetchPageMarkdown,
    savePageMarkdown
  } from '../../lib/editor/pageMarkdown'
  import {
    createSourceHistory,
    type SourceHistory,
    type SourceHistoryEntry,
    type SourceHistorySelection
  } from '../../lib/editor/sourceHistory'
  import {
    AcquireFocusLock,
    ReleaseFocusLock,
    RefreshFocusLock
  } from '../../../bindings/silt/app.js'

  // MarkdownSourceViewer — editable Source mode (#660) with optional read-only
  // Shiki highlight (#171/#194). Seeds from on-disk body via FetchPageMarkdown;
  // reconstructMarkdown is fallback only. Debounced save via SavePageMarkdown.
  //
  // Editing history (#861): a bounded, local stack — see sourceHistory.ts.
  // Native textarea undo is unobservable and gets blown away by Svelte's
  // `.value=` rerender, so every user edit (typing, paste, selection
  // replacement, Tab/Shift-Tab) is recorded here; seed/reload/external
  // replacement are explicit boundaries.

  interface Props {
    blocks: ParsedBlock[]
    filePath: string
    notebook?: string
    section?: string
    page?: string
    onBlocksSaved?: (blocks: ParsedBlock[]) => void
    /** When true (default), body is an editable textarea with debounced save. */
    editable?: boolean
  }

  let {
    blocks,
    filePath,
    notebook = '',
    section = '',
    page = '',
    onBlocksSaved,
    editable = true
  }: Props = $props()

  function reconstructMarkdown(source: ParsedBlock[]): string {
    const lines: string[] = []
    for (const block of source) {
      const indent = '  '.repeat(block.depth || 0)
      const line = block.raw_text || block.clean_text || ''
      lines.push(indent + line)
    }
    return lines.join('\n')
  }

  let buffer = $state('')
  let dirty = $state(false)
  let saveError = $state<string | null>(null)
  let conflictPending = $state(false)
  let savePhase = $state<'idle' | 'saving' | 'saved' | 'error'>('idle')
  let saveTimer: ReturnType<typeof setTimeout> | null = null
  let savedClearTimer: ReturnType<typeof setTimeout> | null = null
  let heartbeatInterval: ReturnType<typeof setInterval> | null = null
  let hasFocusLock = false
  /** Fingerprint of last applied external blocks (ids + raw_text). */
  let lastBlocksKey = $state<string | null>(null)
  /** Fingerprint of blocks we just saved — skip false conflict on self-refresh. */
  let lastSelfSavedKey: string | null = null
  let seedSeq = 0

  // --- Local editing history (#861) --------------------------------------
  // One history per mounted viewer. Tab/Shift-Tab, paste, selection
  // replacement and character typing all push entries; undo/redo just walk
  // the pointer. Boundaries (seed/reload/external-while-clean) call reset().
  const history: SourceHistory = createSourceHistory()
  // history internals are plain JS state, invisible to Svelte's reactivity.
  // Bump this counter on every mutation so $derived(canUndo/canRedo) refresh.
  let historyVersion = $state(0)
  function bumpHistory(): void {
    historyVersion++
  }
  /** Live region announcement for undo/redo feedback (a11y). */
  let historyStatus = $state('')
  let historyStatusTimer: ReturnType<typeof setTimeout> | null = null
  /** Captured pre-edit selection + inputType, set in beforeinput. */
  let pendingInputType = ''
  let pendingPreSelection: SourceHistorySelection | null = null

  let textareaEl = $state<HTMLTextAreaElement | null>(null)
  let lineCount = $derived(Math.max(1, buffer.split('\n').length))
  let canUndo = $derived.by(() => {
    void historyVersion
    return history.canUndo()
  })
  let canRedo = $derived.by(() => {
    void historyVersion
    return history.canRedo()
  })

  function blocksKey(source: ParsedBlock[]): string {
    return source
      .map((b) => `${b.id}\0${b.raw_text ?? b.clean_text ?? ''}`)
      .join('\n')
  }

  function readSelection(
    el: HTMLTextAreaElement | null
  ): SourceHistorySelection {
    if (!el) return { start: 0, end: 0, direction: 'forward' }
    return {
      start: el.selectionStart ?? 0,
      end: el.selectionEnd ?? 0,
      // jsdom returns null sometimes; normalise to a valid direction.
      direction: el.selectionDirection ?? 'forward'
    }
  }

  /** Boundary: seed/reload/external replacement starts a fresh history. */
  function seedBuffer(value: string): void {
    buffer = value
    dirty = false
    saveError = null
    savePhase = 'idle'
    history.reset({
      value,
      selection: { start: 0, end: 0, direction: 'forward' }
    })
    bumpHistory()
    // Drop any in-flight save so we don't write a stale buffer back.
    if (saveTimer) {
      clearTimeout(saveTimer)
      saveTimer = null
    }
    queueMicrotask(() => {
      if (!textareaEl) return
      textareaEl.selectionStart = 0
      textareaEl.selectionEnd = 0
      textareaEl.selectionDirection = 'forward'
    })
  }

  async function seedFromDiskOrBlocks(source: ParsedBlock[]): Promise<void> {
    const seq = ++seedSeq
    const canFetch = Boolean(notebook && page)
    if (canFetch) {
      try {
        const body = await fetchPageMarkdown(notebook, section, page)
        if (seq !== seedSeq) return
        if (body.trim() !== '') {
          seedBuffer(body)
          return
        }
      } catch (e) {
        console.error('MarkdownSourceViewer: fetchPageMarkdown failed:', e)
      }
    }
    if (seq !== seedSeq) return
    if (source.length > 0) {
      seedBuffer(reconstructMarkdown(source))
    } else {
      seedBuffer('')
    }
  }

  // External blocks prop: seed / reset when clean; conflict chooser when dirty.
  $effect(() => {
    const next = blocks
    const key = blocksKey(next)
    if (lastBlocksKey === null) {
      lastBlocksKey = key
      void seedFromDiskOrBlocks(next)
      return
    }
    if (key === lastBlocksKey) return
    if (!dirty) {
      lastBlocksKey = key
      conflictPending = false
      saveError = null
      // External replacement while clean: a hard history boundary. The
      // user wasn't editing, so the incoming buffer is the new ground truth.
      void seedFromDiskOrBlocks(next)
    } else if (lastSelfSavedKey && key === lastSelfSavedKey) {
      // Parent re-applied our own onBlocksSaved payload while the user kept
      // typing — not an external conflict.
      lastSelfSavedKey = null
      lastBlocksKey = key
    } else {
      // Keep lastBlocksKey so we only surface once per external key change.
      lastBlocksKey = key
      conflictPending = true
      if (saveTimer) {
        clearTimeout(saveTimer)
        saveTimer = null
      }
    }
  })

  // Resolve the effective (mode-resolved) token map + concrete dark/light
  // mode from the theme store. "system" follows prefers-color-scheme.
  let systemLight = $state(
    typeof window !== 'undefined' &&
      !!window.matchMedia?.('(prefers-color-scheme: light)').matches
  )
  $effect(() => {
    if (typeof window === 'undefined' || !window.matchMedia) return
    const mql = window.matchMedia('(prefers-color-scheme: light)')
    const onChange = (): void => {
      systemLight = mql.matches
    }
    mql.addEventListener('change', onChange)
    return () => mql.removeEventListener('change', onChange)
  })
  let effectiveMode = $derived<'dark' | 'light'>(
    themeState.mode === 'light'
      ? 'light'
      : themeState.mode === 'dark'
        ? 'dark'
        : systemLight
          ? 'light'
          : 'dark'
  )
  let tokens = $derived(
    effectiveMode === 'light' ? themeState.lightTokens : themeState.darkTokens
  )

  // Shiki only in read-only mode (editable uses plain textarea).
  let highlightedHtml = $state<string | null>(null)
  let highlightSeq = 0
  $effect(() => {
    if (editable) {
      highlightedHtml = null
      return
    }
    const md = buffer
    const t = tokens
    const mode = effectiveMode
    const seq = ++highlightSeq
    void (async () => {
      let html: string | null
      try {
        html = await highlightMarkdown(md, tokensToShikiTheme(t, mode))
      } catch {
        html = null
      }
      if (seq === highlightSeq) highlightedHtml = html
    })()
  })

  let copyStatus = $state<{ kind: 'ok' | 'err'; msg: string } | null>(null)
  let copyStatusTimer: ReturnType<typeof setTimeout> | null = null
  function setCopyStatus(kind: 'ok' | 'err', msg: string): void {
    copyStatus = { kind, msg }
    if (copyStatusTimer) clearTimeout(copyStatusTimer)
    copyStatusTimer = setTimeout(() => {
      copyStatus = null
      copyStatusTimer = null
    }, 2500)
  }

  async function copyAsMarkdown(): Promise<void> {
    try {
      await navigator.clipboard.writeText(buffer)
      setCopyStatus('ok', 'Copied markdown to clipboard.')
    } catch {
      setCopyStatus('err', 'Failed to copy — clipboard unavailable.')
    }
  }

  function scheduleSave(): void {
    if (!editable || !notebook || !page || conflictPending) return
    if (saveTimer) clearTimeout(saveTimer)
    saveTimer = setTimeout(() => {
      saveTimer = null
      void flushSave()
    }, 500)
  }

  async function flushSave(): Promise<void> {
    if (!editable || !notebook || !page || !dirty || conflictPending) return
    const md = buffer
    savePhase = 'saving'
    try {
      const saved = await savePageMarkdown(notebook, section, page, md)
      // Stamp before parent refresh so the blocks $effect can ignore self-saves
      // when the user typed during the IPC round-trip.
      lastSelfSavedKey = blocksKey(saved)
      // Only clear dirty if buffer still matches what we saved.
      if (buffer === md) {
        dirty = false
        savePhase = 'saved'
        if (savedClearTimer) clearTimeout(savedClearTimer)
        savedClearTimer = setTimeout(() => {
          if (savePhase === 'saved') savePhase = 'idle'
          savedClearTimer = null
        }, 2000)
      } else {
        savePhase = 'idle'
      }
      saveError = null
      onBlocksSaved?.(saved)
    } catch (e) {
      saveError = e instanceof Error ? e.message : String(e)
      savePhase = 'error'
    }
  }

  function keepMine(): void {
    conflictPending = false
    // User intentionally overwrites remote; allow save of current buffer.
    // History is preserved — Keep mine is the user choosing their branch,
    // not a content replacement.
    if (dirty) scheduleSave()
  }

  function reloadFromDisk(): void {
    conflictPending = false
    saveError = null
    savePhase = 'idle'
    if (saveTimer) {
      clearTimeout(saveTimer)
      saveTimer = null
    }
    // Reload is an explicit history boundary: the user is discarding their
    // edits entirely, so the redo branch must not resurrect discarded text.
    void seedFromDiskOrBlocks(blocks)
  }

  function announceHistory(msg: string): void {
    // Force re-announcement across identical messages (rapid undo/redo) by
    // clearing synchronously then setting on the next microtask. Svelte
    // batches state writes within a single tick, so a same-tick clear+set
    // would coalesce and AT would observe no change. Splitting across a
    // microtask produces two DOM mutations and the second one re-announces.
    historyStatus = ''
    if (historyStatusTimer) {
      clearTimeout(historyStatusTimer)
      historyStatusTimer = null
    }
    queueMicrotask(() => {
      historyStatus = msg
      historyStatusTimer = setTimeout(() => {
        historyStatus = ''
        historyStatusTimer = null
      }, 1500)
    })
  }

  /** Human-readable description of the value delta between two history
   *  entries. Used so the live region says something meaningful ("Undid:
   *  removed 5 characters") instead of a generic "Undid edit". */
  function describeChange(
    prev: SourceHistoryEntry | null,
    next: SourceHistoryEntry
  ): string {
    if (!prev) return 'edit applied'
    const delta = next.value.length - prev.value.length
    if (delta === 0) return 'replaced content'
    const magnitude = Math.abs(delta)
    const unit = magnitude === 1 ? 'character' : 'characters'
    return delta > 0
      ? `added ${magnitude} ${unit}`
      : `removed ${magnitude} ${unit}`
  }

  function markDirtyAndSchedule(): void {
    dirty = true
    saveError = null
    if (savePhase === 'saved' || savePhase === 'error') savePhase = 'idle'
    scheduleSave()
  }

  /**
   * Apply a value+selection change originating from the user (typing,
   * Tab/Shift-Tab, paste, etc.) — buffer, DOM selection, history, and save
   * debounce all flow through here so the invariants stay aligned.
   */
  function commitUserEdit(
    nextValue: string,
    selection: SourceHistorySelection,
    opts: { coalesce?: boolean } = {}
  ): void {
    buffer = nextValue
    markDirtyAndSchedule()
    history.push({ value: nextValue, selection }, opts)
    bumpHistory()
    queueMicrotask(() => restoreSelection(selection))
  }

  function restoreSelection(selection: SourceHistorySelection): void {
    if (!textareaEl) return
    const len = textareaEl.value.length
    const start = Math.min(selection.start, len)
    const end = Math.min(selection.end, len)
    textareaEl.selectionStart = start
    textareaEl.selectionEnd = end
    textareaEl.selectionDirection = selection.direction
  }

  function onBeforeInput(e: Event): void {
    const ie = e as InputEvent
    const inputType = typeof ie.inputType === 'string' ? ie.inputType : ''
    // Native undo/redo (Edit menu, IME, platform gesture) would desync the
    // buffer from our local history — route it back through undo()/redo().
    // If the WebView does not fire beforeinput for these, our keydown guard
    // still wins for keyboard chords; the residual case is the manual
    // Wails validation item called out in PLAN Phase 7.
    if (inputType === 'historyUndo' || inputType === 'historyRedo') {
      e.preventDefault()
      if (inputType === 'historyUndo') undo()
      else redo()
      return
    }
    const el = e.currentTarget as HTMLTextAreaElement
    pendingInputType = inputType
    pendingPreSelection = readSelection(el)
  }

  function onInput(e: Event): void {
    const el = e.currentTarget as HTMLTextAreaElement
    const value = el.value
    const post = readSelection(el)

    // Coalesce only plain typing that continued at the same caret the last
    // push left behind. Paste, IME composition, cut, drag-drop, and any
    // non-contiguous caret each become their own history entry.
    const current = history.current()
    const coalescable =
      pendingInputType === 'insertText' &&
      current !== null &&
      pendingPreSelection !== null &&
      pendingPreSelection.start === current.selection.start &&
      pendingPreSelection.end === current.selection.end

    commitUserEdit(value, post, { coalesce: coalescable })
    pendingInputType = ''
    pendingPreSelection = null
  }

  /** Tab/Shift+Tab transform covered lines without deleting the selection.
   *
   *  - Caret only (no selection): Tab inserts a literal `\t` at the caret;
   *    Shift+Tab removes one leading `\t` or up to two leading spaces from
   *    the caret's line (no-op when the line has no leading indent).
   *  - With a selection: Tab prefixes every covered line with `\t`; Shift+Tab
   *    removes one leading `\t` or up to two leading spaces from every
   *    covered line. The selection is adjusted to keep covering the same
   *    logical block (and, on indent, the newly-inserted prefixes).
   *
   *  "Covered" lines: the line containing `start` through the line containing
   *  the character before `end`. When `end` sits exactly at a newline (i.e.
   *  the trailing line was not actually selected), that trailing line is
   *  excluded — matches the VS Code / Sublime convention.
   *
   *  Both paths push one history entry (never coalesced with typing) and
   *  restore selection + focus via the shared `commitUserEdit` path. */
  function applyIndent(shift: boolean): void {
    const ta = textareaEl
    if (!ta) return
    const start = ta.selectionStart
    const end = ta.selectionEnd
    const val = buffer
    const direction = ta.selectionDirection ?? 'forward'

    // Caret-only path: simple insert / single-line dedent.
    if (start === end) {
      const lineStart = val.lastIndexOf('\n', start - 1) + 1
      let next: string
      let caret: number
      if (shift) {
        const lead = val.slice(lineStart, lineStart + 2)
        let remove = 0
        if (lead.startsWith('\t')) remove = 1
        else if (lead.startsWith('  ')) remove = 2
        if (remove === 0) return
        next = val.slice(0, lineStart) + val.slice(lineStart + remove)
        caret = Math.max(lineStart, start - remove)
      } else {
        next = val.slice(0, start) + '\t' + val.slice(end)
        caret = start + 1
      }
      commitUserEdit(next, {
        start: caret,
        end: caret,
        direction: 'forward'
      })
      queueMicrotask(() => textareaEl?.focus())
      return
    }

    // Multi-line path: collect the start offset of every covered line.
    const firstLineStart = val.lastIndexOf('\n', start - 1) + 1
    // If end is at a column-0 boundary the user did not select that line.
    const endForLastLine = end > 0 && val[end - 1] === '\n' ? end - 1 : end
    const lastLineStart = val.lastIndexOf('\n', endForLastLine - 1) + 1

    const lineStarts: number[] = []
    for (let i = firstLineStart; i <= lastLineStart;) {
      lineStarts.push(i)
      const nextNL = val.indexOf('\n', i)
      if (nextNL === -1) break
      i = nextNL + 1
    }
    if (lineStarts.length === 0) return

    // Build the new value by iterating in reverse so already-processed
    // positions stay valid for the next iteration's index into `next`.
    let next = val
    if (shift) {
      let removedBeforeStart = 0
      let removedBeforeEnd = 0
      let anyRemoved = false
      for (let j = lineStarts.length - 1; j >= 0; j--) {
        const pos = lineStarts[j]
        let remove = 0
        if (next[pos] === '\t') remove = 1
        else if (next.slice(pos, pos + 2) === '  ') remove = 2
        if (remove === 0) continue
        next = next.slice(0, pos) + next.slice(pos + remove)
        anyRemoved = true
        if (pos < start) removedBeforeStart += remove
        if (pos < end) removedBeforeEnd += remove
      }
      if (!anyRemoved) return
      const newStart = start - removedBeforeStart
      const newEnd = end - removedBeforeEnd
      commitUserEdit(next, { start: newStart, end: newEnd, direction })
    } else {
      for (let j = lineStarts.length - 1; j >= 0; j--) {
        const pos = lineStarts[j]
        next = next.slice(0, pos) + '\t' + next.slice(pos)
      }
      // Indenting shifts the selection right by one per covered line and
      // extends the anchor back to the first inserted prefix so a follow-up
      // Tab/Shift+Tab operates on the same block.
      const newStart = firstLineStart
      const newEnd = end + lineStarts.length
      commitUserEdit(next, { start: newStart, end: newEnd, direction })
    }
    queueMicrotask(() => textareaEl?.focus())
  }

  function applyHistoryEntry(entry: SourceHistoryEntry): void {
    buffer = entry.value
    markDirtyAndSchedule()
    queueMicrotask(() => {
      if (!textareaEl) return
      restoreSelection(entry.selection)
      textareaEl.focus()
    })
  }

  function undo(): void {
    const before = history.current()
    const entry = history.undo()
    if (!entry) return
    bumpHistory()
    applyHistoryEntry(entry)
    announceHistory(`Undid: ${describeChange(before, entry)}`)
  }

  function redo(): void {
    const before = history.current()
    const entry = history.redo()
    if (!entry) return
    bumpHistory()
    applyHistoryEntry(entry)
    announceHistory(`Redid: ${describeChange(before, entry)}`)
  }

  function isUndoKey(e: KeyboardEvent): boolean {
    return (
      (e.ctrlKey || e.metaKey) &&
      !e.shiftKey &&
      !e.altKey &&
      (e.key === 'z' || e.key === 'Z')
    )
  }

  function isRedoKey(e: KeyboardEvent): boolean {
    return (
      (e.ctrlKey || e.metaKey) &&
      (e.key === 'y' ||
        e.key === 'Y' ||
        ((e.key === 'z' || e.key === 'Z') && e.shiftKey)) &&
      !e.altKey
    )
  }

  function onKeyDown(e: KeyboardEvent): void {
    if (isUndoKey(e)) {
      e.preventDefault()
      undo()
      return
    }
    if (isRedoKey(e)) {
      e.preventDefault()
      redo()
      return
    }
    if (e.key === 'Tab') {
      e.preventDefault()
      applyIndent(e.shiftKey)
    }
  }

  async function acquireLock(): Promise<void> {
    if (!editable || !notebook || !page) return
    try {
      await AcquireFocusLock(notebook, section, page)
      hasFocusLock = true
      if (heartbeatInterval) clearInterval(heartbeatInterval)
      heartbeatInterval = setInterval(() => {
        if (!hasFocusLock) return
        RefreshFocusLock(notebook, section, page).catch(() => {})
      }, 20000)
    } catch (e) {
      console.error('MarkdownSourceViewer: AcquireFocusLock failed:', e)
    }
  }

  async function releaseLock(): Promise<void> {
    if (heartbeatInterval) {
      clearInterval(heartbeatInterval)
      heartbeatInterval = null
    }
    if (!hasFocusLock) return
    hasFocusLock = false
    if (!notebook || !page) return
    try {
      await ReleaseFocusLock(notebook, section, page)
    } catch (e) {
      console.error('MarkdownSourceViewer: ReleaseFocusLock failed:', e)
    }
  }

  onMount(() => {
    if (editable) void acquireLock()
  })

  onDestroy(() => {
    if (copyStatusTimer) clearTimeout(copyStatusTimer)
    if (historyStatusTimer) clearTimeout(historyStatusTimer)
    if (savedClearTimer) clearTimeout(savedClearTimer)
    if (saveTimer) {
      clearTimeout(saveTimer)
      // Best-effort flush on unmount when dirty (not during conflict).
      if (dirty && notebook && page && !conflictPending) {
        void savePageMarkdown(notebook, section, page, buffer)
          .then((saved) => onBlocksSaved?.(saved))
          .catch((e) => {
            console.error('MarkdownSourceViewer: unmount flush failed:', e)
          })
      }
    }
    void releaseLock()
  })
</script>

<div class="source-viewer">
  <div class="source-header">
    <span class="file-path" title={filePath}>{filePath}</span>
    <div class="header-actions">
      {#if conflictPending}
        <span class="save-status" role="status" aria-live="polite"
          >External change detected</span
        >
        <button type="button" class="conflict-btn" onclick={keepMine}>
          Keep mine
        </button>
        <button type="button" class="conflict-btn" onclick={reloadFromDisk}>
          Reload
        </button>
      {:else if saveError}
        <span
          class="save-status save-status--err"
          role="alert"
          aria-live="assertive">Save failed: {saveError}</span
        >
      {:else if savePhase === 'saved'}
        <span class="save-status" role="status" aria-live="polite">Saved</span>
      {:else if dirty}
        <span class="save-status" role="status" aria-live="polite"
          >Unsaved…</span
        >
      {/if}
      <!-- Undo/redo on the source surface only — read-only view has no edits. -->
      {#if editable}
        <div class="history-actions" role="group" aria-label="Undo / redo">
          <button
            type="button"
            class="icon-btn"
            onclick={undo}
            disabled={!canUndo}
            aria-label="Undo"
            title="Undo (Ctrl+Z / Cmd+Z)"
            aria-keyshortcuts="Control+Z Meta+Z"
          >
            <span class="material-symbols-outlined" aria-hidden="true"
              >undo</span
            >
          </button>
          <button
            type="button"
            class="icon-btn"
            onclick={redo}
            disabled={!canRedo}
            aria-label="Redo"
            title="Redo (Ctrl+Y / Ctrl+Shift+Z / Cmd+Shift+Z)"
            aria-keyshortcuts="Control+Y Control+Shift+Z Meta+Shift+Z"
          >
            <span class="material-symbols-outlined" aria-hidden="true"
              >redo</span
            >
          </button>
        </div>
      {/if}
      <button
        type="button"
        class="copy-btn"
        onclick={copyAsMarkdown}
        aria-label="Copy markdown to clipboard"
      >
        <span class="material-symbols-outlined" aria-hidden="true"
          >content_copy</span
        >
        Copy as Markdown
      </button>
      {#if copyStatus}
        <span
          class="copy-status"
          class:copy-status--err={copyStatus.kind === 'err'}
          role={copyStatus.kind === 'err' ? 'alert' : 'status'}
          aria-live={copyStatus.kind === 'err' ? 'assertive' : 'polite'}
          >{copyStatus.msg}</span
        >
      {/if}
    </div>
  </div>
  <div
    class="source-body"
    role={editable ? undefined : 'document'}
    aria-label={editable ? undefined : `Source view of ${filePath}`}
  >
    <div class="line-numbers" aria-hidden="true">
      {#each Array(lineCount) as _, i (i)}
        <span class="line-num">{i + 1}</span>
      {/each}
    </div>
    {#if editable}
      <textarea
        bind:this={textareaEl}
        class="source-code source-textarea"
        value={buffer}
        wrap="off"
        onbeforeinput={onBeforeInput}
        oninput={onInput}
        onkeydown={onKeyDown}
        onfocus={() => {
          if (!hasFocusLock) void acquireLock()
        }}
        spellcheck="false"
        aria-label="Markdown source of {filePath}"
        aria-keyshortcuts="Control+Z Control+Y Control+Shift+Z Meta+Z Meta+Shift+Z"
        rows={lineCount}></textarea>
    {:else}
      <pre
        class="source-code"><!-- eslint-disable-next-line svelte/no-at-html-tags -- highlight.js on local buffer -->
        {#if highlightedHtml}{@html highlightedHtml}{:else}{buffer}{/if}</pre>
    {/if}
  </div>
  <!-- Stable live region: undo/redo needs to be perceivable to AT users
       without moving focus, and a fresh mount node can be missed. No
       explicit role so it does not collide with the copy/save status
       role="status" queries exercised by tests. -->
  <span class="sr-only" aria-live="polite">{historyStatus}</span>
</div>

<style>
  .source-viewer {
    display: flex;
    flex-direction: column;
    height: 100%;
    min-height: 12rem;
    background: var(--color-surface-panel);
  }

  .source-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
    padding: 6px 12px;
    border-bottom: 1px solid var(--color-surface-panel-border);
    flex-shrink: 0;
  }

  .file-path {
    font-size: 0.75rem;
    color: var(--color-text-muted);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    min-width: 0;
  }

  .header-actions {
    display: flex;
    align-items: center;
    gap: 8px;
    flex-shrink: 0;
  }

  .copy-btn,
  .conflict-btn,
  .icon-btn {
    display: flex;
    align-items: center;
    gap: 4px;
    padding: 3px 8px;
    border: 1px solid var(--color-surface-panel-border);
    border-radius: 6px;
    background: transparent;
    color: var(--color-text-muted);
    font-size: 0.72rem;
    cursor: pointer;
  }

  .icon-btn {
    padding: 3px 4px;
    line-height: 0;
  }

  .icon-btn:disabled {
    cursor: not-allowed;
    opacity: 0.4;
  }

  .copy-btn:hover,
  .conflict-btn:hover,
  .icon-btn:not(:disabled):hover {
    background: color-mix(
      in srgb,
      var(--color-accent-primary-start) 15%,
      transparent
    );
    color: var(--color-text-primary);
  }

  .icon-btn:focus-visible,
  .copy-btn:focus-visible,
  .conflict-btn:focus-visible {
    outline: 2px solid var(--color-accent-primary-start);
    outline-offset: 1px;
  }

  .copy-btn .material-symbols-outlined,
  .icon-btn .material-symbols-outlined {
    font-size: 14px;
  }

  .copy-status,
  .save-status {
    font-size: 0.7rem;
    color: var(--color-text-muted);
    white-space: nowrap;
  }

  .copy-status--err,
  .save-status--err {
    color: var(--color-status-danger);
  }

  /* Single scroll owner: source-body scrolls vertically so line-numbers and
     textarea move together; horizontal overflow lives on the textarea itself
     (line-numbers are positional and stay fixed on the left while long lines
     scroll under them, the standard code-editor split). Vertical scroll
     never competes with the outer VSC container — h-full on the page-zoom
     wrapper in Source mode bounds the chain. */
  .source-body {
    display: flex;
    overflow-y: auto;
    overflow-x: hidden;
    flex: 1;
    min-height: 0;
  }

  .line-numbers {
    display: flex;
    flex-direction: column;
    padding: 8px 8px 8px 12px;
    text-align: right;
    user-select: none;
    border-right: 1px solid var(--color-surface-panel-border);
    flex-shrink: 0;
  }

  .line-num {
    font-family: var(--font-mono, monospace);
    font-size: 0.75rem;
    line-height: 1.6;
    color: var(--color-text-muted);
    opacity: 0.5;
  }

  .source-code {
    margin: 0;
    padding: 8px 12px;
    font-family: var(--font-mono, monospace);
    font-size: 0.8rem;
    line-height: 1.6;
    color: var(--color-text-primary);
    white-space: pre;
    word-break: normal;
    flex: 1;
  }

  .source-textarea {
    resize: none;
    border: none;
    outline: none;
    background: transparent;
    width: 100%;
    min-height: 100%;
    box-sizing: border-box;
    /* rows={lineCount} makes the textarea tall enough that vertical never
       overflows its own box, so the vertical scrollbar lives on .source-body.
       Long lines (wrap="off") used to be clipped here, hiding typed text
       past the right edge; horizontal auto lets the textarea scroll its own
       content into view without disturbing the line-number column. */
    overflow-x: auto;
    overflow-y: hidden;
  }

  .source-textarea:focus {
    outline: 1px solid
      color-mix(in srgb, var(--color-accent-primary-start) 40%, transparent);
    outline-offset: -1px;
    border-radius: 2px;
  }

  .sr-only {
    position: absolute;
    width: 1px;
    height: 1px;
    padding: 0;
    margin: -1px;
    overflow: hidden;
    clip: rect(0, 0, 0, 0);
    white-space: nowrap;
    border: 0;
  }

  @media (prefers-reduced-motion: reduce) {
    .source-textarea:focus {
      transition: none;
    }
  }
</style>
