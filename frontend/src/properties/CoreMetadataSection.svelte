<script lang="ts">
  // Core metadata section — the type-independent fields every page exposes in
  // the PropertiesPanel (#867), rendered ABOVE the type-defined section. Splits
  // the editable fields (date / tags / aliases / created) from the read-only
  // `modified` field. Edits route through onCommit with a field-granular
  // CoreFieldUpdate (only the changed field is sent, so sibling in-flight edits
  // don't race each other).
  //
  // Editable text-list fields (tags, aliases) use a comma-separated text input
  // whose value is split-and-trimmed on blur / Enter. This keeps the control
  // single-line, fully keyboard-operable, and avoids a custom chip UI that
  // would need its own a11y wiring (Tab trap, Remove affordance, live-region
  // announcements). A value containing a comma round-trips through the
  // frontmatter JSON array (yamlInline handles quoting).
  import { coerceIPCError } from '../lib/ipcError'
  import type { CoreFieldUpdate, PageCoreMetadata } from './types'

  interface Props {
    core: PageCoreMetadata
    onCommit: (update: CoreFieldUpdate) => Promise<void>
    onError: (message: string) => void
    onChanged: () => void
  }

  let { core, onCommit, onError, onChanged }: Props = $props()

  // Editable text-list fields (tags, aliases) are uncontrolled inputs whose
  // value is projected from `core` and flushed on blur / Enter by reading the
  // DOM value. Because Svelte only re-sets `value` when `core` actually
  // changes, in-progress typing is preserved between flushes, and an external
  // refresh (block:changed, IPC rejection) resyncs the field without stranding
  // a stale typed value.
  function splitList(input: string): string[] {
    // Empty / whitespace-only → empty list (clears the frontmatter key).
    if (input.trim() === '') return []
    return input
      .split(',')
      .map((s) => s.trim())
      .filter((s) => s !== '')
  }

  async function commit(update: CoreFieldUpdate): Promise<void> {
    try {
      await onCommit(update)
      onChanged()
    } catch (e) {
      onError(coerceIPCError(e).message)
    }
  }

  async function onDateChange(e: Event): Promise<void> {
    const v = (e.currentTarget as HTMLInputElement).value
    await commit({ date: v })
  }

  async function onCreatedChange(e: Event): Promise<void> {
    const v = (e.currentTarget as HTMLInputElement).value
    await commit({ created: v })
  }

  function flushListField(
    input: HTMLInputElement,
    current: string[],
    build: (next: string[]) => CoreFieldUpdate
  ): void {
    const next = splitList(input.value)
    // Skip the commit when the parsed value matches the committed value —
    // avoids a redundant write on a no-op blur (e.g. focusing then unfocusing
    // the field without typing).
    if (eqList(next, current)) return
    void commit(build(next))
  }

  function eqList(a: string[], b: string[]): boolean {
    if (a.length !== b.length) return false
    for (let i = 0; i < a.length; i++) {
      if (a[i] !== b[i]) return false
    }
    return true
  }

  // `created` accepts either a bare date or a full RFC3339 timestamp. Use a
  // datetime-local input when the committed value carries a time component;
  // fall back to a date input for bare dates (so the user can pick from a
  // calendar). The input value strips the seconds + timezone for the form.
  let createdInputType = $derived(
    core.created.includes('T') || core.created.includes(':')
      ? 'datetime-local'
      : 'date'
  )
  // datetime-local value format: YYYY-MM-DDTHH:MM (seconds + tz stripped).
  let createdInputValue = $derived(toInputDateTimeLocal(core.created))

  function toInputDateTimeLocal(s: string): string {
    if (s === '') return ''
    // Bare date: pass through.
    if (!s.includes('T')) return s
    // RFC3339: trim trailing timezone + seconds to match datetime-local form.
    const withoutTz = s.replace(/([+-]\d{2}:\d{2}|Z)$/, '')
    return withoutTz.slice(0, 16) // YYYY-MM-DDTHH:MM
  }

  // `modified` is read-only; render as a static, formatted string for clarity
  // (raw RFC3339 is hard to scan).
  let modifiedDisplay = $derived(formatModified(core.modified))

  function formatModified(s: string): string {
    if (s === '') return '—'
    const d = new Date(s)
    if (Number.isNaN(d.getTime())) return s
    return d.toLocaleString()
  }
