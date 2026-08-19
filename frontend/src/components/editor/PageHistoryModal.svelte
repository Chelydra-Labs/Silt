<script lang="ts">
  import { onMount, tick } from 'svelte'
  import {
    ListPageVersions,
    GetPageVersion,
    RestorePageVersion,
    RestoreDeletedPageVersion
  } from '../../../bindings/silt/app.js'
  import { trapFocus } from '../../lib/focusTrap'
  import { settings } from '../../settings/store.svelte'
  import { editorKey, getEditor } from '../../lib/editor/editorRegistry.svelte'
  import { fetchPageMarkdown } from '../../lib/editor/pageMarkdown'
  import {
    diffPageBodies,
    type DiffHunk,
    type WordPart
  } from '../../lib/editor/pageDiff'
  import { coerceIPCError } from '../../lib/ipcError'
  import { IPCErrorCode } from '../../generated/enums'
  import { openDeletedPageHistory } from './openDeletedPageHistory'
  import ConfirmDialog from '../ConfirmDialog.svelte'

  interface PageVersionRow {
    id: string
    timestamp: string
    source: string
    bytes: number
  }

  interface DiffLine {
    text: string
    parts?: WordPart[]
  }

  type HistoryPane = 'preview' | 'compare'
  type DiffLayout = 'split' | 'unified'

  interface Props {
    notebook: string
    section: string
    page: string
    deleted?: boolean
    source?: string
    onBack?: () => void
    onClose: () => void
  }

  let {
    notebook,
    section,
    page,
    deleted = false,
    source = '',
    onBack,
    onClose
  }: Props = $props()

  const SOURCE_LABELS: Record<string, string> = {
    editor: 'Editor',
    source: 'Source',
    mcp: 'Agent',
    plugin: 'Plugin',
    restore: 'Restore',
    rename: 'Rename'
  }

  const dtf = new Intl.DateTimeFormat(undefined, {
    year: 'numeric',
    month: 'short',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit'
  })

  let dialogRef = $state<HTMLDivElement | null>(null)
  let listRef = $state<HTMLDivElement | null>(null)
  let previouslyFocused: HTMLElement | null = null
  let versions = $state<PageVersionRow[]>([])
  let selectedId = $state<string | null>(null)
  let preview = $state('')
  let listLoading = $state(true)
  let previewLoading = $state(false)
  let listError = $state('')
  let previewError = $state('')
  let restoreError = $state('')
  let restoreTarget = $state<PageVersionRow | null>(null)
  let restoring = $state(false)
  let previewedId = $state<string | null>(null)
  let previewGen = 0
  let statusMessage = $state('')

  let paneMode = $state<HistoryPane>('preview')
  let layoutOverride = $state<DiffLayout | null>(null)
  let preferSplit = $state(true)
  let liveBody = $state('')
  let compareLoading = $state(false)
  let compareError = $state('')
  let compareGen = 0
  let expandedHunks = $state<Record<number, boolean>>({})
  let restoreAs = $state(false)
  let destNotebook = $state('')
  let destSection = $state('')
  let destPage = $state('')
  let destPageRef = $state<HTMLInputElement | null>(null)

  let versioningEnabled = $derived(
    settings.config?.editor?.auto_versioning_enabled === true
  )
  let selected = $derived(versions.find((v) => v.id === selectedId) ?? null)
  let previewReady = $derived(
    !!selected &&
      selected.id === previewedId &&
      !previewLoading &&
      !previewError
  )
  let layout = $derived<DiffLayout>(
    layoutOverride ?? (preferSplit ? 'split' : 'unified')
  )
  let liveReady = $state(false)
  let compareFromSaved = $state(false)
  let pageDiff = $derived(
    paneMode === 'compare' && previewReady && liveReady
      ? diffPageBodies(preview, liveBody)
      : null
  )
  let diffSummary = $derived(pageDiff ? formatDiffSummary(pageDiff) : '')

  function sourceLabel(source: string): string {
    return SOURCE_LABELS[source] ?? source
  }

  function formatTimestamp(iso: string): string {
    if (!iso) return 'Unknown time'
    const d = new Date(iso)
    return Number.isNaN(d.getTime()) ? iso : dtf.format(d)
  }

  function formatBytes(n: number): string {
    if (!Number.isFinite(n) || n < 0) return ''
    if (n < 1024) return `${n} B`
    if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`
    return `${(n / (1024 * 1024)).toFixed(1)} MB`
  }

  function formatLineCount(n: number, verb: string): string {
    return `${n} ${n === 1 ? 'line' : 'lines'} ${verb}`
  }

  function formatDiffSummary(diff: {
    addedLines: number
    removedLines: number
    tooLarge?: boolean
  }): string {
    if (diff.tooLarge) {
      return 'This comparison is too large to show.'
    }
    if (diff.addedLines === 0 && diff.removedLines === 0) {
      return 'No body changes. Frontmatter is not compared.'
    }
    const parts: string[] = []
    if (diff.addedLines > 0) {
      parts.push(formatLineCount(diff.addedLines, 'added'))
    }
    if (diff.removedLines > 0) {
      parts.push(formatLineCount(diff.removedLines, 'removed'))
    }
    return parts.join(', ')
  }

  function splitBodyLines(text: string): string[] {
    if (text === '') return []
    const parts = text.split('\n')
    if (parts[parts.length - 1] === '') parts.pop()
    return parts
  }

  function linesFromWords(words: WordPart[]): DiffLine[] {
    const lines: DiffLine[] = [{ text: '', parts: [] }]
    for (const word of words) {
      const segs = word.text.split('\n')
      segs.forEach((seg, i) => {
        if (i > 0) lines.push({ text: '', parts: [] })
        const line = lines[lines.length - 1]
        line.text += seg
        if (seg !== '' || segs.length === 1) {
          line.parts = [...(line.parts ?? []), { text: seg, kind: word.kind }]
        }
      })
    }
    if (lines.length > 1 && lines[lines.length - 1].text === '') lines.pop()
    return lines
  }

  function sideLines(hunk: DiffHunk, side: 'previous' | 'current'): DiffLine[] {
    const words = side === 'previous' ? hunk.previousWords : hunk.currentWords
    const text = side === 'previous' ? hunk.previous : hunk.current
    if (hunk.kind === 'replace' && words && words.length > 0) {
      return linesFromWords(words)
    }
    return splitBodyLines(text).map((line) => ({ text: line }))
  }

  function hunkCollapsed(hunk: DiffHunk, index: number): boolean {
    return hunk.kind === 'equal' && !!hunk.collapsed && !expandedHunks[index]
  }

  function expandHunk(index: number): void {
    expandedHunks = { ...expandedHunks, [index]: true }
    void tick().then(() => {
      document
        .querySelector<HTMLElement>('[data-testid="page-history-compare"]')
        ?.focus()
    })
  }

  function readPreferSplit(): boolean {
    if (typeof window.matchMedia !== 'function') return true
    return !window.matchMedia('(max-width: 720px)').matches
  }

  async function loadVersions(preferId?: string | null): Promise<void> {
    listLoading = true
    listError = ''
    try {
      const rows = (await ListPageVersions(notebook, section, page)) ?? []
      versions = rows.map((r) => ({
        id: r.id,
        timestamp: r.timestamp,
        source: r.source,
        bytes: r.bytes
      }))
      const next =
        (preferId && versions.some((v) => v.id === preferId)
          ? preferId
          : null) ??
        versions[0]?.id ??
        null
      selectedId = next
      // Show the list before the first preview so a hung GetPageVersion
      // cannot leave the picker stuck on “Loading versions…”.
      listLoading = false
      if (next) {
        await loadPreview(next)
        await tick()
        listRef?.focus()
      } else {
        preview = ''
        previewError = ''
        previewedId = null
      }
    } catch (e) {
      listError = coerceIPCError(e).message
      versions = []
      selectedId = null
      preview = ''
    } finally {
      listLoading = false
    }
  }

  async function loadPreview(versionID: string): Promise<void> {
    const gen = ++previewGen
    previewLoading = true
    previewError = ''
    try {
      const body =
        (await GetPageVersion(notebook, section, page, versionID)) ?? ''
      if (gen !== previewGen) return
      preview = body
      previewedId = versionID
    } catch (e) {
      if (gen !== previewGen) return
      preview = ''
      previewedId = null
      previewError = coerceIPCError(e).message
    } finally {
      if (gen === previewGen) previewLoading = false
    }
  }

  function selectVersion(id: string): void {
    if (selectedId === id) return
    statusMessage = ''
    selectedId = id
    expandedHunks = {}
    void loadPreview(id)
    scrollSelectedIntoView()
  }

  function moveSelection(delta: number): void {
    if (versions.length === 0) return
    const idx = versions.findIndex((v) => v.id === selectedId)
    const from = idx < 0 ? 0 : idx
    const next = Math.max(0, Math.min(versions.length - 1, from + delta))
    selectVersion(versions[next].id)
  }

  function versionOptionId(id: string): string {
    return `page-history-${encodeURIComponent(id)}`
  }

  function scrollSelectedIntoView(): void {
    queueMicrotask(() => {
      if (!listRef || !selectedId) return
      const el = listRef.querySelector(
        `[data-version-id="${CSS.escape(selectedId)}"]`
      )
      el?.scrollIntoView?.({ block: 'nearest' })
    })
  }

  function canRestoreSelected(): boolean {
    if (
      !selected ||
      selected.id !== previewedId ||
      previewLoading ||
      previewError ||
      restoring
    ) {
      return false
    }
    if (deleted && restoreAs) {
      return destNotebook.trim() !== '' && destPage.trim() !== ''
    }
    return true
  }

  function suggestRestoredPageName(name: string): string {
    const raw = name.trim() || 'Page'
    const numbered = /^(.*?)(?:\s+(\d+))$/.exec(raw)
    if (!numbered) return `${raw} 2`
    const n = Number(numbered[2])
    // Only increment small collision suffixes ("Daily 2" → "Daily 3").
    // Years and other meaningful numbers stay ("Budget 2026" → "Budget 2026 2").
    if (Number.isFinite(n) && n >= 2 && n < 100) {
      return `${numbered[1].trim() || 'Page'} ${n + 1}`
    }
    return `${raw} 2`
  }

  function restoreDestination(): {
    notebook: string
    section: string
    page: string
  } {
    if (restoreAs && destNotebook.trim() && destPage.trim()) {
      return {
        notebook: destNotebook.trim(),
        section: destSection.trim(),
        page: destPage.trim()
      }
    }
    return { notebook, section, page }
  }

  function formatLocatorPath(nb: string, sec: string, pg: string): string {
    return sec ? `${nb} / ${sec} / ${pg}` : `${nb} / ${pg}`
  }

  function choosePane(mode: HistoryPane): void {
    statusMessage = ''
    if (mode === 'preview') {
      compareGen += 1
      paneMode = 'preview'
      compareLoading = false
      liveReady = false
      compareFromSaved = false
      return
    }
    if (deleted || paneMode === 'compare' || compareLoading) return
    void enterCompare()
  }

  function onPaneKeydown(e: KeyboardEvent): void {
    if (!['ArrowRight', 'ArrowLeft', 'Home', 'End'].includes(e.key)) return
    const canOpen = !deleted && previewReady && !compareLoading
    const options: HistoryPane[] = canOpen
      ? ['preview', 'compare']
      : ['preview']
    const current = paneMode
    let idx = options.indexOf(current)
    if (idx < 0) idx = 0
    if (e.key === 'Home') idx = 0
    else if (e.key === 'End') idx = options.length - 1
    else if (e.key === 'ArrowRight') idx = (idx + 1) % options.length
    else idx = (idx - 1 + options.length) % options.length
    e.preventDefault()
    const next = options[idx]
    choosePane(next)
    const root = e.currentTarget as HTMLElement
    root.querySelector<HTMLElement>(`[data-history-pane="${next}"]`)?.focus()
  }

  function chooseLayout(next: DiffLayout): void {
    layoutOverride = next
  }

  function onLayoutKeydown(e: KeyboardEvent): void {
    if (!['ArrowRight', 'ArrowLeft', 'Home', 'End'].includes(e.key)) return
    const options: DiffLayout[] = ['split', 'unified']
    let idx = options.indexOf(layout)
    if (idx < 0) idx = 0
    if (e.key === 'Home') idx = 0
    else if (e.key === 'End') idx = options.length - 1
    else if (e.key === 'ArrowRight') idx = (idx + 1) % options.length
    else idx = (idx - 1 + options.length) % options.length
    e.preventDefault()
    chooseLayout(options[idx])
    const root = e.currentTarget as HTMLElement
    root
      .querySelector<HTMLElement>(`[data-diff-layout="${options[idx]}"]`)
      ?.focus()
  }

  async function enterCompare(): Promise<void> {
    if (deleted || !previewReady || !selected || compareLoading) return
    const gen = ++compareGen
    compareLoading = true
    compareError = ''
    paneMode = 'compare'
    try {
      const editor = getEditor(editorKey(notebook, section, page))
      const dirty = editor?.isDirty() === true
      const body = await fetchPageMarkdown(notebook, section, page)
      if (gen !== compareGen) return
      liveBody = body
      liveReady = true
      compareFromSaved = dirty
      expandedHunks = {}
    } catch (e) {
      if (gen !== compareGen) return
      paneMode = 'preview'
      liveReady = false
      compareFromSaved = false
      compareError = coerceIPCError(e).message
    } finally {
      if (gen === compareGen) compareLoading = false
    }
  }

  function openRestoreConfirm(): void {
    if (!canRestoreSelected() || !selected) return
    restoreError = ''
    restoreTarget = selected
  }

  async function confirmRestore(): Promise<void> {
    if (!restoreTarget || restoring) return
    if (restoreTarget.id !== previewedId) {
      restoreTarget = null
      restoreError =
        'Preview is still loading. Select the version again, then restore.'
      return
    }
    const target = restoreTarget
    restoring = true
    restoreError = ''
    try {
      if (deleted) {
        const dest = restoreAs
          ? [destNotebook.trim(), destSection.trim(), destPage.trim()]
          : ['', '', '']
        try {
          await RestoreDeletedPageVersion(
            notebook,
            section,
            page,
            target.id,
            dest[0],
            dest[1],
            dest[2]
          )
        } catch (err) {
          const ipc = coerceIPCError(err)
          if (ipc.code === IPCErrorCode.CodePageStillExists) {
            restoreTarget = null
            restoreError =
              ipc.message ||
              'That page still exists. Restore it from page history instead.'
            return
          }
          if (ipc.code === IPCErrorCode.CodePageExists) {
            restoreTarget = null
            const firstCollision = !restoreAs
            restoreAs = true
            if (firstCollision) {
              destNotebook = destNotebook.trim() || notebook
              destSection = destSection.trim() || section
            }
            destPage = suggestRestoredPageName(destPage.trim() || page)
            restoreError =
              ipc.message ||
              'Restoring here would overwrite an existing page. Choose a different location.'
            void tick().then(() => destPageRef?.focus())
            return
          }
          throw err
        }
        restoreTarget = null
        const destLoc = restoreDestination()
        window.dispatchEvent(
          new CustomEvent('navigate-to-page', {
            detail: {
              notebook: destLoc.notebook,
              section: destLoc.section,
              page: destLoc.page
            }
          })
        )
        onClose()
        return
      }

      const editor = getEditor(editorKey(notebook, section, page))
      if (editor?.isDirty()) {
        const clean = await editor.flush()
        if (!clean) {
          restoreTarget = null
          restoreError =
            "Couldn't save the current page before restoring. Fix the save error, then try again."
          return
        }
      }
      // Arm the focused-edit bypass before the write so block:changed
      // reloads the restored body instead of letting a dirty buffer win.
      editor?.forceExternalReload()
      try {
        await RestorePageVersion(notebook, section, page, target.id)
      } catch (err) {
        editor?.clearExternalReload()
        throw err
      }
      restoreTarget = null
      statusMessage = `Restored version from ${formatTimestamp(target.timestamp)}. A snapshot of the previous page was kept.`
      compareGen += 1
      paneMode = 'preview'
      compareLoading = false
      liveReady = false
      liveBody = ''
      compareFromSaved = false
      compareError = ''
      restoreError = ''
      await loadVersions(target.id)
    } catch (e) {
      restoreError = coerceIPCError(e).message
    } finally {
      restoring = false
    }
  }

  function openDeletedPages(): void {
    onClose()
    openDeletedPageHistory()
  }

  function cancelRestore(): void {
    if (restoring) return
    restoreTarget = null
  }

  function openEditorSettings(): void {
    onClose()
    window.dispatchEvent(new CustomEvent('open-settings', { detail: 'editor' }))
  }

  function dismissHistory(): void {
    if (deleted && onBack && !restoring) {
      onBack()
      return
    }
    onClose()
  }

  function handleKeydown(e: KeyboardEvent): void {
    // ConfirmDialog owns Esc / Tab while it is open.
    if (restoreTarget) return
    if (e.key === 'Escape') {
      e.preventDefault()
      e.stopPropagation()
      dismissHistory()
      return
    }
    const inList = !!(e.target as HTMLElement | null)?.closest(
      '[data-testid="page-history-list"]'
    )
    if (!inList) return
    if (e.key === 'ArrowDown') {
      e.preventDefault()
      moveSelection(1)
      return
    }
    if (e.key === 'ArrowUp') {
      e.preventDefault()
      moveSelection(-1)
      return
    }
    if (e.key === 'Home') {
      e.preventDefault()
      if (versions[0]) selectVersion(versions[0].id)
      return
    }
    if (e.key === 'End') {
      e.preventDefault()
      const last = versions[versions.length - 1]
      if (last) selectVersion(last.id)
      return
    }
    if (e.key === 'Enter') {
      if (versions.length === 0) return
      e.preventDefault()
      if (selectedId) void loadPreview(selectedId)
    }
  }

  onMount(() => {
    previouslyFocused = document.activeElement as HTMLElement | null
    window.addEventListener('keydown', handleKeydown, true)
    preferSplit = readPreferSplit()
    const mq =
      typeof window.matchMedia === 'function'
        ? window.matchMedia('(max-width: 720px)')
        : null
    const onViewport = () => {
      preferSplit = readPreferSplit()
    }
    mq?.addEventListener('change', onViewport)
    void tick().then(() => dialogRef?.focus())
    void loadVersions()
    return () => {
      window.removeEventListener('keydown', handleKeydown, true)
      mq?.removeEventListener('change', onViewport)
      if (previouslyFocused?.isConnected) previouslyFocused.focus?.()
    }
  })

  $effect(() => {
    if (restoreTarget || !dialogRef) return
    const dispose = trapFocus(dialogRef)
    return () => dispose()
  })
</script>

<div
  class="fixed inset-0 z-[180] flex items-center justify-center bg-black/45 backdrop-blur-[3px] px-4"
  data-focus-trap
>
  <button
    type="button"
    tabindex="-1"
    aria-label={deleted && onBack
      ? 'Back to deleted pages'
      : 'Close page history'}
    class="absolute inset-0 cursor-default border-none bg-transparent p-0"
    onclick={dismissHistory}
  ></button>

  <div
    bind:this={dialogRef}
    role="dialog"
    aria-modal="true"
    aria-labelledby="page-history-title"
    aria-describedby="page-history-path page-history-honesty"
    tabindex="-1"
    data-testid="page-history-modal"
    class="dialog-surface relative flex h-[min(36rem,calc(100vh-4rem))] w-full flex-col overflow-hidden rounded-xl border border-surface-modal-border glass-palette glass-palette-strong shadow-2xl"
    class:is-comparing={paneMode === 'compare'}
  >
    <header
      class="flex items-center gap-3 border-b border-surface-modal-border px-5 py-3.5"
    >
      <span
        class="material-symbols-outlined text-icon-xl text-accent-primary-start"
        aria-hidden="true">history</span
      >
      <div class="min-w-0 flex-1">
        <h2
          id="page-history-title"
          class="font-headline-md text-headline-md text-text-primary"
        >
          {deleted ? 'Deleted page history' : 'Page history'}
        </h2>
        <p
          id="page-history-path"
          class="truncate text-type-xs font-body-md text-text-muted"
          title="{notebook}{section ? ` / ${section}` : ''} / {page}"
        >
          {notebook}{section ? ` / ${section}` : ''} / {page}
        </p>
        <p
          id="page-history-honesty"
          class="mt-1 text-type-2xs font-body-md text-text-muted"
        >
          Snapshots stay in this vault under .system/history. They sync with the
          vault, are not encrypted, and are not a backup.
        </p>
      </div>
      <div class="flex shrink-0 items-center gap-1">
        {#if deleted && onBack}
          <button
            type="button"
            data-testid="page-history-back"
            onclick={onBack}
            class="rounded-lg border-none bg-transparent px-2.5 py-1.5 text-type-xs font-label-sm-bold text-text-muted transition-colors hover:bg-hover hover:text-text-primary"
          >
            Back
          </button>
        {:else if !deleted}
          <button
            type="button"
            data-testid="page-history-deleted-pages"
            onclick={openDeletedPages}
            class="rounded-lg border-none bg-transparent px-2.5 py-1.5 text-type-xs font-label-sm-bold text-text-muted transition-colors hover:bg-hover hover:text-text-primary"
          >
            Deleted pages
          </button>
        {/if}
        <button
          type="button"
          aria-label={deleted && onBack
            ? 'Back to deleted pages'
            : 'Close page history'}
          onclick={dismissHistory}
          class="flex h-8 w-8 items-center justify-center rounded-lg border-none bg-transparent text-text-muted transition-colors hover:bg-hover hover:text-text-primary"
        >
          <span
            class="material-symbols-outlined text-icon-lg"
            aria-hidden="true">close</span
          >
        </button>
      </div>
    </header>

    {#if listError}
      <div
        class="mx-5 mt-4 flex items-start gap-2 rounded-lg border border-error-border bg-error-bg px-3 py-2 text-type-sm font-body-md text-error"
        role="alert"
      >
        <span class="material-symbols-outlined text-icon-lg" aria-hidden="true"
          >error</span
        >
        <span class="flex-1">{listError}</span>
        <button
          type="button"
          class="rounded-md border border-error-border bg-transparent px-2 py-1 text-type-xs font-label-sm-bold text-error hover:brightness-110"
          onclick={() => void loadVersions(selectedId)}
        >
          Retry
        </button>
      </div>
    {/if}

    {#if statusMessage}
      <div
        class="mx-5 mt-4 rounded-lg border border-surface-modal-border bg-surface-panel/40 px-3 py-2 text-type-sm font-body-md text-text-primary"
        role="status"
        data-testid="page-history-status"
      >
        {statusMessage}
      </div>
    {/if}

    {#if restoreError}
      <div
        id="page-history-restore-error"
        class="mx-5 mt-4 flex items-start gap-2 rounded-lg border border-error-border bg-error-bg px-3 py-2 text-type-sm font-body-md text-error"
        role="alert"
      >
        <span class="material-symbols-outlined text-icon-lg" aria-hidden="true"
          >error</span
        >
        <span class="flex-1">{restoreError}</span>
      </div>
    {/if}

    {#if compareError}
      <div
        class="mx-5 mt-4 flex items-start gap-2 rounded-lg border border-error-border bg-error-bg px-3 py-2 text-type-sm font-body-md text-error"
        role="alert"
        data-testid="page-history-compare-error"
      >
        <span class="material-symbols-outlined text-icon-lg" aria-hidden="true"
          >error</span
        >
        <span class="flex-1">{compareError}</span>
      </div>
    {/if}

    {#if deleted && restoreAs}
      <div
        class="restore-as mx-5 mt-4 rounded-lg border border-surface-modal-border bg-surface-panel/40 px-3 py-3"
        data-testid="page-history-restore-as"
      >
        <p class="mb-2 text-type-sm font-label-sm-bold text-text-primary">
          Restore as…
        </p>
        <p class="mb-3 text-type-xs font-body-md text-text-muted">
          A page already exists at that location. Choose another name or place
          in this vault.
        </p>
        <div class="restore-as-fields">
          <label class="restore-as-field">
            <span>Notebook</span>
            <input
              bind:value={destNotebook}
              type="text"
              autocomplete="off"
              aria-label="Restore as notebook"
              aria-invalid={restoreAs && restoreError ? true : undefined}
              aria-describedby={restoreAs && restoreError
                ? 'page-history-restore-error'
                : undefined}
              data-testid="page-history-dest-notebook"
              readonly={source === 'linked'}
            />
          </label>
          <label class="restore-as-field">
            <span>Section</span>
            <input
              bind:value={destSection}
              type="text"
              autocomplete="off"
              aria-label="Restore as section"
              aria-invalid={restoreAs && restoreError ? true : undefined}
              aria-describedby={restoreAs && restoreError
                ? 'page-history-restore-error'
                : undefined}
              data-testid="page-history-dest-section"
            />
          </label>
          <label class="restore-as-field">
            <span>Page</span>
            <input
              bind:this={destPageRef}
              bind:value={destPage}
              type="text"
              autocomplete="off"
              aria-label="Restore as page"
              aria-invalid={restoreAs && restoreError ? true : undefined}
              aria-describedby={restoreAs && restoreError
                ? 'page-history-restore-error'
                : undefined}
              data-testid="page-history-dest-page"
            />
          </label>
        </div>
      </div>
    {/if}

    <div class="history-split min-h-0 flex-1">
      {#if !listLoading && versions.length === 0 && !listError}
        <div
          class="history-list custom-scrollbar flex flex-col items-start gap-2 px-4 py-6"
          role="status"
          data-testid="page-history-empty"
        >
          <p class="text-type-md font-body-md text-text-primary">
            No versions yet
          </p>
          {#if !versioningEnabled}
            <p class="text-type-sm font-body-md text-text-muted">
              Turn on Capture page history in Settings → Editor to start saving
              snapshots.
            </p>
            <button
              type="button"
              class="mt-1 rounded-lg border border-accent-primary-start/40 bg-accent-primary-start/15 px-3 py-1.5 text-type-sm font-label-sm-bold text-text-primary transition-all hover:brightness-110"
              onclick={openEditorSettings}
            >
              Open Editor settings
            </button>
          {:else}
            <p class="text-type-sm font-body-md text-text-muted">
              Snapshots appear after the next saved change to this page. Pages
              larger than 1 MB are not snapshotted.
            </p>
          {/if}
          {#if !deleted}
            <button
              type="button"
              class="mt-1 rounded-lg border border-surface-modal-border bg-transparent px-3 py-1.5 text-type-sm font-label-sm-bold text-text-muted transition-colors hover:bg-hover hover:text-text-primary"
              data-testid="page-history-empty-deleted"
              onclick={openDeletedPages}
            >
              Deleted pages
            </button>
          {/if}
        </div>
      {:else}
        <div
          bind:this={listRef}
          class="history-list custom-scrollbar"
          role="listbox"
          aria-label="Versions"
          aria-busy={listLoading || undefined}
          tabindex="0"
          aria-activedescendant={versions.length > 0 && selectedId
            ? versionOptionId(selectedId)
            : undefined}
          data-testid="page-history-list"
        >
          {#if listLoading}
            <div
              class="flex items-center gap-2 px-4 py-6 text-type-sm font-body-md text-text-muted"
              role="status"
            >
              <span
                class="material-symbols-outlined animate-spin text-icon-lg text-accent-primary-start"
                aria-hidden="true">sync</span
              >
              Loading versions…
            </div>
          {:else}
            {#each versions as version, idx (version.id)}
              <button
                type="button"
                role="option"
                id={versionOptionId(version.id)}
                data-version-id={version.id}
                aria-selected={selectedId === version.id}
                tabindex="-1"
                class="version-row"
                class:selected={selectedId === version.id}
                onclick={() => selectVersion(version.id)}
              >
                <span class="version-time"
                  >{formatTimestamp(version.timestamp)}</span
                >
                <span class="version-meta">
                  <span class="source-badge source-{version.source}">
                    {sourceLabel(version.source)}
                  </span>
                  {#if Number.isFinite(version.bytes) && version.bytes >= 0}
                    <span class="version-bytes"
                      >{formatBytes(version.bytes)}</span
                    >
                  {/if}
                </span>
                {#if idx === 0}
                  <span class="sr-only">Newest</span>
                {/if}
              </button>
            {/each}
          {/if}
        </div>
      {/if}

      {#if versions.length > 0 || listLoading || listError}
        <section
          class="history-preview"
          aria-label={paneMode === 'compare'
            ? 'Compare with current page'
            : 'Version preview'}
          data-testid="page-history-preview"
        >
          {#if selected}
            <div class="preview-toolbar">
              <div class="min-w-0 flex-1">
                <p class="truncate text-type-sm font-body-md text-text-primary">
                  {formatTimestamp(selected.timestamp)}
                </p>
                <p class="text-type-2xs font-label-sm text-text-muted">
                  {sourceLabel(selected.source)}
                  {#if Number.isFinite(selected.bytes) && selected.bytes >= 0}
                    · {formatBytes(selected.bytes)}
                  {/if}
                  · read-only
                </p>
              </div>
              <div class="preview-toolbar-actions">
                <div
                  class="pane-switch"
                  role="radiogroup"
                  aria-label="History pane"
                  aria-busy={compareLoading || undefined}
                  aria-describedby={deleted
                    ? 'page-history-compare-unavailable'
                    : undefined}
                  tabindex="-1"
                  data-testid="page-history-pane"
                  onkeydown={onPaneKeydown}
                >
                  <button
                    type="button"
                    role="radio"
                    data-history-pane="preview"
                    data-testid="page-history-pane-preview"
                    aria-checked={paneMode === 'preview'}
                    tabindex={paneMode === 'preview' ? 0 : -1}
                    class="pane-switch-btn"
                    class:active={paneMode === 'preview'}
                    onclick={() => choosePane('preview')}
                  >
                    Preview
                  </button>
                  <button
                    type="button"
                    role="radio"
                    data-history-pane="compare"
                    data-testid="page-history-pane-compare"
                    aria-checked={paneMode === 'compare'}
                    aria-disabled={deleted ||
                      (!previewReady && paneMode !== 'compare')}
                    disabled={deleted ||
                      (!previewReady && paneMode !== 'compare')}
                    title={deleted ? 'No current page to compare' : undefined}
                    tabindex={paneMode === 'compare' ? 0 : -1}
                    class="pane-switch-btn"
                    class:active={paneMode === 'compare'}
                    onclick={() => choosePane('compare')}
                  >
                    {#if compareLoading}
                      <span
                        class="material-symbols-outlined animate-spin text-icon-xs"
                        aria-hidden="true">sync</span
                      >
                    {/if}
                    Compare
                  </button>
                </div>
                {#if deleted}
                  <span id="page-history-compare-unavailable" class="sr-only">
                    No current page to compare
                  </span>
                {/if}
                <button
                  type="button"
                  data-page-history-restore
                  data-testid="page-history-restore"
                  disabled={!canRestoreSelected()}
                  onclick={openRestoreConfirm}
                  class="shrink-0 rounded-lg border border-status-danger/40 bg-status-danger/15 px-3 py-1.5 text-type-sm font-label-sm-bold text-status-danger transition-all hover:brightness-110 disabled:cursor-not-allowed disabled:opacity-50"
                >
                  Restore
                </button>
              </div>
            </div>
            {#if paneMode === 'compare'}
              {#if compareLoading || (previewLoading && !pageDiff)}
                <div
                  class="flex items-center gap-2 px-5 py-6 text-type-sm font-body-md text-text-muted"
                  role="status"
                >
                  <span
                    class="material-symbols-outlined animate-spin text-icon-lg text-accent-primary-start"
                    aria-hidden="true">sync</span
                  >
                  Loading comparison…
                </div>
              {:else if previewError}
                <div
                  class="flex items-start gap-2 px-5 py-6 text-type-sm font-body-md text-error"
                  role="alert"
                >
                  <span class="flex-1">{previewError}</span>
                  <button
                    type="button"
                    class="rounded-md border border-error-border bg-transparent px-2 py-1 text-type-xs font-label-sm-bold text-error hover:brightness-110"
                    onclick={() => selectedId && void loadPreview(selectedId)}
                  >
                    Retry
                  </button>
                </div>
              {:else if pageDiff}
                <div class="compare-chrome">
                  <p
                    class="compare-summary"
                    role="status"
                    aria-live="polite"
                    data-testid="page-history-diff-summary"
                  >
                    {diffSummary}
                    {#if compareFromSaved}
                      <span data-testid="page-history-compare-saved">
                        Comparing the last saved page.
                      </span>
                    {/if}
                  </p>
                  <div
                    class="pane-switch"
                    role="radiogroup"
                    aria-label="Diff layout"
                    tabindex="-1"
                    data-testid="page-history-layout"
                    onkeydown={onLayoutKeydown}
                  >
                    <button
                      type="button"
                      role="radio"
                      data-diff-layout="split"
                      data-testid="page-history-layout-split"
                      aria-checked={layout === 'split'}
                      tabindex={layout === 'split' ? 0 : -1}
                      class="pane-switch-btn"
                      class:active={layout === 'split'}
                      onclick={() => chooseLayout('split')}
                    >
                      Split
                    </button>
                    <button
                      type="button"
                      role="radio"
                      data-diff-layout="unified"
                      data-testid="page-history-layout-unified"
                      aria-checked={layout === 'unified'}
                      tabindex={layout === 'unified' ? 0 : -1}
                      class="pane-switch-btn"
                      class:active={layout === 'unified'}
                      onclick={() => chooseLayout('unified')}
                    >
                      Unified
                    </button>
                  </div>
                </div>
                <!-- Keyboard-scrollable region: role=region is correct; tabindex lets the pane receive focus. -->
                <!-- svelte-ignore a11y_no_noninteractive_tabindex -->
                <div
                  class="diff-scroll custom-scrollbar"
                  tabindex="0"
                  role="region"
                  aria-label="Page difference"
                  data-testid="page-history-compare"
                >
                  <div
                    class="diff-board"
                    class:is-split={layout === 'split'}
                    data-testid="page-history-diff"
                  >
                    {#if pageDiff.tooLarge}
                      <p
                        class="px-1 py-4 text-type-sm font-body-md text-text-muted"
                      >
                        This comparison is too large to show. Use Preview
                        instead.
                      </p>
                    {:else if layout === 'split'}
                      <div class="diff-col-head old">Version</div>
                      <div class="diff-col-head new">Current page</div>
                    {:else}
                      <div class="diff-legend">
                        <span>Version</span>
                        <span class="diff-legend-sep" aria-hidden="true">·</span
                        >
                        <span>Current page</span>
                      </div>
                    {/if}
                    {#each pageDiff.hunks as hunk, hunkIndex (hunkIndex)}
                      {#if hunkCollapsed(hunk, hunkIndex)}
                        <button
                          type="button"
                          class="diff-expand"
                          onclick={() => expandHunk(hunkIndex)}
                        >
                          {hunk.hiddenLines ?? 0} unchanged
                          {hunk.hiddenLines === 1 ? 'line' : 'lines'}
                        </button>
                      {:else}
                        {@const oldLines = sideLines(hunk, 'previous')}
                        {@const newLines = sideLines(hunk, 'current')}
                        <div
                          class="diff-side old"
                          class:is-empty={oldLines.length === 0}
                          class:is-dup={hunk.kind === 'equal'}
                          data-kind={hunk.kind === 'add'
                            ? 'empty'
                            : hunk.kind === 'equal'
                              ? 'equal'
                              : 'remove'}
                        >
                          {#each oldLines as line, lineIndex (`o-${hunkIndex}-${lineIndex}`)}
                            <div
                              class="diff-line"
                              class:remove={hunk.kind !== 'equal'}
                            >
                              {#if hunk.kind !== 'equal'}
                                <span class="sr-only">Removed</span>
                                <span class="diff-gutter" aria-hidden="true"
                                  >−</span
                                >
                              {:else}
                                <span class="diff-gutter" aria-hidden="true"
                                ></span>
                              {/if}
                              <span class="diff-text">
                                {#if line.parts}
                                  {#each line.parts as part, partIndex (`op-${lineIndex}-${partIndex}`)}
                                    {#if part.kind === 'remove'}
                                      <span class="word-remove"
                                        >{part.text}</span
                                      >
                                    {:else}
                                      {part.text}
                                    {/if}
                                  {/each}
                                {:else}
                                  {line.text}
                                {/if}
                              </span>
                            </div>
                          {/each}
                        </div>
                        <div
                          class="diff-side new"
                          class:is-empty={newLines.length === 0}
                          class:is-dup={hunk.kind === 'equal'}
                          aria-hidden={layout === 'split' &&
                          hunk.kind === 'equal'
                            ? true
                            : undefined}
                          data-kind={hunk.kind === 'remove'
                            ? 'empty'
                            : hunk.kind === 'equal'
                              ? 'equal'
                              : 'add'}
                        >
                          {#each newLines as line, lineIndex (`n-${hunkIndex}-${lineIndex}`)}
                            <div
                              class="diff-line"
                              class:add={hunk.kind !== 'equal'}
                            >
                              {#if hunk.kind !== 'equal'}
                                <span class="sr-only">Added</span>
                                <span class="diff-gutter" aria-hidden="true"
                                  >+</span
                                >
                              {:else}
                                <span class="diff-gutter" aria-hidden="true"
                                ></span>
                              {/if}
                              <span class="diff-text">
                                {#if line.parts}
                                  {#each line.parts as part, partIndex (`np-${lineIndex}-${partIndex}`)}
                                    {#if part.kind === 'add'}
                                      <span class="word-add">{part.text}</span>
                                    {:else}
                                      {part.text}
                                    {/if}
                                  {/each}
                                {:else}
                                  {line.text}
                                {/if}
                              </span>
                            </div>
                          {/each}
                        </div>
                      {/if}
                    {/each}
                  </div>
                </div>
              {/if}
            {:else if previewLoading}
              <div
                class="flex items-center gap-2 px-5 py-6 text-type-sm font-body-md text-text-muted"
                role="status"
              >
                <span
                  class="material-symbols-outlined animate-spin text-icon-lg text-accent-primary-start"
                  aria-hidden="true">sync</span
                >
                Loading preview…
              </div>
            {:else if previewError}
              <div
                class="flex items-start gap-2 px-5 py-6 text-type-sm font-body-md text-error"
                role="alert"
              >
                <span class="flex-1">{previewError}</span>
                <button
                  type="button"
                  class="rounded-md border border-error-border bg-transparent px-2 py-1 text-type-xs font-label-sm-bold text-error hover:brightness-110"
                  onclick={() => selectedId && void loadPreview(selectedId)}
                >
                  Retry
                </button>
              </div>
            {:else}
              <!-- Keyboard-scrollable region: role=region is correct; tabindex lets the pane receive focus. -->
              <!-- svelte-ignore a11y_no_noninteractive_tabindex -->
              <pre
                class="preview-body custom-scrollbar"
                tabindex="0"
                role="region"
                aria-label="Version preview body"
                data-testid="page-history-preview-body">{preview}</pre>
            {/if}
          {:else if !listLoading && versions.length > 0}
            <p class="px-5 py-6 text-type-sm font-body-md text-text-muted">
              Select a version to preview.
            </p>
          {/if}
        </section>
      {/if}
    </div>
  </div>
</div>

{#if restoreTarget}
  <ConfirmDialog
    title={deleted ? 'Restore this deleted page?' : 'Restore this version?'}
    message={deleted
      ? restoreAs
        ? `Recreate this page as ${formatLocatorPath(destNotebook.trim(), destSection.trim(), destPage.trim())} from the version from ${formatTimestamp(restoreTarget.timestamp)}?`
        : `Recreate ${formatLocatorPath(notebook, section, page)} from the version from ${formatTimestamp(restoreTarget.timestamp)}?`
      : `Replace this page with the version from ${formatTimestamp(
          restoreTarget.timestamp
        )}? A snapshot of the current page will be kept.`}
    confirmLabel={restoring ? 'Restoring…' : 'Restore'}
    cancelLabel="Cancel"
    destructive
    busy={restoring}
    dataTestId="page-history-confirm"
    onConfirm={() => void confirmRestore()}
    onCancel={cancelRestore}
  />
{/if}

<style>
  .dialog-surface {
    max-width: 56rem;
    transition: max-width 200ms cubic-bezier(0.16, 1, 0.3, 1);
  }

  .dialog-surface.is-comparing {
    max-width: 72rem;
  }

  .dialog-surface:focus-visible {
    outline: 2px solid var(--color-border-focus);
    outline-offset: 2px;
  }

  .history-split {
    display: grid;
    grid-template-columns: minmax(15rem, 18rem) minmax(0, 1fr);
    min-height: 0;
  }

  .history-list {
    overflow-y: auto;
    border-right: 1px solid var(--color-surface-modal-border);
    background: color-mix(in srgb, var(--color-surface-panel) 35%, transparent);
  }

  .version-row {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: 4px;
    width: 100%;
    padding: 10px 16px;
    border: none;
    border-left: 2px solid transparent;
    background: transparent;
    color: inherit;
    text-align: left;
    cursor: pointer;
    transition:
      background 120ms ease,
      border-color 120ms ease;
  }

  .version-row:hover {
    background: var(--color-hover);
  }

  .version-row.selected {
    background: color-mix(
      in srgb,
      var(--color-accent-primary-start) 12%,
      transparent
    );
    border-left-color: var(--color-accent-primary-start);
  }

  .version-row:focus-visible {
    outline: 2px solid var(--color-accent-primary-start);
    outline-offset: -2px;
  }

  .version-time {
    font-size: var(--text-type-sm);
    font-weight: 600;
    color: var(--color-text-primary);
  }

  .version-meta {
    display: flex;
    align-items: center;
    gap: 8px;
  }

  .source-badge {
    display: inline-flex;
    align-items: center;
    padding: 1px 6px;
    border-radius: 999px;
    border: 1px solid var(--color-surface-panel-border);
    background: color-mix(in srgb, var(--color-surface-card) 70%, transparent);
    color: var(--color-text-muted);
    font-size: var(--text-type-2xs);
    font-weight: 700;
    letter-spacing: 0.04em;
    text-transform: uppercase;
  }

  .source-badge.source-restore {
    border-color: color-mix(
      in srgb,
      var(--color-accent-primary-start) 45%,
      var(--color-surface-panel-border)
    );
    color: var(--color-accent-primary-start);
  }

  .source-badge.source-mcp,
  .source-badge.source-plugin {
    border-color: color-mix(
      in srgb,
      var(--color-accent-secondary-start) 45%,
      var(--color-surface-panel-border)
    );
    color: var(--color-accent-secondary-start);
  }

  .version-bytes {
    color: var(--color-text-muted);
    font-size: var(--text-type-2xs);
  }

  .history-preview {
    display: flex;
    min-width: 0;
    min-height: 0;
    flex-direction: column;
    background: color-mix(
      in srgb,
      var(--color-surface-editor) 55%,
      transparent
    );
  }

  .restore-as-fields {
    display: grid;
    grid-template-columns: repeat(3, minmax(0, 1fr));
    gap: 8px;
  }

  .restore-as-field {
    display: flex;
    min-width: 0;
    flex-direction: column;
    gap: 4px;
  }

  .restore-as-field span {
    color: var(--color-text-muted);
    font-size: var(--text-type-2xs);
    font-weight: 700;
    letter-spacing: 0.04em;
    text-transform: uppercase;
  }

  .restore-as-field input {
    width: 100%;
    border: 1px solid var(--color-surface-panel-border);
    border-radius: 8px;
    background: var(--color-surface-panel);
    padding: 6px 10px;
    color: var(--color-text-primary);
    font-size: var(--text-type-sm);
    outline: none;
  }

  .restore-as-field input:focus-visible {
    border-color: var(--color-accent-primary-start);
  }

  .preview-toolbar {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 8px 12px;
    padding: 10px 16px;
    border-bottom: 1px solid var(--color-surface-modal-border);
  }

  .preview-toolbar-actions {
    display: flex;
    align-items: center;
    gap: 8px;
    flex-shrink: 0;
  }

  .pane-switch {
    display: flex;
    align-items: center;
    gap: 2px;
    padding: 2px;
    border: 1px solid var(--color-surface-modal-border);
    border-radius: 8px;
    background: color-mix(in srgb, var(--color-surface-panel) 45%, transparent);
  }

  .pane-switch-btn {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    min-height: 28px;
    padding: 0 10px;
    border: 1px solid transparent;
    border-radius: 6px;
    background: transparent;
    color: var(--color-text-muted);
    font-size: var(--text-type-xs);
    font-weight: 700;
    letter-spacing: 0.02em;
    cursor: pointer;
    transition:
      background 120ms ease,
      color 120ms ease,
      border-color 120ms ease;
  }

  .pane-switch-btn:hover:not(:disabled) {
    background: var(--color-hover);
    color: var(--color-text-primary);
  }

  .pane-switch-btn.active {
    border-color: color-mix(
      in srgb,
      var(--color-accent-primary-start) 30%,
      transparent
    );
    background: var(--color-accent-primary-glow);
    color: var(--color-accent-primary-start);
  }

  .pane-switch-btn:disabled {
    cursor: not-allowed;
    opacity: 0.5;
  }

  .pane-switch-btn:focus-visible {
    outline: 2px solid var(--color-accent-primary-start);
    outline-offset: 1px;
  }

  .preview-body {
    margin: 0;
    min-height: 0;
    flex: 1;
    overflow: auto;
    padding: 16px 20px 24px;
    white-space: pre-wrap;
    overflow-wrap: anywhere;
    font-family: var(--font-mono, 'JetBrains Mono', monospace);
    font-size: var(--text-type-sm);
    line-height: 1.55;
    color: var(--color-text-primary);
  }

  .compare-chrome {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 8px 12px;
    padding: 8px 16px;
    border-bottom: 1px solid var(--color-surface-modal-border);
  }

  .compare-summary {
    margin: 0;
    flex: 1;
    min-width: 0;
    font-size: var(--text-type-sm);
    font-weight: 600;
    color: var(--color-text-primary);
  }

  .diff-scroll {
    min-height: 0;
    flex: 1;
    overflow: auto;
  }

  .diff-scroll:focus-visible {
    outline: 2px solid var(--color-border-focus);
    outline-offset: -2px;
  }

  .diff-board {
    display: flex;
    flex-direction: column;
    min-width: 0;
    font-family: var(--font-mono, 'JetBrains Mono', monospace);
    font-size: var(--text-type-sm);
    line-height: 1.55;
    color: var(--color-text-primary);
  }

  .diff-board.is-split {
    display: grid;
    grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
    align-items: stretch;
  }

  .diff-col-head,
  .diff-legend {
    position: sticky;
    top: 0;
    z-index: 1;
    padding: 8px 16px;
    background: color-mix(
      in srgb,
      var(--color-surface-editor) 92%,
      var(--color-surface-modal)
    );
    border-bottom: 1px solid var(--color-surface-modal-border);
    color: var(--color-text-muted);
    font-size: var(--text-type-2xs);
    font-weight: 700;
    letter-spacing: 0.04em;
    text-transform: uppercase;
  }

  .diff-board.is-split .diff-col-head.old {
    border-right: 1px solid var(--color-surface-modal-border);
  }

  .diff-legend {
    display: flex;
    align-items: center;
    gap: 8px;
  }

  .diff-legend-sep {
    color: var(--color-text-disabled);
  }

  .diff-side {
    min-width: 0;
  }

  .diff-board.is-split .diff-side.old {
    border-right: 1px solid var(--color-surface-modal-border);
  }

  .diff-board:not(.is-split) .diff-side.is-empty,
  .diff-board:not(.is-split) .diff-side.new.is-dup {
    display: none;
  }

  .diff-side.is-empty {
    min-height: 1.55em;
    background: color-mix(in srgb, var(--color-surface-panel) 28%, transparent);
  }

  .diff-line {
    display: grid;
    grid-template-columns: 1.25rem minmax(0, 1fr);
    column-gap: 8px;
    padding: 0 12px 0 8px;
    white-space: pre-wrap;
    overflow-wrap: anywhere;
  }

  .diff-line.remove {
    background: color-mix(in srgb, var(--color-status-danger) 10%, transparent);
  }

  .diff-line.add {
    background: color-mix(
      in srgb,
      var(--color-status-success) 10%,
      transparent
    );
  }

  .diff-gutter {
    user-select: none;
    text-align: center;
    font-weight: 700;
  }

  .diff-line.remove .diff-gutter {
    color: var(--color-status-danger);
  }

  .diff-line.add .diff-gutter {
    color: var(--color-status-success);
  }

  .word-add {
    border-radius: 2px;
    background: color-mix(
      in srgb,
      var(--color-status-success) 16%,
      transparent
    );
  }

  .word-remove {
    border-radius: 2px;
    background: color-mix(in srgb, var(--color-status-danger) 16%, transparent);
  }

  .diff-expand {
    width: 100%;
    padding: 6px 16px;
    border: none;
    border-top: 1px dashed var(--color-surface-modal-border);
    border-bottom: 1px dashed var(--color-surface-modal-border);
    background: color-mix(in srgb, var(--color-surface-panel) 40%, transparent);
    color: var(--color-text-muted);
    font-family: var(--font-mono, 'JetBrains Mono', monospace);
    font-size: var(--text-type-xs);
    font-weight: 600;
    letter-spacing: 0.02em;
    cursor: pointer;
    text-align: center;
  }

  .diff-board.is-split .diff-expand {
    grid-column: 1 / -1;
  }

  .diff-expand:hover {
    background: var(--color-hover);
    color: var(--color-text-primary);
  }

  .diff-expand:focus-visible {
    outline: 2px solid var(--color-accent-primary-start);
    outline-offset: -2px;
  }

  @media (max-width: 720px) {
    .history-split {
      grid-template-columns: 1fr;
      grid-template-rows: minmax(10rem, 38%) minmax(0, 1fr);
    }

    .history-list {
      border-right: none;
      border-bottom: 1px solid var(--color-surface-modal-border);
    }

    .restore-as-fields {
      grid-template-columns: 1fr;
    }
  }
</style>
