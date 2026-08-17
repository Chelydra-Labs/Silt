<script lang="ts">
  import { onMount, tick } from 'svelte'
  import {
    ListPageVersions,
    GetPageVersion,
    RestorePageVersion
  } from '../../../bindings/silt/app.js'
  import { trapFocus } from '../../lib/focusTrap'
  import { settings } from '../../settings/store.svelte'
  import { editorKey, getEditor } from '../../lib/editor/editorRegistry.svelte'
  import ConfirmDialog from '../ConfirmDialog.svelte'

  interface PageVersionRow {
    id: string
    timestamp: string
    source: string
    bytes: number
  }

  interface Props {
    notebook: string
    section: string
    page: string
    onClose: () => void
  }

  let { notebook, section, page, onClose }: Props = $props()

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

  let versioningEnabled = $derived(
    settings.config?.editor?.auto_versioning_enabled === true
  )
  let selected = $derived(versions.find((v) => v.id === selectedId) ?? null)

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
      listError = e instanceof Error ? e.message : String(e)
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
      previewError = e instanceof Error ? e.message : String(e)
    } finally {
      if (gen === previewGen) previewLoading = false
    }
  }

  function selectVersion(id: string): void {
    if (selectedId === id) return
    selectedId = id
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

  function scrollSelectedIntoView(): void {
    queueMicrotask(() => {
      if (!listRef || !selectedId) return
      const el = listRef.querySelector(`[data-version-id="${selectedId}"]`)
      el?.scrollIntoView?.({ block: 'nearest' })
    })
  }

  function canRestoreSelected(): boolean {
    return (
      !!selected &&
      selected.id === previewedId &&
      !previewLoading &&
      !previewError &&
      !restoring
    )
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
      await RestorePageVersion(notebook, section, page, target.id)
      restoreTarget = null
      statusMessage = `Restored version from ${formatTimestamp(target.timestamp)}. A snapshot of the previous page was kept.`
      await loadVersions(target.id)
    } catch (e) {
      restoreError = e instanceof Error ? e.message : String(e)
    } finally {
      restoring = false
    }
  }

  function cancelRestore(): void {
    if (restoring) return
    restoreTarget = null
  }

  function openEditorSettings(): void {
    onClose()
    window.dispatchEvent(new CustomEvent('open-settings', { detail: 'editor' }))
  }

  function handleKeydown(e: KeyboardEvent): void {
    // ConfirmDialog owns Esc / Tab while it is open.
    if (restoreTarget) return
    if (e.key === 'Escape') {
      e.preventDefault()
      e.stopPropagation()
      onClose()
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
      const target = e.target as HTMLElement | null
      if (target?.closest('[data-page-history-restore]')) return
      if (versions.length === 0) return
      e.preventDefault()
      if (selectedId) void loadPreview(selectedId)
    }
  }

  onMount(() => {
    previouslyFocused = document.activeElement as HTMLElement | null
    window.addEventListener('keydown', handleKeydown, true)
    void tick().then(() => dialogRef?.focus())
    void loadVersions()
    return () => {
      window.removeEventListener('keydown', handleKeydown, true)
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
    aria-label="Close page history"
    class="absolute inset-0 cursor-default border-none bg-transparent p-0"
    onclick={onClose}
  ></button>

  <div
    bind:this={dialogRef}
    role="dialog"
    aria-modal="true"
    aria-labelledby="page-history-title"
    aria-describedby="page-history-path page-history-honesty"
    tabindex="-1"
    data-testid="page-history-modal"
    class="dialog-surface relative flex h-[min(36rem,calc(100vh-4rem))] w-full max-w-4xl flex-col overflow-hidden rounded-xl border border-surface-modal-border glass-palette glass-palette-strong shadow-2xl"
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
          Page history
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
      <button
        type="button"
        aria-label="Close page history"
        onclick={onClose}
        class="flex h-8 w-8 items-center justify-center rounded-lg border-none bg-transparent text-text-muted transition-colors hover:bg-hover hover:text-text-primary"
      >
        <span class="material-symbols-outlined text-icon-lg" aria-hidden="true"
          >close</span
        >
      </button>
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
        class="mx-5 mt-4 flex items-start gap-2 rounded-lg border border-error-border bg-error-bg px-3 py-2 text-type-sm font-body-md text-error"
        role="alert"
      >
        <span class="material-symbols-outlined text-icon-lg" aria-hidden="true"
          >error</span
        >
        <span class="flex-1">{restoreError}</span>
      </div>
    {/if}

    <div class="history-split min-h-0 flex-1">
      <div
        bind:this={listRef}
        class="history-list custom-scrollbar"
        role="listbox"
        aria-label="Versions"
        aria-busy={listLoading || undefined}
        tabindex="0"
        aria-activedescendant={versions.length > 0 && selectedId
          ? `page-history-${selectedId}`
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
        {:else if versions.length === 0}
          <div
            class="flex flex-col items-start gap-2 px-4 py-6"
            role="status"
            data-testid="page-history-empty"
          >
            <p class="text-type-md font-body-md text-text-primary">
              No versions yet
            </p>
            {#if !versioningEnabled}
              <p class="text-type-sm font-body-md text-text-muted">
                Turn on Capture page history in Settings → Editor to start
                saving snapshots.
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
          </div>
        {:else}
          {#each versions as version, idx (version.id)}
            <button
              type="button"
              role="option"
              id={`page-history-${version.id}`}
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
                  <span class="version-bytes">{formatBytes(version.bytes)}</span
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

      {#if versions.length > 0 || listLoading || listError}
        <section
          class="history-preview"
          aria-label="Version preview"
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
            {#if previewLoading}
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
              <pre
                class="preview-body custom-scrollbar"
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
    title="Restore this version?"
    message="Replace this page with the version from {formatTimestamp(
      restoreTarget.timestamp
    )}? A snapshot of the current page will be kept."
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

  .preview-toolbar {
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 10px 16px;
    border-bottom: 1px solid var(--color-surface-modal-border);
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

  @media (max-width: 720px) {
    .history-split {
      grid-template-columns: 1fr;
      grid-template-rows: minmax(10rem, 38%) minmax(0, 1fr);
    }

    .history-list {
      border-right: none;
      border-bottom: 1px solid var(--color-surface-modal-border);
    }
  }
</style>
