<script lang="ts">
  import type { Editor } from '@tiptap/core'
  import {
    getMatchCount,
    getActiveMatchIndex,
    clearSearch
  } from '../../lib/editor/search/searchExtension'
  import { findBarState } from '../../lib/editor/search/findBarState.svelte'
  import {
    findSourceMatches,
    expandReplace,
    replaceAllSource,
    type SourceMatch,
    type SourceSearchTarget
  } from '../../lib/editor/search/sourceSearch'

  // Dual backend: TipTap (Edit) or SourceSearchTarget (Source). Exactly one
  // should be set by VirtualScrollContainer for the active view mode (#884).
  let {
    editor,
    sourceTarget,
    onClose
  }: {
    editor?: Editor
    sourceTarget?: SourceSearchTarget | null
    onClose: () => void
  } = $props()

  const hasBackend = $derived(!!editor || !!sourceTarget)

  // User inputs.
  let query = $state('')
  let replaceValue = $state('')
  let caseSensitive = $state(false)
  let wholeWord = $state(false)
  let regexp = $state(false)

  // Projections of match state, refreshed on query change / doc edit / nav.
  let matchCount = $state(0)
  let activeIndex = $state(-1)
  let lastReplaceMessage = $state('')
  let inputEl = $state<HTMLInputElement | null>(null)

  // Source-mode match list (TipTap keeps decorations inside the editor).
  let sourceMatches = $state<SourceMatch[]>([])

  function sourceOpts() {
    return { caseSensitive, wholeWord, regexp }
  }

  function refreshCounts(): void {
    if (sourceTarget) {
      matchCount = sourceMatches.length
      // activeIndex is owned by source navigation; clamp if list shrank.
      if (matchCount === 0) activeIndex = -1
      else if (activeIndex >= matchCount) activeIndex = matchCount - 1
      return
    }
    if (!editor || !editor.isEditable) return
    matchCount = getMatchCount(editor)
    activeIndex = getActiveMatchIndex(editor)
  }

  function selectSourceMatch(index: number, matches = sourceMatches): void {
    if (!sourceTarget || matches.length === 0) return
    const i = ((index % matches.length) + matches.length) % matches.length
    activeIndex = i
    const m = matches[i]
    sourceTarget.setSelection(m.from, m.to)
  }

  function applySourceQuery(preferCaret = true): void {
    if (!sourceTarget) return
    const text = sourceTarget.getText()
    // Local list first — reading $state after writing it in the same effect
    // would re-subscribe and loop (effect_update_depth_exceeded).
    const matches = findSourceMatches(text, query, sourceOpts())
    sourceMatches = matches
    matchCount = matches.length
    if (!query || matchCount === 0) {
      activeIndex = -1
      return
    }
    if (preferCaret) {
      const caret = sourceTarget.getCaret()
      // Prefer first match at/after caret; wrap to 0.
      let idx = matches.findIndex((m) => m.from >= caret)
      if (idx < 0) idx = 0
      selectSourceMatch(idx, matches)
    } else if (activeIndex >= 0 && activeIndex < matchCount) {
      selectSourceMatch(activeIndex, matches)
    } else {
      selectSourceMatch(0, matches)
    }
  }

  function applyQuery(): void {
    if (sourceTarget) {
      applySourceQuery(true)
      return
    }
    if (!editor) return
    editor.commands.setSearchQuery({
      search: query,
      caseSensitive,
      wholeWord,
      regexp,
      replace: findBarState.replaceOpen ? replaceValue : ''
    })
    // Jump to the nearest match so the counter reflects a real position.
    if (query) editor.commands.findNextInPage()
    refreshCounts()
  }

  // Re-apply whenever the query string, replace text, or a toggle flips.
  $effect(() => {
    void query
    void replaceValue
    void caseSensitive
    void wholeWord
    void regexp
    void sourceTarget
    void editor
    applyQuery()
  })

  // Subscribe to editor updates so the counter stays accurate as the doc
  // changes (the user edits while the bar is open) or navigation moves the
  // selection. Cleaned up on destroy.
  $effect(() => {
    if (!editor) return
    const onUpdate = () => refreshCounts()
    editor.on('update', onUpdate)
    editor.on('selectionUpdate', onUpdate)
    return () => {
      editor.off('update', onUpdate)
      editor.off('selectionUpdate', onUpdate)
    }
  })

  // Source buffer edits while Find is open — re-scan without resetting caret
  // preference every keystroke beyond re-finding from current active.
  $effect(() => {
    if (!sourceTarget) return
    return sourceTarget.subscribe(() => {
      applySourceQuery(false)
    })
  })

  function handleKeydown(e: KeyboardEvent): void {
    if (e.key === 'Escape') {
      e.preventDefault()
      close()
    } else if (e.key === 'Enter' && e.altKey) {
      // Alt+Enter → Replace All when the replace row is open (#656).
      e.preventDefault()
      if (findBarState.replaceOpen) doReplaceAll()
    } else if (e.key === 'Enter') {
      e.preventDefault()
      if (!query) return
      if (sourceTarget) {
        if (e.shiftKey) prev()
        else next()
        return
      }
      if (!editor) return
      if (e.shiftKey) editor.commands.findPrevInPage()
      else editor.commands.findNextInPage()
      refreshCounts()
    }
  }

  function next(): void {
    if (sourceTarget) {
      if (sourceMatches.length === 0) return
      selectSourceMatch(activeIndex < 0 ? 0 : activeIndex + 1)
      refreshCounts()
      return
    }
    editor?.commands.findNextInPage()
    refreshCounts()
  }
  function prev(): void {
    if (sourceTarget) {
      if (sourceMatches.length === 0) return
      selectSourceMatch(
        activeIndex < 0 ? sourceMatches.length - 1 : activeIndex - 1
      )
      refreshCounts()
      return
    }
    editor?.commands.findPrevInPage()
    refreshCounts()
  }
  function doReplace(): void {
    if (matchCount === 0) return
    if (sourceTarget) {
      if (activeIndex < 0 || activeIndex >= sourceMatches.length) return
      const m = sourceMatches[activeIndex]
      const text = sourceTarget.getText()
      const matchText = text.slice(m.from, m.to)
      let groups: string[] = []
      if (regexp) {
        try {
          const flags = caseSensitive ? '' : 'i'
          const body = wholeWord ? `\\b(?:${query})\\b` : query
          const re = new RegExp(body, flags)
          const exec = re.exec(matchText)
          if (exec) {
            groups = []
            for (let i = 1; i < exec.length; i++) groups.push(exec[i] ?? '')
          }
        } catch {
          groups = []
        }
      }
      const rep = expandReplace(replaceValue, matchText, groups)
      sourceTarget.replaceRange(m.from, m.to, rep)
      // Re-scan; stay near the replacement point.
      applySourceQuery(true)
      inputEl?.focus()
      return
    }
    if (!editor) return
    editor.commands.replaceNextInPage()
    refreshCounts()
    inputEl?.focus()
  }
  function doReplaceAll(): void {
    if (matchCount === 0) return
    if (sourceTarget) {
      const before = matchCount
      const { text, count } = replaceAllSource(
        sourceTarget.getText(),
        query,
        replaceValue,
        sourceOpts()
      )
      if (count > 0) sourceTarget.setText(text)
      applySourceQuery(true)
      lastReplaceMessage = `Replaced ${before} match${before === 1 ? '' : 'es'}`
      window.setTimeout(() => {
        lastReplaceMessage = ''
      }, 2500)
      inputEl?.focus()
      return
    }
    if (!editor) return
    const before = matchCount
    editor.commands.replaceAllInPage()
    refreshCounts()
    lastReplaceMessage = `Replaced ${before} match${before === 1 ? '' : 'es'}`
    window.setTimeout(() => {
      lastReplaceMessage = ''
    }, 2500)
    inputEl?.focus()
  }

  function close(): void {
    if (editor) clearSearch(editor)
    onClose()
  }

  function focusInput(): void {
    // Select-all on (re)open so re-typing replaces the previous query.
    inputEl?.focus()
    inputEl?.select()
  }

  // Focus when mounted with a backend.
  $effect(() => {
    if (hasBackend) focusInput()
  })

  const status = $derived(
    !query
      ? ''
      : matchCount === 0
        ? 'No results'
        : `${activeIndex + 1} of ${matchCount}`
  )
  const noResults = $derived(!!query && matchCount === 0)
