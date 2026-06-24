<script lang="ts">
  import { NodeViewWrapper, NodeViewContent } from 'svelte-tiptap'
  import type { NodeViewProps } from '@tiptap/core'
  import { themeState } from '../../theme/store.svelte'
  import { isSystemDark } from '../../lib/systemTheme.svelte'
  import {
    highlightCode,
    COMMON_LANGUAGES,
    type ShikiTheme
  } from '../../lib/editor/useShiki'
  import { pushNotification } from '../../notifications/store.svelte'

  // Dual-layer code block (#189). The ProseMirror-managed contenteditable
  // (NodeViewContent) carries the raw text with a TRANSPARENT foreground, so
  // only the caret is visible there. A Shiki-highlighted `<pre>` sits behind
  // it at identical font metrics, supplying the coloured tokens. Both layers
  // share the editor's mono font / size / line-height / padding, so the
  // coloured tokens line up exactly with the (invisible) raw characters.
  let { node, updateAttributes, selected }: NodeViewProps = $props()

  let language = $derived((node.attrs.language as string) || '')
  // node.textContent reacts to transactions; it is the source the Shiki layer
  // mirrors. Falling back to '' keeps the highlighter happy on empty blocks.
  let code = $derived(node.textContent ?? '')

  let isDark = $derived(
    themeState.mode === 'dark' ||
      (themeState.mode === 'system' && isSystemDark())
  )
  let shikiTheme = $derived<ShikiTheme>(isDark ? 'github-dark' : 'github-light')

  let highlighted = $state('')
  let copyState = $state<'idle' | 'done' | 'error'>('idle')

  // Re-highlight (debounced) whenever the code, language, or theme changes.
  // Shiki is async (lazy grammar load); the cache in useShiki makes the common
  // re-render-after-keystroke case synchronous from the caller's view.
  let highlightTimer: ReturnType<typeof setTimeout> | null = null
  $effect(() => {
    const c = code
    const lang = language
    const theme = shikiTheme
    if (highlightTimer) clearTimeout(highlightTimer)
    highlightTimer = setTimeout(async () => {
      highlighted = await highlightCode(c, lang, theme)
    }, 60)
  })

  async function copyCode(): Promise<void> {
    try {
      await navigator.clipboard.writeText(code)
      copyState = 'done'
      setTimeout(() => (copyState = 'idle'), 1200)
    } catch {
      copyState = 'error'
      pushNotification({
        kind: 'error',
        message: 'Could not copy code to the clipboard.'
      })
    }
  }

  function onLanguageChange(e: Event): void {
    const value = (e.currentTarget as HTMLSelectElement).value
    updateAttributes({ language: value === 'plaintext' ? '' : value })
  }
</script>

<NodeViewWrapper
  class={`silt-code group relative my-1${selected ? ' selected' : ''}`}
  data-language={language || 'plaintext'}
  role="region"
  aria-label={language ? `${language} code block` : 'code block'}
>
  <div class="silt-code-bar">
    <select
      class="silt-code-lang"
      value={language || 'plaintext'}
      aria-label="Code language"
      onchange={onLanguageChange}
    >
      {#each COMMON_LANGUAGES as lang (lang)}
        <option value={lang || 'plaintext'}>
          {lang || 'plaintext'}
        </option>
      {/each}
    </select>
    <button
      type="button"
      class="silt-code-copy"
      onclick={copyCode}
      aria-label="Copy code"
    >
      <span class="material-symbols-outlined" aria-hidden="true">
        {copyState === 'done' ? 'check' : 'content_copy'}
      </span>
    </button>
  </div>
  <div class="silt-code-body">
    <!-- Shiki highlight layer (visible, non-interactive). -->
    <!-- svelte-ignore a11y_no_static_element_interactions -->
    <div class="silt-code-display" aria-hidden="true">
      {@html highlighted}
    </div>
    <!-- Editable layer (transparent text, visible caret). ProseMirror owns it. -->
    <NodeViewContent as="pre" class="silt-code-edit">
      <code></code>
    </NodeViewContent>
  </div>
</NodeViewWrapper>
