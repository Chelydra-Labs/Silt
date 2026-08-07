<script lang="ts">
  // Type creation modal — the missing half of the typed-notes empty-state fix.
  // Lets a user build a new note type entirely in-app (the only path before
  // was hand-editing YAML under .system/types/). Mirrors TurnIntoDialog's
  // overlay + focus-trap pattern (the codebase has no shared Modal component).
  //
  // On Save, assembles a TypeDef (id omitted — SaveType derives it from name)
  // and calls SaveType, which validates, writes `.system/types/<id>.yaml`,
  // emits types:changed (so any open PropertiesPanel + the TypeDashboard
  // refresh automatically), and re-projects. Backend validation errors
  // (reserved name collisions, dup property names, bad id derivation) surface
  // inline so the user can fix and resubmit.
  import { tick } from 'svelte'
  import { SaveType } from '../../bindings/silt/app.js'
  // The bindings TypeDef/PropertyDef classes are used at the IPC boundary so
  // the wire shape matches what SaveType expects. The local types.ts shapes
  // drive everything else (props, validation, the row model). Mirrors the
  // existing convention (PropertiesPanel imports local types; binding types
  // are only touched where the IPC actually fires).
  import {
    PropertyDef as BindingPropertyDef,
    TypeDef as BindingTypeDef
  } from '../../bindings/silt/backend/types/models.js'
  import { coerceIPCError } from '../lib/ipcError'
  import { trapFocus } from '../lib/focusTrap'
  import { PROPERTY_TYPES, type PropertyType } from './types'

  interface Props {
    open: boolean
    onClose: () => void
  }

  let { open, onClose }: Props = $props()

  // --- Form state ----------------------------------------------------------
  // Svelte 5 deep-reactivity makes per-field mutations on $state object arrays
  // trigger the right re-renders; a plain array is correct here (no need for
  // SvelteMap/SvelteSet — the list is ordered, keyed by rowId).
  let name = $state('')
  let description = $state('')
  let icon = $state('')
  let heroField = $state('')
  let saving = $state(false)
  let saveError = $state('')
  // Tracks whether the user has clicked Save at least once — validation only
  // surfaces after that first attempt so the dialog doesn't shout at a user
  // who is still typing their first property name.
  let attemptedSave = $state(false)

  interface PropertyRow {
    rowId: number
    name: string
    type: PropertyType
    required: boolean
    // comma-separated text → string[] on save (select / multiselect)
    optionsText: string
    // number-only bounds, kept as strings so the input can be empty
    min: string
    max: string
    // default value for text / number / select (string-shaped)
    defaultText: string
    // default for checkbox (boolean)
    defaultBool: boolean
  }

  // nextRowId is an internal counter for the {#each} keyed iteration. It is
  // intentionally NOT $state: nothing reactively depends on its value (only
  // the row's rowId field is read, and that's a snapshot). Keeping it outside
  // $state avoids a write-during-effect loop in the open() reset effect.
  let nextRowId = 1
  let rows = $state<PropertyRow[]>([])

  function newPropertyRow(): PropertyRow {
    const id = nextRowId
    nextRowId += 1
    return {
      rowId: id,
      name: '',
      type: 'text',
      required: false,
      optionsText: '',
      min: '',
      max: '',
      defaultText: '',
      defaultBool: false
    }
  }

  // Reset the form each open so a stale draft from a prior session can't leak
  // in. Seed one empty property so the list isn't a blank dead-end.
  $effect(() => {
    if (!open) return
    name = ''
    description = ''
    icon = ''
    heroField = ''
    saving = false
    saveError = ''
    attemptedSave = false
    nextRowId = 1
    rows = [newPropertyRow()]
  })

  function addProperty(): void {
    rows.push(newPropertyRow())
  }

  function removeProperty(rowId: number): void {
    const idx = rows.findIndex((r) => r.rowId === rowId)
    if (idx >= 0) rows.splice(idx, 1)
    // Drop heroField if it pointed at the removed row's name.
    if (!rows.some((r) => r.name === heroField)) heroField = ''
  }

  function changeType(row: PropertyRow, next: PropertyType): void {
    row.type = next
    // Clear conditional fields that don't apply to the new type so they
    // don't leak into the assembled TypeDef as nonsense.
    if (next !== 'select' && next !== 'multiselect') row.optionsText = ''
    if (next !== 'number') {
      row.min = ''
      row.max = ''
    }
    if (next === 'checkbox') row.defaultText = ''
  }

  // heroField options: properties that have a name. A nameless row isn't a
  // valid heroField target.
  let heroOptions = $derived(
    rows.map((r) => r.name.trim()).filter((n) => n.length > 0)
  )

  // --- Validation ----------------------------------------------------------
  // Mirrors the backend's regex (validate.go): ^[a-z][a-z0-9_]*$ — a safe
  // frontmatter key, SQL identifier, and free of smart-graph syntax.
  const NAME_RE = /^[a-z][a-z0-9_]*$/
  // Reserved frontmatter keys (validate.go): collide with system-managed
  // identity/locator fields. The backend rejects these; we mirror client-side
  // so the user gets immediate feedback.
  const RESERVED = new Set([
    'notebook',
    'section',
    'page',
    'date',
    'tags',
    'type'
  ])

  interface FieldError {
    rowId: number | null // null = type-level (name) error
    message: string
  }

  function parseOptions(text: string): string[] {
    return text
      .split(',')
      .map((s) => s.trim())
      .filter(Boolean)
  }

  function validate(): FieldError[] {
    const out: FieldError[] = []
    if (name.trim() === '') {
      out.push({ rowId: null, message: 'Type name is required.' })
    }
    // Local scratch set: rebuilt fresh per call, never read reactively.
    // SvelteSet would only add tracking overhead here.
    // eslint-disable-next-line svelte/prefer-svelte-reactivity -- local scratch
    const seen = new Set<string>()
    for (const r of rows) {
      if (r.name.trim() === '') {
        out.push({ rowId: r.rowId, message: 'Property name is required.' })
        continue
      }
      if (!NAME_RE.test(r.name)) {
        out.push({
          rowId: r.rowId,
          message: `"${r.name}" must be lowercase (a–z, 0–9, _) and start with a letter.`
        })
        continue
      }
      if (RESERVED.has(r.name)) {
        out.push({
          rowId: r.rowId,
          message: `"${r.name}" is a reserved frontmatter key.`
        })
        continue
      }
      if (seen.has(r.name)) {
        out.push({ rowId: r.rowId, message: `Duplicate property "${r.name}".` })
        continue
      }
      seen.add(r.name)
      // select REQUIRES ≥1 option; multiselect may be free-form tags (no
      // options allowed) — matches backend ValidateTypeDef.
      if (r.type === 'select' && parseOptions(r.optionsText).length === 0) {
        out.push({
          rowId: r.rowId,
          message: 'Select properties need at least one option.'
        })
      }
    }
    return out
  }

  // Errors are a derived view of the form once the user has attempted a save;
  // they update live as the user types so feedback is immediate after the
  // first attempt.
  let errors = $derived(attemptedSave ? validate() : [])

  function errorFor(rowId: number | null): string {
    return errors
      .filter((e) => e.rowId === rowId)
      .map((e) => e.message)
      .join(' ')
  }

  let typeNameError = $derived(errorFor(null))

  // --- TypeDef assembly ----------------------------------------------------
  function assemble(): BindingTypeDef {
    const props = rows
      .filter((r) => r.name.trim() !== '')
      .map((r): BindingPropertyDef => {
        // Build the wire shape from the row's string-typed inputs. Only
        // include the optional keys when they actually carry a value so the
        // canonical YAML stays clean (the backend's omitempty would drop
        // nulls anyway, but explicit > omitted is clearer for round-trips).
        const src: Record<string, unknown> = {
          name: r.name,
          type: r.type
        }
        if (r.required) src.required = true
        if (r.type === 'select' || r.type === 'multiselect') {
          const opts = parseOptions(r.optionsText)
          if (opts.length > 0) src.options = opts
        }
        if (r.type === 'number') {
          if (r.min !== '' && Number.isFinite(Number(r.min)))
            src.min = Number(r.min)
          if (r.max !== '' && Number.isFinite(Number(r.max)))
            src.max = Number(r.max)
        }
        // default: emit only for the types where the editor can express it
        // cleanly. date/datetime/multiselect/page/pages default omitted — a
        // richer default editor for those is a separate lane.
        if (r.type === 'text' && r.defaultText.trim() !== '') {
          src.default = r.defaultText
        } else if (r.type === 'number' && r.defaultText.trim() !== '') {
          const n = Number(r.defaultText)
          if (Number.isFinite(n)) src.default = n
        } else if (r.type === 'select' && r.defaultText !== '') {
          src.default = r.defaultText
        } else if (r.type === 'checkbox') {
          // Always emit a checkbox default so "default off" is explicit; the
          // backend's omitempty means false would otherwise be dropped.
          src.default = r.defaultBool
        }
        return new BindingPropertyDef(src)
      })
    const typeSrc: Record<string, unknown> = {
      id: '',
      name: name.trim(),
      properties: props
    }
    if (description.trim()) typeSrc.description = description.trim()
    if (icon.trim()) typeSrc.icon = icon.trim()
    if (heroField) typeSrc.heroField = heroField
    return new BindingTypeDef(typeSrc)
  }

  async function save(): Promise<void> {
    saveError = ''
    attemptedSave = true
    if (errors.length > 0) return
    saving = true
    try {
      await SaveType(assemble())
      onClose()
    } catch (e) {
      saveError = coerceIPCError(e).message
    } finally {
      saving = false
    }
  }

  // --- Focus trap ---------------------------------------------------------
  // Tab/Shift+Tab wrap is delegated to the shared trapFocus util (spec-aligned
  // selector that includes <select>/<textarea>/a[href]/[contenteditable>, which
  // the old per-dialog selector omitted). Esc is handled here because it has a
  // surface-specific side effect (close + stopPropagation, gated on `saving`).
  let dialogRef = $state<HTMLDivElement | null>(null)
  let previouslyFocused: HTMLElement | null = null

  function handleKeydown(e: KeyboardEvent): void {
    if (e.key === 'Escape') {
      e.preventDefault()
      e.stopPropagation()
      if (!saving) onClose()
    }
  }

  $effect(() => {
    if (!open) return
    previouslyFocused = document.activeElement as HTMLElement | null
    const disposeTrap = dialogRef ? trapFocus(dialogRef) : () => {}
    window.addEventListener('keydown', handleKeydown, true)
    void tick().then(() => dialogRef?.focus())
    return () => {
      disposeTrap()
      window.removeEventListener('keydown', handleKeydown, true)
      if (previouslyFocused?.isConnected) previouslyFocused.focus?.()
      previouslyFocused = null
    }
  })
