<script lang="ts">
  // Core metadata section — the type-independent fields every page exposes in
  // the PropertiesPanel (#867), rendered ABOVE the type-defined section. Splits
  // the editable fields (date / tags / aliases / created) from the read-only
  // `modified` field. Edits route through onCommit with a field-granular
  // CoreFieldUpdate (only the changed field is sent, so sibling in-flight edits
  // don't race each other).
  //
  // Free-text list fields (tags, aliases) are rendered by CoreListInput, which
  // holds a LOCAL edit draft seeded on mount and remounts on page-locator
  // change (the {#key pageLocator} gate below). That design prevents a
  // concurrent core refresh (block:changed from a sync client or a sibling
  // edit) from clobbering in-progress typing: the draft only re-seeds when the
  // PAGE changes, not on every core refresh. The date / created fields are
  // change-committed (select/pick → onchange), so the same clobber window has
  // negligible impact there.
  import { coerceIPCError } from '../lib/ipcError'
  import type { CoreFieldUpdate, PageCoreMetadata } from './types'
  import CoreListInput from './CoreListInput.svelte'

  interface Props {
    core: PageCoreMetadata
    onCommit: (update: CoreFieldUpdate) => Promise<void>
    onError: (message: string) => void
    onChanged: () => void
  }

  let { core, onCommit, onError, onChanged }: Props = $props()

  // Page identity gates CoreListInput remounts (see comment above). Composed
  // from the core payload's locator fields.
  let pageLocator = $derived(`${core.notebook}/${core.section}/${core.page}`)

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

    {#key pageLocator}
      <CoreListInput
        id="core-tags"
        label={`Tags${core.tagsAreReadOnly ? ' (read-only)' : ''}`}
        placeholder="comma, separated, tags"
        hint="Frontmatter tag list. Comma-separated."
        values={core.tags}
        current={core.tags}
        disabled={core.tagsAreReadOnly}
        buildUpdate={(n) => ({ tags: n })}
        onCommit={(u) => void commit(u)}
      />
    {/key}

    {#key pageLocator}
      <CoreListInput
        id="core-aliases"
        label="Aliases"
        placeholder="comma, separated, aliases"
        hint="Alternate names. Comma-separated. Distinct from wiki-link display text."
        values={core.aliases}
        current={core.aliases}
        buildUpdate={(n) => ({ aliases: n })}
        onCommit={(u) => void commit(u)}
      />
    {/key}

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
  .core-value-readonly {
    font-size: var(--text-type-sm);
    color: var(--color-text-primary);
    padding: 0.25rem 0;
  }
</style>
