<script lang="ts">
  // MathNodeView — KaTeX renderer for inline ($...$) and block ($$...$$) math
  // (#191). One component serves both nodes; displayMode follows the node type.
  // KaTeX is lazy-loaded via useKatex (kept out of the main bundle). The raw
  // LaTeX is the accessible name (aria-label) and KaTeX's htmlAndMathml output
  // also exposes MathML to screen readers. Parse errors render inline in error
  // color (visible, not silent).
  import { NodeViewWrapper } from 'svelte-tiptap'
  import type { NodeViewProps } from '@tiptap/core'
  import { renderKatex } from '../../lib/editor/useKatex'

  let { node, updateAttributes }: NodeViewProps = $props()
  const latex = $derived((node.attrs.latex as string) || '')
  const displayMode = $derived(node.type.name === 'blockMathNode')

  let html = $state('')
  let error = $state<string | null>(null)

  // Re-render (async) whenever the latex or display mode changes. KaTeX loads
  // on first use; thereafter renders are effectively synchronous.
  $effect(() => {
    const l = latex
    const dm = displayMode
    let cancelled = false
    renderKatex(l, dm).then((res) => {
      if (cancelled) return
      html = res.html
      error = res.error
    })
    return () => {
      cancelled = true
    }
  })

  // Click opens a prompt pre-filled with the raw LaTeX — both an edit affordance
  // and a way to copy the source (#191). Cancel leaves the node unchanged.
  function editLatex(): void {
    const next = window.prompt('LaTeX:', latex)
    if (next !== null) updateAttributes({ latex: next })
  }
</script>

<NodeViewWrapper as={displayMode ? 'div' : 'span'}>
  {#if latex}
    <button
      type="button"
      class="silt-math"
      class:silt-math-block={displayMode}
      aria-label="Equation: {latex}. Activate to edit."
      onclick={editLatex}
    >
      {#if error}
        <span class="silt-math-err" role="alert">{error}</span>
      {:else}
        {@html html}
      {/if}
    </button>
  {:else}
    <button
      type="button"
      class="silt-math-empty"
      onclick={editLatex}
      aria-label="Add LaTeX equation"
    >
      Add LaTeX equation
    </button>
  {/if}
</NodeViewWrapper>

<style>
  /* The button provides interaction + a11y only; KaTeX supplies the visual +
     MathML semantics. Strip button chrome so it renders as bare inline math. */
  .silt-math {
    background: none;
    border: none;
    padding: 0;
    font: inherit;
    color: inherit;
    cursor: pointer;
    display: inline;
  }
  .silt-math-block {
    display: block;
  }
  .silt-math:focus-visible {
    outline: 2px solid var(--color-accent-primary-start, #4f7cff);
    outline-offset: 2px;
    border-radius: 3px;
  }
  .silt-math :global(.katex) {
    font-size: 1.05em;
  }
  .silt-math :global(.katex-display) {
    margin: 0.5em 0;
    text-align: center;
  }
  .silt-math-err {
    color: var(--color-error, #ef4444);
    font-family: var(--font-mono, monospace);
    font-size: 0.85em;
  }
  .silt-math-empty {
    display: inline-block;
    padding: 0.5em 1em;
    border: 1px dashed var(--color-border-muted, #444);
    border-radius: 6px;
    background: transparent;
    color: var(--color-text-muted, #888);
    cursor: pointer;
    font-family: inherit;
    font-size: 0.9em;
  }
  .silt-math-empty:hover {
    border-color: var(--color-accent-primary-start, #4f7cff);
    color: var(--color-accent-primary-start, #4f7cff);
  }
</style>
