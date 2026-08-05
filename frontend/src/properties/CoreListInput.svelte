<script lang="ts">
  // CoreListInput — comma-separated text input for a core string-list field
  // (tags / aliases) in the PropertiesPanel Core section (#867). Owns a local
  // edit draft so a concurrent core refresh (block:changed from a sync client
  // or a sibling edit) cannot clobber in-progress typing: the draft is seeded
  // from `values` once at mount, and the parent remounts this component via
  // {#key pageLocator} on navigation so the draft re-seeds when the page
  // changes, while a same-page refresh does NOT remount — preserving any dirty
  // draft until the user blurs (flush). Using bind:value (not a reactive
  // value= projection) is what makes the field non-clobbering; the {#key} gate
  // is what re-seeds it on navigation.
  import type { CoreFieldUpdate } from './types'

  interface Props {
    /** The committed list, rendered as the initial draft (and on remount). */
    values: string[]
    /** The committed list for the no-op-skip check on flush. */
    current: string[]
    /** Field-granular update builder, e.g. (n) => ({ tags: n }). */
    buildUpdate: (next: string[]) => CoreFieldUpdate
    /** Resolves true on success, false on failure. On failure the parent
     *  bumps its rollback nonce and remounts this input (re-seeding the draft
     *  from `current`); we ALSO re-seed here synchronously so a stale
     *  draft can't drive a second failing write before the remount lands. */
    onCommit: (update: CoreFieldUpdate) => Promise<boolean>
    id: string
    label: string
    placeholder: string
    hint: string
    disabled?: boolean
  }

  let {
    values,
    current,
    buildUpdate,
    onCommit,
    id,
    label,
    placeholder,
    hint,
    disabled = false
  }: Props = $props()

  // Seed once from the committed values. The parent's {#key} on the page
  // locator remounts this component on navigation so the draft re-seeds; a
  // same-page core refresh does not remount, leaving a dirty draft intact.
  // svelte-ignore state_referenced_locally
  // Capturing only the INITIAL values is intentional — reactivity here would
  // clobber in-progress typing on a concurrent core refresh; the {#key} gate
  // is the re-seed mechanism, not $state reactivity.
  let draft = $state(values.join(', '))

  function splitList(input: string): string[] {
    // Empty / whitespace-only → empty list (clears the frontmatter key).
    if (input.trim() === '') return []
    return input
      .split(',')
      .map((s) => s.trim())
      .filter((s) => s !== '')
  }

  function eqList(a: string[], b: string[]): boolean {
    if (a.length !== b.length) return false
    for (let i = 0; i < a.length; i++) {
      if (a[i] !== b[i]) return false
    }
    return true
  }

  async function flush(): Promise<void> {
    const next = splitList(draft)
    // Skip the commit when the parsed draft matches the committed value —
    // avoids a redundant write on a no-op blur (focus-then-blur without typing).
    if (eqList(next, current)) return
    const ok = await onCommit(buildUpdate(next))
    if (!ok) {
      // Rejected write (disk full / sync lock / page moved): re-seed the draft
      // from the committed value so the next blur's eqList check passes and we
      // don't retry the same failing write on every subsequent blur. The parent
      // will also bump rollbackNonce to remount us; this synchronous re-seed
      // covers the window before the remount lands.
      draft = current.join(', ')
    }
  }
</script>

<div class="core-field core-field-wide">
  <label class="core-label" for={id}>{label}</label>
  <input
    {id}
    class="core-input"
    type="text"
    inputmode="text"
    {placeholder}
    bind:value={draft}
    {disabled}
    onblur={flush}
    onkeydown={(e) => {
      if (e.key === 'Enter') {
        // Don't flush during IME composition (CJK input) — the Enter that
        // confirms a candidate would otherwise commit the raw composition
        // string to tags/aliases.
        if (e.isComposing || e.keyCode === 229) return
        e.preventDefault()
        void flush()
      }
    }}
    aria-describedby={`${id}-hint`}
  />
  <p id={`${id}-hint`} class="core-hint">{hint}</p>
</div>

<style>
  .core-field {
    display: flex;
    flex-direction: column;
    gap: 0.2rem;
    min-width: 0;
  }
  .core-field-wide {
    grid-column: span 2;
  }
  /*
    At narrow widths, the wide fields drop back to a single column so the
    comma-separated inputs keep usable width rather than forcing horizontal
    scroll. Mirrors the parent grid's responsive collapse.
  */
  @media (max-width: 40rem) {
    .core-field-wide {
      grid-column: auto;
    }
  }
  .core-label {
    font-size: var(--text-type-xs);
    color: var(--color-text-muted);
  }
  .core-input {
    width: 100%;
    padding: 0.25rem 0.5rem;
    border-radius: 0.375rem;
    border: 1px solid var(--color-surface-panel-border);
    background: var(--color-surface-app);
    color: var(--color-text-primary);
    font-size: var(--text-type-sm);
    min-width: 0;
  }
  .core-input:hover:not(:disabled) {
    background: var(--color-hover);
  }
  .core-input:focus-visible {
    outline: 2px solid var(--color-border-focus);
    outline-offset: 1px;
  }
  .core-input:disabled {
    opacity: 0.6;
    cursor: not-allowed;
  }
  .core-hint {
    margin: 0;
    font-size: var(--text-type-xs);
    color: var(--color-text-muted);
  }
</style>