</script>

<div
  class="find-bar"
  role="toolbar"
  aria-label={findBarState.replaceOpen ? 'Find and replace' : 'Find'}
>
  <div class="row">
    <input
      bind:this={inputEl}
      type="search"
      aria-label="Find"
      aria-keyshortcuts="Ctrl+F"
      aria-describedby="find-status"
      placeholder="Find"
      autocomplete="off"
      spellcheck="false"
      bind:value={query}
      onkeydown={handleKeydown}
      class="find-input"
      class:no-results={noResults}
    />
    <span
      id="find-status"
      class="find-status"
      aria-live="polite"
      aria-atomic="true"
    >
      {status}
    </span>
    <div class="find-toggles">
      <button
        type="button"
        class="toggle"
        class:on={caseSensitive}
        aria-pressed={caseSensitive}
        aria-label="Case sensitive (Alt+C)"
        title="Case sensitive (Alt+C)"
        onclick={() => (caseSensitive = !caseSensitive)}>Aa</button
      >
      <button
        type="button"
        class="toggle"
        class:on={wholeWord}
        aria-pressed={wholeWord}
        aria-label="Whole word (Alt+W)"
        title="Whole word (Alt+W)"
        onclick={() => (wholeWord = !wholeWord)}>ab</button
      >
      <button
        type="button"
        class="toggle"
        class:on={regexp}
        aria-pressed={regexp}
        aria-label="Regular expression (Alt+R)"
        title="Regular expression (Alt+R)"
        onclick={() => (regexp = !regexp)}>.*</button
      >
    </div>
    <div class="find-nav">
      <button
        type="button"
        class="nav-btn"
        aria-label="Previous match (Shift+Enter)"
        aria-keyshortcuts="Shift+Enter"
        title="Previous match"
        disabled={!hasBackend || matchCount === 0}
        onclick={prev}>↑</button
      >
      <button
        type="button"
        class="nav-btn"
        aria-label="Next match (Enter)"
        aria-keyshortcuts="Enter"
        title="Next match"
        disabled={!hasBackend || matchCount === 0}
        onclick={next}>↓</button
      >
    </div>
    <button
      type="button"
      class="close-btn"
      aria-label="Close find bar (Esc)"
      aria-keyshortcuts="Escape"
      title="Close"
      onclick={close}>✕</button
    >
  </div>
  {#if findBarState.replaceOpen}
    <div class="row replace-row">
      <input
        type="text"
        aria-label="Replace with"
        aria-keyshortcuts="Alt+Enter"
        placeholder="Replace"
        autocomplete="off"
        spellcheck="false"
        bind:value={replaceValue}
        class="find-input"
        onkeydown={handleKeydown}
      />
      <button
        type="button"
        class="action-btn"
        aria-label="Replace selected match"
        title="Replace this match"
        disabled={!hasBackend || matchCount === 0}
        onclick={doReplace}>Replace</button
      >
      <button
        type="button"
        class="action-btn"
        aria-label="Replace all matches"
        aria-keyshortcuts="Alt+Enter"
        title="Replace all"
        disabled={!hasBackend || matchCount === 0}
        onclick={doReplaceAll}>All</button
      >
      {#if lastReplaceMessage}
        <span class="replace-message" aria-live="polite"
          >{lastReplaceMessage}</span
        >
      {/if}
    </div>
  {/if}
</div>

<svelte:window
  onkeydown={(e) => {
    // Toggle shortcuts active while the find bar is focused.
    if (e.altKey && (e.key === 'c' || e.key === 'C')) {
      e.preventDefault()
      caseSensitive = !caseSensitive
    } else if (e.altKey && (e.key === 'w' || e.key === 'W')) {
      e.preventDefault()
      wholeWord = !wholeWord
    } else if (e.altKey && (e.key === 'r' || e.key === 'R') && !e.shiftKey) {
      // Alt+R toggles regex; Alt+Enter replace-all is handled only on the
      // find/replace inputs (handleKeydown) so it cannot double-fire.
      e.preventDefault()
      regexp = !regexp
    }
  }}
/>

<style>
  .find-bar {
    position: absolute;
    top: 0.5rem;
    right: 1rem;
    z-index: 50;
    display: flex;
    flex-direction: column;
    gap: 0.25rem;
    padding: 0.375rem 0.5rem;
    background: var(--color-surface-popover);
    border: 1px solid var(--color-surface-popover-border);
    border-radius: 8px;
    box-shadow: var(--shadow-md, 0 4px 12px rgba(0, 0, 0, 0.35));
    font-size: 13px;
  }
  .row {
    display: flex;
    align-items: center;
    gap: 0.375rem;
  }
  .replace-row {
    border-top: 1px solid var(--color-surface-popover-border);
    padding-top: 0.25rem;
  }
  .find-input {
    width: 200px;
    padding: 0.25rem 0.5rem;
    background: var(--color-surface-popover);
    color: var(--color-text-primary);
    border: 1px solid var(--color-surface-popover-border);
    border-radius: 4px;
    font: inherit;
  }
  .find-input.no-results {
    border-color: var(--color-status-danger);
  }
  .find-input:focus {
    outline: none;
    border-color: var(--color-accent-primary-start);
  }
  .find-status {
    min-width: 70px;
    color: var(--color-text-muted);
    font-size: 12px;
    text-align: center;
  }
  .find-toggles,
  .find-nav {
    display: flex;
    gap: 0.125rem;
  }
  .toggle,
  .nav-btn,
  .close-btn {
    min-width: 26px;
    height: 26px;
    padding: 0 0.375rem;
    background: transparent;
    color: var(--color-text-muted);
    border: 1px solid transparent;
    border-radius: 4px;
    cursor: pointer;
    font: inherit;
    line-height: 1;
  }
  .toggle:hover,
  .nav-btn:hover,
  .close-btn:hover {
    background: var(--color-hover);
    color: var(--color-text-primary);
  }
  .toggle.on {
    background: var(--color-accent-primary-start);
    color: var(--color-text-on-accent);
    border-color: var(--color-accent-primary-start);
  }
  .nav-btn:disabled,
  .close-btn:disabled {
    opacity: 0.4;
    cursor: default;
  }
  .toggle:focus-visible,
  .nav-btn:focus-visible,
  .close-btn:focus-visible,
  .action-btn:focus-visible {
    outline: 2px solid var(--color-accent-primary-start);
    outline-offset: 1px;
  }
  .action-btn {
    padding: 0.25rem 0.625rem;
    background: transparent;
    color: var(--color-text-primary);
    border: 1px solid var(--color-surface-popover-border);
    border-radius: 4px;
    cursor: pointer;
    font: inherit;
    line-height: 1;
  }
  .action-btn:hover:not(:disabled) {
    background: var(--color-hover);
  }
  .action-btn:disabled {
    opacity: 0.4;
    cursor: default;
  }
  .replace-message {
    color: var(--color-text-muted);
    font-size: 12px;
  }
</style>
