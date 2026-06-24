<script lang="ts">
  import { NodeViewWrapper, NodeViewContent } from 'svelte-tiptap'
  import type { NodeViewProps } from '@tiptap/core'

  let { node, editor }: NodeViewProps = $props()
  let lang = $derived(node.attrs.lang || '')
  let isFocused = $derived(editor.isFocused)

  function copyContent(): void {
    const text = node.textContent || ''
    navigator.clipboard.writeText(text).catch(() => {
      // Clipboard API may fail in restricted contexts — silently ignore.
    })
  }
</script>

<NodeViewWrapper
  class="code-block-wrapper my-3 rounded-lg overflow-hidden border border-border"
  data-type="code-block"
  role="region"
  aria-label={lang ? `Code block (${lang})` : 'Code block'}
>
  <div
    class="flex items-center justify-between px-4 py-1.5 bg-bg-interface text-text-muted text-xs border-b border-border"
  >
    <span class="font-mono uppercase tracking-wide" aria-hidden="true">
      {lang || 'code'}
    </span>
    <button
      class="material-symbols-outlined text-[16px] hover:text-text transition-colors cursor-pointer select-none"
      class:opacity-0={!isFocused}
      onclick={copyContent}
      aria-label="Copy code"
    >
      content_copy
    </button>
  </div>
  <pre class="px-4 py-3 overflow-x-auto bg-bg text-sm leading-relaxed">
    <NodeViewContent class="font-mono whitespace-pre" />
  </pre>
</NodeViewWrapper>