</script>

{#if open}
  <div class="te-overlay" data-focus-trap>
    <!--
      Overlay click target. tabindex=-1 keeps it out of the focus trap; the
      visually-hidden-on-purpose button is the click surface (Cancel label is
      surfaced to AT rather than the misleading "close").
    -->
    <button
      type="button"
      tabindex="-1"
      aria-label="Cancel"
      class="te-backdrop"
      onclick={() => {
        if (!saving) onClose()
      }}
    ></button>

    <div
      bind:this={dialogRef}
      role="dialog"
      aria-modal="true"
      aria-labelledby="type-editor-title"
      tabindex="-1"
      class="te-surface"
    >
      <header class="te-head">
        <h2 id="type-editor-title" class="te-title">New type</h2>
        <p class="te-subtitle">
          Define a type and its properties. The id is derived from the name.
        </p>
      </header>

      <div class="te-body custom-scrollbar">
        <fieldset class="te-section">
          <legend class="te-legend">Identity</legend>
          <div class="te-grid">
            <label class="te-field te-span-2" for="te-name">
              <span class="te-label">
                Name <span class="req" aria-hidden="true">*</span>
              </span>
              <input
                id="te-name"
                type="text"
                class="te-input"
                bind:value={name}
                aria-required="true"
                aria-invalid={typeNameError !== ''}
                aria-describedby={typeNameError ? 'te-name-err' : undefined}
                placeholder="Book"
              />
            </label>
            <label class="te-field te-span-2" for="te-desc">
              <span class="te-label">Description</span>
              <input
                id="te-desc"
                type="text"
                class="te-input"
                bind:value={description}
                placeholder="Reading notes with author, status, and rating"
              />
            </label>
            <label class="te-field" for="te-icon">
              <span class="te-label">Icon</span>
              <span class="te-icon-row">
                {#if icon.trim()}
                  <span
                    class="material-symbols-outlined te-icon-preview"
                    aria-hidden="true">{icon.trim()}</span
                  >
                {/if}
                <input
                  id="te-icon"
                  type="text"
                  class="te-input"
                  bind:value={icon}
                  placeholder="menu_book"
                  aria-describedby="te-icon-hint"
                />
              </span>
              <span id="te-icon-hint" class="te-hint"
                >Material Symbol name (e.g. menu_book)</span
              >
            </label>
            <label class="te-field" for="te-hero">
              <span class="te-label">Hero field</span>
              <select id="te-hero" class="te-input" bind:value={heroField}>
                <option value="">None</option>
                {#each heroOptions as opt (opt)}
                  <option value={opt}>{opt}</option>
                {/each}
              </select>
              <span class="te-hint">Shown as the page headline</span>
            </label>
          </div>
          {#if typeNameError}
            <p id="te-name-err" class="te-err" role="alert">{typeNameError}</p>
          {/if}
        </fieldset>

        <fieldset class="te-section">
          <legend class="te-legend">Properties</legend>

          {#if rows.length === 0}
            <p class="te-empty">No properties yet — the type will be empty.</p>
          {:else}
            <ul class="te-rows" role="list">
              {#each rows as row (row.rowId)}
                {@const rowErr = errorFor(row.rowId)}
                {@const opts = parseOptions(row.optionsText)}
                <li class="te-row">
                  <div class="te-row-head">
                    <input
                      id={`te-pname-${row.rowId}`}
                      type="text"
                      class="te-input te-pname"
                      placeholder="property name (e.g. title)"
                      bind:value={row.name}
                      aria-label={`Property ${row.rowId} name`}
                      aria-required="true"
                      aria-invalid={rowErr !== ''}
                      aria-describedby={rowErr
                        ? `te-perr-${row.rowId}`
                        : undefined}
                    />
                    <select
                      class="te-input te-ptype"
                      value={row.type}
                      onchange={(e) =>
                        changeType(row, e.currentTarget.value as PropertyType)}
                      aria-label={`Property ${row.rowId} type`}
                    >
                      {#each PROPERTY_TYPES as pt (pt)}
                        <option value={pt}>{pt}</option>
                      {/each}
                    </select>
                    <label class="te-check">
                      <input
                        type="checkbox"
                        bind:checked={row.required}
                        aria-label={`Property ${row.rowId} required`}
                      />
                      <span aria-hidden="true">required</span>
                    </label>
                    <button
                      type="button"
                      class="te-icon-btn te-remove"
                      aria-label={`Remove property ${row.rowId}`}
                      title="Remove property"
                      onclick={() => removeProperty(row.rowId)}
                      disabled={saving}
                    >
                      <span class="material-symbols-outlined" aria-hidden="true"
                        >remove_circle_outline</span
                      >
                    </button>
                  </div>

                  <div class="te-row-cond">
                    {#if row.type === 'select' || row.type === 'multiselect'}
                      <label class="te-field" for={`te-opts-${row.rowId}`}>
                        <span class="te-label">Options</span>
                        <input
                          id={`te-opts-${row.rowId}`}
                          type="text"
                          class="te-input"
                          bind:value={row.optionsText}
                          placeholder="todo, reading, done"
                          aria-required={row.type === 'select' || undefined}
                        />
                        <span class="te-hint">
                          {#if row.type === 'multiselect'}
                            Comma-separated. Leave empty for free-form tags.
                          {:else}
                            Comma-separated.
                          {/if}
                        </span>
                      </label>
                    {/if}

                    {#if row.type === 'number'}
                      <label class="te-field" for={`te-min-${row.rowId}`}>
                        <span class="te-label">Min</span>
                        <input
                          id={`te-min-${row.rowId}`}
                          type="number"
                          class="te-input"
                          bind:value={row.min}
                          placeholder="0"
                        />
                      </label>
                      <label class="te-field" for={`te-max-${row.rowId}`}>
                        <span class="te-label">Max</span>
                        <input
                          id={`te-max-${row.rowId}`}
                          type="number"
                          class="te-input"
                          bind:value={row.max}
                          placeholder="5"
                        />
                      </label>
                    {/if}

                    {#if row.type === 'text' || row.type === 'number'}
                      <label class="te-field" for={`te-def-${row.rowId}`}>
                        <span class="te-label">
                          Default <span class="te-hint">(optional)</span>
                        </span>
                        <input
                          id={`te-def-${row.rowId}`}
                          type={row.type === 'number' ? 'number' : 'text'}
                          class="te-input"
                          bind:value={row.defaultText}
                        />
                      </label>
                    {:else if row.type === 'select'}
                      <label class="te-field" for={`te-def-${row.rowId}`}>
                        <span class="te-label">
                          Default <span class="te-hint">(optional)</span>
                        </span>
                        <select
                          id={`te-def-${row.rowId}`}
                          class="te-input"
                          bind:value={row.defaultText}
                        >
                          <option value="">—</option>
                          {#each opts as opt (opt)}
                            <option value={opt}>{opt}</option>
                          {/each}
                        </select>
                      </label>
                    {:else if row.type === 'checkbox'}
                      <label class="te-check te-default">
                        <input type="checkbox" bind:checked={row.defaultBool} />
                        <span>default on</span>
                      </label>
                    {/if}
                  </div>

                  {#if rowErr}
                    <p id={`te-perr-${row.rowId}`} class="te-err" role="alert">
                      {rowErr}
                    </p>
                  {/if}
                </li>
              {/each}
            </ul>
          {/if}

          <button
            type="button"
            class="te-add"
            onclick={addProperty}
            disabled={saving}
          >
            <span class="material-symbols-outlined" aria-hidden="true">add</span
            >
            Add property
          </button>
        </fieldset>

        {#if saveError}
          <p class="te-err te-save-err" role="alert">{saveError}</p>
        {/if}
      </div>

      <footer class="te-foot">
        <button
          type="button"
          class="te-btn te-btn-ghost"
          onclick={() => {
            if (!saving) onClose()
          }}
          disabled={saving}
        >
          Cancel
        </button>
        <button
          type="button"
          class="te-btn te-btn-primary"
          onclick={save}
          disabled={saving}
        >
          {saving ? 'Saving…' : 'Create type'}
        </button>
      </footer>
    </div>
  </div>
{/if}

<style>
  .te-overlay {
    position: fixed;
    inset: 0;
    z-index: 200;
    display: flex;
    align-items: center;
    justify-content: center;
    background: rgba(0, 0, 0, 0.4);
    backdrop-filter: blur(2px);
    padding: 1rem;
  }
  .te-backdrop {
    position: absolute;
    inset: 0;
    border: 0;
    background: transparent;
    cursor: default;
    padding: 0;
  }
  .te-surface {
    position: relative;
    width: 40rem;
    max-width: 100%;
    max-height: calc(100vh - 2rem);
    display: flex;
    flex-direction: column;
    background: var(--color-surface-modal);
    border: 1px solid var(--color-surface-modal-border);
    border-radius: 0.75rem;
    box-shadow: var(--shadow-lg);
    color: var(--color-surface-modal-text);
    overflow: hidden;
    outline: none;
  }
  .te-surface:focus-visible {
    outline: 2px solid var(--color-border-focus);
    outline-offset: 2px;
  }
  .te-head {
    padding: 1rem 1.25rem 0.75rem;
    border-bottom: 1px solid var(--color-surface-modal-border);
    flex: 0 0 auto;
  }
  .te-title {
    margin: 0;
    font-family: var(--font-headline, sans-serif);
    font-size: var(--text-type-lg);
    font-weight: 600;
    color: var(--color-text-primary);
  }
  .te-subtitle {
    margin: 0.35rem 0 0;
    color: var(--color-text-muted);
    font-size: var(--text-type-sm);
  }
  .te-body {
    padding: 0.85rem 1.25rem;
    overflow-y: auto;
    min-height: 0;
    flex: 1 1 auto;
    display: flex;
    flex-direction: column;
    gap: 1rem;
  }
  .te-section {
    border: 0;
    margin: 0;
    padding: 0;
    min-width: 0;
  }
  .te-legend {
    padding: 0;
    margin-bottom: 0.5rem;
    font-family: var(--font-mono, monospace);
    font-size: var(--text-type-2xs);
    text-transform: uppercase;
    letter-spacing: 0.08em;
    color: var(--color-text-muted);
  }
  .te-grid {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 0.6rem;
  }
  .te-field {
    display: flex;
    flex-direction: column;
    gap: 0.2rem;
    min-width: 0;
  }
  .te-span-2 {
    grid-column: 1 / -1;
  }
  .te-label {
    font-size: var(--text-type-xs);
    color: var(--color-text-muted);
    font-weight: 600;
  }
  .te-input {
    width: 100%;
    background: var(--color-surface-app);
    border: 1px solid var(--color-surface-panel-border);
    color: var(--color-text-primary);
    border-radius: 0.375rem;
    padding: 0.35rem 0.55rem;
    font-size: var(--text-type-sm);
    line-height: 1.4;
    font-family: inherit;
  }
  .te-input:focus-visible {
    outline: 2px solid var(--color-border-focus);
    outline-offset: 1px;
  }
  .te-input:disabled {
    opacity: 0.6;
  }
  .te-icon-row {
    display: flex;
    align-items: center;
    gap: 0.4rem;
    min-width: 0;
  }
  .te-icon-row .te-input {
    flex: 1 1 auto;
    min-width: 0;
  }
  .te-icon-preview {
    flex: 0 0 auto;
    font-size: var(--text-icon-md);
    color: var(--color-text-muted);
  }
  .te-hint {
    font-size: var(--text-type-2xs);
    color: var(--color-text-muted);
    font-weight: 400;
  }
  .te-rows {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 0.5rem;
  }
  .te-row {
    display: flex;
    flex-direction: column;
    gap: 0.4rem;
    padding: 0.6rem;
    border: 1px solid var(--color-surface-panel-border);
    border-radius: 0.5rem;
    background: var(--color-surface-app);
  }
  .te-row-head {
    display: grid;
    grid-template-columns: 1fr 8rem auto auto;
    align-items: center;
    gap: 0.4rem;
  }
  .te-row-cond {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(8rem, 1fr));
    gap: 0.45rem;
  }
  .te-check {
    display: inline-flex;
    align-items: center;
    gap: 0.3rem;
    font-size: var(--text-type-xs);
    color: var(--color-text-muted);
    cursor: pointer;
    user-select: none;
    white-space: nowrap;
  }
  .te-check input {
    margin: 0;
  }
  .te-icon-btn {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    border: 0;
    background: transparent;
    color: var(--color-text-muted);
    padding: 0.2rem;
    border-radius: 0.3rem;
    cursor: pointer;
  }
  .te-icon-btn .material-symbols-outlined {
    font-size: var(--text-icon-md);
  }
  .te-icon-btn:hover {
    color: var(--color-status-danger);
    background: var(--color-hover);
  }
  .te-icon-btn:focus-visible {
    outline: 2px solid var(--color-border-focus);
    outline-offset: 1px;
  }
  .te-icon-btn:disabled {
    opacity: 0.4;
    cursor: default;
  }
  .te-empty {
    color: var(--color-text-muted);
    font-size: var(--text-type-sm);
    margin: 0;
  }
  .te-add {
    margin-top: 0.6rem;
    display: inline-flex;
    align-items: center;
    gap: 0.35rem;
    align-self: flex-start;
    padding: 0.35rem 0.75rem;
    border: 1px dashed var(--color-surface-panel-border);
    background: transparent;
    color: var(--color-text-muted);
    border-radius: 0.375rem;
    font-size: var(--text-type-sm);
    cursor: pointer;
    transition:
      background 120ms var(--transition-standard),
      color 120ms var(--transition-standard);
  }
  .te-add .material-symbols-outlined {
    font-size: var(--text-icon-sm);
  }
  .te-add:hover {
    color: var(--color-text-primary);
    background: var(--color-hover);
  }
  .te-add:focus-visible {
    outline: 2px solid var(--color-border-focus);
    outline-offset: 1px;
  }
  .te-add:disabled {
    opacity: 0.5;
    cursor: default;
  }
  .req {
    color: var(--color-status-warn);
    margin-left: 0.1rem;
  }
  .te-err {
    margin: 0.4rem 0 0;
    color: var(--color-error-fg);
    font-size: var(--text-type-xs);
  }
  .te-save-err {
    margin: 0;
    padding: 0.55rem 0.7rem;
    background: var(--color-error-bg);
    border: 1px solid var(--color-error-border);
    border-radius: 0.375rem;
  }
  .te-foot {
    display: flex;
    justify-content: flex-end;
    gap: 0.5rem;
    padding: 0.75rem 1.25rem;
    border-top: 1px solid var(--color-surface-modal-border);
    flex: 0 0 auto;
  }
  .te-btn {
    padding: 0.4rem 1rem;
    border-radius: 0.5rem;
    font-size: var(--text-type-sm);
    font-weight: 600;
    font-family: var(--font-mono, monospace);
    cursor: pointer;
    border: 1px solid transparent;
  }
  .te-btn:disabled {
    opacity: 0.6;
    cursor: default;
  }
  .te-btn-ghost {
    background: transparent;
    color: var(--color-text-muted);
    border-color: var(--color-surface-panel-border);
  }
  .te-btn-ghost:hover:not(:disabled) {
    color: var(--color-text-primary);
    background: var(--color-hover);
  }
  .te-btn-primary {
    background: var(--color-accent-primary-glow);
    color: var(--color-accent-primary-start);
    border-color: var(--color-accent-primary-start);
  }
  .te-btn-primary:hover:not(:disabled) {
    background: var(--color-hover);
  }
  .te-btn:focus-visible {
    outline: 2px solid var(--color-border-focus);
    outline-offset: 1px;
  }
</style>
