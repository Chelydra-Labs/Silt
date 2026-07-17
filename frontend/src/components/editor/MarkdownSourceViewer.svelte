<script lang="ts">
  import { onDestroy, onMount } from 'svelte'
  import type { ParsedBlock } from '../../lib/editor/types'
  import { themeState } from '../../theme/store.svelte'
  import {
    highlightMarkdown,
    tokensToShikiTheme
  } from '../../lib/editor/useMarkdownHighlighter'
  import { savePageMarkdown } from '../../lib/editor/savePageMarkdown'
  import {
    AcquireFocusLock,
    ReleaseFocusLock,
    RefreshFocusLock
  } from '../../../bindings/silt/app.js'

  // MarkdownSourceViewer — editable Source mode (#660) with optional read-only
  // Shiki highlight (#171/#194). Reconstructs markdown from blocks as the
  // initial buffer; debounced save writes via SavePageMarkdown.

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
  let conflictStatus = $state<string | null>(null)
  let saveTimer: ReturnType<typeof setTimeout> | null = null
  let heartbeatInterval: ReturnType<typeof setInterval> | null = null
  let hasFocusLock = false
  /** Fingerprint of last applied external blocks (ids + raw_text). */
  let lastBlocksKey = $state<string | null>(null)

  let lineCount = $derived(Math.max(1, buffer.split('\n').length))

  function blocksKey(source: ParsedBlock[]): string {
    return source
      .map((b) => `${b.id}\0${b.raw_text ?? b.clean_text ?? ''}`)
      .join('\n')
  }

  // External blocks prop: seed / reset when clean; keep local buffer when dirty.
  $effect(() => {
    const next = blocks
    const key = blocksKey(next)
    if (lastBlocksKey === null) {
      buffer = reconstructMarkdown(next)
      lastBlocksKey = key
      return
    }
    if (key === lastBlocksKey) return
    if (!dirty) {
      buffer = reconstructMarkdown(next)
      lastBlocksKey = key
      conflictStatus = null
      saveError = null
    } else {
      conflictStatus =
        'External change detected — save or reload to pick up remote edits.'
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
      let html: string | null = null
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
    if (!editable || !notebook || !page) return
    if (saveTimer) clearTimeout(saveTimer)
    saveTimer = setTimeout(() => {
      saveTimer = null
      void flushSave()
    }, 500)
  }

  async function flushSave(): Promise<void> {
    if (!editable || !notebook || !page || !dirty) return
    const md = buffer
    try {
      const saved = await savePageMarkdown(notebook, section, page, md)
      // Only clear dirty if buffer still matches what we saved.
      if (buffer === md) {
        dirty = false
        conflictStatus = null
      }
      saveError = null
      onBlocksSaved?.(saved)
    } catch (e) {
      saveError = e instanceof Error ? e.message : String(e)
    }
  }

  function onInput(e: Event): void {
    const el = e.currentTarget as HTMLTextAreaElement
    buffer = el.value
    dirty = true
    saveError = null
    scheduleSave()
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
    if (saveTimer) {
      clearTimeout(saveTimer)
      // Best-effort flush on unmount when dirty.
      if (dirty && notebook && page) {
        void savePageMarkdown(notebook, section, page, buffer)
          .then((saved) => onBlocksSaved?.(saved))
          .catch(() => {})
      }
    }
    void releaseLock()
  })
</script>

<div class="source-viewer">
  <div class="source-header">
    <span class="file-path" title={filePath}>{filePath}</span>
    <div class="header-actions">
      {#if saveError}
        <span
          class="save-status save-status--err"
          role="alert"
          aria-live="assertive">Save failed: {saveError}</span
        >
      {:else if conflictStatus}
        <span class="save-status" role="status" aria-live="polite"
          >{conflictStatus}</span
        >
      {:else if dirty}
        <span class="save-status" role="status" aria-live="polite"
          >Unsaved…</span
        >
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
      {#each Array(lineCount) as _, i}
        <span class="line-num">{i + 1}</span>
      {/each}
    </div>
    {#if editable}
      <textarea
        class="source-code source-textarea"
        value={buffer}
        oninput={onInput}
        onfocus={() => {
          if (!hasFocusLock) void acquireLock()
        }}
        spellcheck="false"
        aria-label="Markdown source of {filePath}"
        rows={lineCount}></textarea>
    {:else}
      <pre
        class="source-code">{#if highlightedHtml}{@html highlightedHtml}{:else}{buffer}{/if}</pre>
    {/if}
  </div>
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

  .copy-btn {
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

  .copy-btn:hover {
    background: color-mix(
      in srgb,
      var(--color-accent-primary-start) 15%,
      transparent
    );
    color: var(--color-text-primary);
  }

  .copy-btn .material-symbols-outlined {
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

  .source-body {
    display: flex;
    overflow: auto;
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
    white-space: pre-wrap;
    word-break: break-word;
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
    overflow: hidden;
  }

  .source-textarea:focus {
    outline: 1px solid
      color-mix(in srgb, var(--color-accent-primary-start) 40%, transparent);
    outline-offset: -1px;
    border-radius: 2px;
  }
</style>