</script>

<section class="core-section" aria-labelledby="core-section-title">
  <h3 id="core-section-title" class="core-title">Core</h3>

  <div class="core-grid">
    <div class="core-field">
      <label class="core-label" for="core-date">Date</label>
      <input
        id="core-date"
        class="core-input"
        type="date"
        value={core.date}
        onchange={onDateChange}
      />
    </div>

    <div class="core-field">
      <label class="core-label" for="core-created">Created</label>
      <input
        id="core-created"
        class="core-input"
        type={createdInputType}
        value={createdInputValue}
        onchange={onCreatedChange}
      />
    </div>

    <div class="core-field core-field-wide">
      <label class="core-label" for="core-tags">
        Tags{core.tagsAreReadOnly ? ' (read-only)' : ''}
      </label>
      <input
        id="core-tags"
        class="core-input"
        type="text"
        inputmode="text"
        placeholder="comma, separated, tags"
        value={core.tags.join(', ')}
        disabled={core.tagsAreReadOnly}
        onblur={(e) =>
          flushListField(
            e.currentTarget as HTMLInputElement,
            core.tags,
            (n) => ({ tags: n })
          )}
        onkeydown={(e) => {
          if (e.key === 'Enter') {
            e.preventDefault()
            flushListField(
              e.currentTarget as HTMLInputElement,
              core.tags,
              (n) => ({ tags: n })
            )
          }
        }}
        aria-describedby="core-tags-hint"
      />
      <p id="core-tags-hint" class="core-hint">
        Frontmatter tag list. Comma-separated.
      </p>
    </div>

    <div class="core-field core-field-wide">
      <label class="core-label" for="core-aliases">Aliases</label>
      <input
        id="core-aliases"
        class="core-input"
        type="text"
        inputmode="text"
        placeholder="comma, separated, aliases"
        value={core.aliases.join(', ')}
        onblur={(e) =>
          flushListField(
            e.currentTarget as HTMLInputElement,
            core.aliases,
            (n) => ({ aliases: n })
          )}
        onkeydown={(e) => {
          if (e.key === 'Enter') {
            e.preventDefault()
            flushListField(
              e.currentTarget as HTMLInputElement,
              core.aliases,
              (n) => ({ aliases: n })
            )
          }
        }}
        aria-describedby="core-aliases-hint"
      />
      <p id="core-aliases-hint" class="core-hint">
        Alternate names. Comma-separated. Distinct from wiki-link display text.
      </p>
    </div>

    <div class="core-field">
      <span class="core-label" id="core-modified-label">Modified</span>
      <span
        class="core-value-readonly"
        role="status"
        aria-labelledby="core-modified-label"
      >
        {modifiedDisplay}
      </span>
    </div>
  </div>
</section>

<style>
  .core-section {
    display: flex;
    flex-direction: column;
    gap: 0.4rem;
    padding: 0.6rem 0.75rem;
    border-bottom: 1px solid var(--color-surface-panel-border);
    flex: 0 0 auto;
  }
  .core-title {
    margin: 0;
    font-size: var(--text-type-xs);
    font-weight: 600;
    color: var(--color-text-muted);
    text-transform: uppercase;
    letter-spacing: 0.04em;
  }
  .core-grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(15rem, 1fr));
    gap: 0.5rem 0.9rem;
  }
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
    scroll.
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
  .core-value-readonly {
    font-size: var(--text-type-sm);
    color: var(--color-text-primary);
    padding: 0.25rem 0;
  }
</style>
