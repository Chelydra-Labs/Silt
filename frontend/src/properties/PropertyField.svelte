<script lang="ts">
  // One typed-property editor. Dispatches the right control by `value.type`
  // and commits every edit through the optimistic pattern: snapshot → show new
  // value → await SetPageProperty → revert + report on rejection. The wire
  // value is what SetPageProperty receives; the input's displayed value is a
  // derived string/array off the optimistic field, so a rejected edit snaps
  // back to the last accepted value.
  import { tick, untrack } from 'svelte'
  import {
    ClearPageProperty,
    SetPageProperty
  } from '../../bindings/silt/app.js'
  import { coerceIPCError } from '../lib/ipcError'
  import { optimisticField } from './optimisticField.svelte'
  import PageLinkField from './PageLinkField.svelte'
  import type { PageLocator, PagePropertyValue } from './types'

  interface Props {
    value: PagePropertyValue
    locator: PageLocator
    min?: number | null
    max?: number | null
    /** Declared relation target type (page/pages only). */
    target?: string
    mismatched?: boolean
    onError: (message: string) => void
    onChanged: () => void
    /**
     * Re-fetch after a write rejection without treating it as success (must
     * NOT clear the error banner). Defaults to onChanged when omitted.
     */
    onResync?: () => void
  }

  let {
    value,
    locator,
    min = null,
    max = null,
    target = '',
    mismatched = false,
    onError,
    onChanged,
    onResync
  }: Props = $props()

  // Coerce the incoming wire value to the editor's native type so the control
  // never renders `undefined`. Multiselect/pages are arrays; everything else
  // reduces to a string or boolean.
  // Stringify a scalar without risking "[object Object]"; non-primitive shapes
  // aren't expected for scalar fields and fall back to ''.
  function scalarToString(v: unknown): string {
    if (typeof v === 'string') return v
    if (
      typeof v === 'number' ||
      typeof v === 'boolean' ||
      typeof v === 'bigint'
    ) {
      return String(v)
    }
    return ''
  }

  function toWire(v: unknown): string | number | boolean | string[] {
    if (value.type === 'checkbox') return !!v
    if (value.type === 'multiselect' || value.type === 'pages') {
      return Array.isArray(v)
        ? v.map(String)
        : v == null || v === ''
          ? []
          : [scalarToString(v)]
    }
    if (value.type === 'number') {
      if (v == null || v === '') return ''
      const n = Number(v)
      return Number.isFinite(n) ? n : ''
    }
    return v == null ? '' : scalarToString(v)
  }

  // untrack for `initial`: the seed is intentionally a one-time read. The
  // $effect below re-seeds the field when the controller refreshes the value,
  // so the prop is not lost — only the top-level reactive read is suppressed.
  // Callbacks are wrapped in arrows so they read the live props at call time.
  let field = $state(
    optimisticField<string | number | boolean | string[]>({
      initial: untrack(() => toWire(value.value)),
      write: (next) =>
        SetPageProperty(
          locator.notebook,
          locator.section,
          locator.page,
          value.name,
          next
        ),
      onError: (msg) => onError(msg),
      onChanged: () => onChanged(),
      // On write failure, re-fetch so the field reseeds from disk. Prefer the
      // dedicated onResync prop (keeps the error banner); fall back to onChanged.
      onResync: () => (onResync ?? onChanged)()
    })
  )

  // Re-seed when the controller refreshes the value (page switch, external
  // edit, post-commit re-sync). reset() does NOT invoke write. Skip while the
  // control is focused or a write is in flight so a sibling-field commit /
  // types:changed refresh cannot wipe uncommitted keystrokes.
  let focused = $state(false)
  let lastLocatorKey = untrack(
    () => `${locator.notebook}\0${locator.section}\0${locator.page}`
  )
  $effect(() => {
    const locKey = `${locator.notebook}\0${locator.section}\0${locator.page}`
    const next = toWire(value.value)
    if (locKey !== lastLocatorKey) {
      lastLocatorKey = locKey
      focused = false
      field.reset(next)
      return
    }
    if (focused || field.pending) return
    field.reset(next)
  })

  function onFocus(): void {
    focused = true
  }
  function onBlur(): void {
    focused = false
  }

  let wire = $derived(field.value)
  // Single-line string rendering for text-like inputs.
  let textValue = $derived(
    typeof wire === 'string'
      ? wire
      : Array.isArray(wire)
        ? wire.join(', ')
        : String(wire ?? '')
  )
  let numberValue = $derived(
    typeof wire === 'number'
      ? String(wire)
      : typeof wire === 'string'
        ? wire
        : ''
  )
  let checked = $derived(wire === true)
  let multiValue = $derived(Array.isArray(wire) ? wire : [])
  // Relation fields take only string (page) or string[] (pages); narrow the
  // optimistic union so PageLinkField gets a well-typed value.
  let relationWire = $derived(
    value.type === 'pages'
      ? Array.isArray(wire)
        ? wire
        : []
      : typeof wire === 'string'
        ? wire
        : ''
  )

  let fieldId = $derived(`prop-${value.name}`)
  let hasOptions = $derived(!!value.options && value.options.length > 0)
  // Drives both the visual `*` and aria-required on the rendered control so
  // screen readers announce required fields (the marker span is aria-hidden).
  let isRequired = $derived(!!value.required)

  let freeMultiDraft = $state('')

  function commitText(e: Event): void {
    const target = e.currentTarget as HTMLInputElement
    void field.commit(target.value)
  }

  let freeMultiInputRef = $state<HTMLInputElement | null>(null)

  function removeMultiValue(opt: string): void {
    const next = multiValue.filter((v) => v !== opt)
    void field.commit(next)
    // Chip button unmounts with the value; restore focus to the add input
    // (or the group) so keyboard users are not stranded on <body>.
    void tick().then(() => {
      freeMultiInputRef?.focus()
      if (!freeMultiInputRef) {
        document.getElementById(fieldId)?.querySelector('button')?.focus()
      }
    })
  }

  function commitFreeMultiAdd(e?: Event): void {
    const raw =
      e && e.currentTarget instanceof HTMLInputElement
        ? e.currentTarget.value
        : freeMultiDraft
    const part = raw.trim()
    if (!part) return
    // Whole token — do not split on commas (values may contain them).
    if (!multiValue.includes(part)) {
      void field.commit([...multiValue, part])
    }
    freeMultiDraft = ''
    if (e && e.currentTarget instanceof HTMLInputElement) {
      e.currentTarget.value = ''
    }
  }

  function onFreeMultiKeydown(e: KeyboardEvent): void {
    if (e.key === 'Enter') {
      e.preventDefault()
      commitFreeMultiAdd()
    }
  }

  function commitNumber(e: Event): void {
    const target = e.currentTarget as HTMLInputElement
    const raw = target.value
    // An empty number input clears the frontmatter key (ClearPageProperty)
    // rather than writing an empty string, so the property becomes unset.
    if (raw === '') {
      void clearField()
      return
    }
    const n = Number(raw)
    void field.commit(Number.isFinite(n) ? n : raw)
  }

  function commitSelect(e: Event): void {
    const target = e.currentTarget as HTMLSelectElement
    void field.commit(target.value)
  }

  function toggleOption(opt: string): void {
    // One-shot local: built from the current selection, mutated, then spread
    // into the commit payload. Never read reactively.
    // eslint-disable-next-line svelte/prefer-svelte-reactivity -- local helper
    const set = new Set(multiValue)
    if (set.has(opt)) set.delete(opt)
    else set.add(opt)
    void field.commit(Array.from(set))
  }

  let clearPending = $state(false)
  // Optimistic clear: snapshot → set the field to its empty form → await
  // ClearPageProperty (which removes the frontmatter key) → revert + report on
  // failure. Mirrors the optimisticField commit skeleton but with the clear IPC.
  async function clearField(): Promise<void> {
    if (clearPending) return
    const prev = wire
    const cleared = toWire(undefined)
    field.value = cleared
    clearPending = true
    onError('')
    try {
      await ClearPageProperty(
        locator.notebook,
        locator.section,
        locator.page,
        value.name
      )
      // Advance the field's revert snapshot so a later failed commit reverts
      // to the cleared state, not the pre-clear value.
      field.markPersisted(cleared)
      onChanged()
    } catch (e) {
      onError(coerceIPCError(e).message)
      // Same write-then-error concern as optimisticField: resync from disk
      // rather than assuming the clear never landed.
      if (onResync) onResync()
      else field.value = prev
    } finally {
      clearPending = false
      // Clear button unmounts when canClear becomes false; focus the control.
      void tick().then(() => {
        const el = document.getElementById(fieldId)
        if (el instanceof HTMLElement) el.focus()
      })
    }
  }

  let isPending = $derived(field.pending || clearPending)
  let canClear = $derived(value.isSet && !isPending)
</script>

<div class="field" class:mismatched>
  <div class="label-row">
    <label id={`${fieldId}-label`} for={fieldId} class="label">
      {value.label || value.name}
      {#if value.required}
        <span class="req" aria-hidden="true">*</span>
        <!-- group containers cannot take aria-required; announce via label -->
        <span class="sr-only"> (required)</span>
      {/if}
    </label>
    {#if canClear}
      <button
        type="button"
        class="clear-btn"
        aria-label="Clear {value.label || value.name}"
        title="Clear"
        onclick={clearField}
      >
        <span class="material-symbols-outlined" aria-hidden="true">close</span>
      </button>
    {/if}
  </div>

  {#if mismatched}
    <p class="warn" id={`${fieldId}-warn`}>
      Value doesn't fit this type — kept as-is.
    </p>
  {/if}

  <div class="control">
    {#if value.type === 'page' || value.type === 'pages'}
      <PageLinkField
        value={relationWire}
        multi={value.type === 'pages'}
        {target}
        label={value.label || value.name}
        {fieldId}
        disabled={isPending}
        {mismatched}
        required={isRequired}
        onCommit={(next) => void field.commit(next)}
      />
    {:else if value.type === 'text'}
      <input
        id={fieldId}
        type="text"
        class="input"
        value={textValue}
        onchange={commitText}
        onfocus={onFocus}
        onblur={onBlur}
        disabled={isPending}
        aria-required={isRequired}
        aria-describedby={mismatched ? `${fieldId}-warn` : undefined}
      />
    {:else if value.type === 'number'}
      <input
        id={fieldId}
        type="number"
        class="input"
        value={numberValue}
        onchange={commitNumber}
        onfocus={onFocus}
        onblur={onBlur}
        disabled={isPending}
        aria-required={isRequired}
        min={min ?? undefined}
        max={max ?? undefined}
        aria-describedby={mismatched ? `${fieldId}-warn` : undefined}
      />
    {:else if value.type === 'date'}
      <input
        id={fieldId}
        type="date"
        class="input"
        value={textValue}
        onchange={commitText}
        onfocus={onFocus}
        onblur={onBlur}
        disabled={isPending}
        aria-required={isRequired}
        aria-describedby={mismatched ? `${fieldId}-warn` : undefined}
      />
    {:else if value.type === 'datetime'}
      <input
        id={fieldId}
        type="datetime-local"
        class="input"
        value={textValue}
        onchange={commitText}
        onfocus={onFocus}
        onblur={onBlur}
        disabled={isPending}
        aria-required={isRequired}
        aria-describedby={mismatched ? `${fieldId}-warn` : undefined}
      />
    {:else if value.type === 'checkbox'}
      <button
        id={fieldId}
        type="button"
        role="switch"
        aria-checked={checked}
        aria-required={isRequired}
        aria-label={value.label || value.name}
        class="switch"
        class:on={checked}
        onclick={() => void field.commit(!checked)}
        onfocus={onFocus}
        onblur={onBlur}
        disabled={isPending}
      >
        <span class="knob" aria-hidden="true"></span>
      </button>
    {:else if value.type === 'select'}
      <select
        id={fieldId}
        class="input"
        value={textValue}
        onchange={commitSelect}
        onfocus={onFocus}
        onblur={onBlur}
        disabled={isPending}
        aria-required={isRequired}
        aria-describedby={mismatched ? `${fieldId}-warn` : undefined}
      >
        <option value="">—</option>
        {#each value.options ?? [] as opt (opt)}
          <option value={opt}>{opt}</option>
        {/each}
      </select>
    {:else if value.type === 'multiselect'}
      {#if hasOptions}
        <!-- fieldset is not HTML-labelable; name it via aria-labelledby. -->
        <div
          id={fieldId}
          class="chips"
          role="group"
          aria-labelledby={`${fieldId}-label`}
          aria-describedby={mismatched ? `${fieldId}-warn` : undefined}
        >
          {#each value.options ?? [] as opt (opt)}
            <button
              type="button"
              class="chip"
              class:sel={multiValue.includes(opt)}
              aria-pressed={multiValue.includes(opt)}
              onclick={() => toggleOption(opt)}
              disabled={isPending}
            >
              {opt}
            </button>
          {/each}
        </div>
      {:else}
        <!-- Free-text multiselect: chips for stored values (no join/split
             round-trip) + an add input so commas inside a value survive. -->
        <div
          id={fieldId}
          class="chips free-multi"
          role="group"
          aria-labelledby={`${fieldId}-label`}
          aria-describedby={mismatched ? `${fieldId}-warn` : undefined}
        >
          {#each multiValue as opt (opt)}
            <button
              type="button"
              class="chip sel"
              aria-label="Remove {opt}"
              onclick={() => removeMultiValue(opt)}
              disabled={isPending}
            >
              {opt}
              <span class="material-symbols-outlined chip-x" aria-hidden="true"
                >close</span
              >
            </button>
          {/each}
          <input
            bind:this={freeMultiInputRef}
            type="text"
            class="input free-multi-input"
            value={freeMultiDraft}
            aria-required={isRequired || undefined}
            oninput={(e) => (freeMultiDraft = e.currentTarget.value)}
            onkeydown={onFreeMultiKeydown}
            onchange={commitFreeMultiAdd}
            onfocus={onFocus}
            onblur={onBlur}
            disabled={isPending}
            aria-label="Add {value.label || value.name}"
            placeholder="Add value…"
          />
        </div>
      {/if}
    {:else}
      <input
        id={fieldId}
        type="text"
        class="input"
        value={textValue}
        onchange={commitText}
        onfocus={onFocus}
        onblur={onBlur}
        disabled={isPending}
        aria-required={isRequired}
      />
    {/if}
  </div>
</div>

<style>
  .field {
    display: flex;
    flex-direction: column;
    gap: 0.2rem;
    min-width: 0;
  }
  .label-row {
    display: flex;
    align-items: center;
    gap: 0.25rem;
  }
  .label-row .label {
    flex: 1 1 auto;
    min-width: 0;
  }
  .clear-btn {
    flex: 0 0 auto;
    display: inline-flex;
    align-items: center;
    border: 0;
    background: transparent;
    color: var(--color-text-muted);
    padding: 0.05rem;
    border-radius: 0.25rem;
    cursor: pointer;
    /* Resting opacity stays low so a grid of set fields reads as glanceable
       reference (not a row of × marks), but is high enough to be scannable,
       and focus-within reveals it for keyboard users tabbing into the
       control — the old hover-only rule hid it whenever the mouse wasn't
       over the field, which is the common case in the peek. */
    opacity: 0.5;
    transition:
      opacity 120ms var(--transition-standard),
      color 120ms var(--transition-standard);
  }
  .clear-btn .material-symbols-outlined {
    font-size: var(--text-icon-sm);
  }
  .field:hover .clear-btn,
  .field:focus-within .clear-btn,
  .clear-btn:focus-visible {
    opacity: 1;
  }
  .clear-btn:hover {
    color: var(--color-status-danger);
  }
  .clear-btn:focus-visible {
    outline: 2px solid var(--color-border-focus);
    outline-offset: 1px;
  }
  .label {
    font-size: var(--text-type-xs);
    color: var(--color-text-muted);
    font-weight: 600;
  }
  .req {
    color: var(--color-status-warn);
    margin-left: 0.1rem;
  }
  .input {
    width: 100%;
    background: var(--color-surface-app);
    border: 1px solid var(--color-surface-panel-border);
    color: var(--color-text-primary);
    border-radius: 0.375rem;
    padding: 0.3rem 0.5rem;
    font-size: var(--text-type-sm);
    line-height: 1.4;
    transition: background-color 120ms var(--transition-standard);
  }
  .input:hover:not(:disabled) {
    background: var(--color-hover);
  }
  .input:focus-visible {
    outline: 2px solid var(--color-border-focus);
    outline-offset: 1px;
  }
  .input:disabled {
    opacity: 0.6;
  }
  .switch {
    position: relative;
    width: 2.25rem;
    height: 1.25rem;
    border-radius: 9999px;
    border: 1px solid var(--color-surface-panel-border);
    background: var(--color-surface-app);
    cursor: pointer;
    padding: 0;
    transition: background 120ms var(--transition-standard);
  }
  .switch:focus-visible {
    outline: 2px solid var(--color-border-focus);
    outline-offset: 1px;
  }
  .switch.on {
    background: var(--color-accent-primary-glow);
    border-color: var(--color-accent-primary-start);
  }
  .knob {
    position: absolute;
    top: 0.1rem;
    left: 0.1rem;
    width: 1rem;
    height: 1rem;
    border-radius: 9999px;
    background: var(--color-text-muted);
    transition: transform 120ms var(--transition-standard);
  }
  .switch.on .knob {
    transform: translateX(1rem);
    background: var(--color-accent-primary-start);
  }
  .chips {
    display: flex;
    flex-wrap: wrap;
    gap: 0.25rem;
    margin: 0;
    padding: 0;
    border: 0;
    min-inline-size: 0;
  }
  .chip {
    border: 1px solid var(--color-surface-panel-border);
    background: var(--color-surface-app);
    color: var(--color-text-muted);
    border-radius: 9999px;
    padding: 0.15rem 0.5rem;
    font-size: var(--text-type-xs);
    cursor: pointer;
    transition: all 120ms var(--transition-standard);
  }
  .chip:hover {
    border-color: var(--color-border-focus);
  }
  .chip:focus-visible {
    outline: 2px solid var(--color-border-focus);
    outline-offset: 1px;
  }
  .chip.sel {
    background: var(--color-accent-primary-glow);
    border-color: var(--color-accent-primary-start);
    color: var(--color-accent-primary-start);
  }
  .chip-x {
    font-size: 0.85em;
    margin-left: 0.15rem;
    vertical-align: -0.1em;
  }
  .free-multi {
    align-items: center;
  }
  .free-multi-input {
    flex: 1 1 6rem;
    min-width: 5rem;
  }
  .warn {
    margin: 0;
    font-size: var(--text-type-2xs);
    color: var(--color-status-warn);
    /* Inline alert tied to its field (left rule + faint warn wash) so the
       notice reads as connected to the input in the dense peek grid, not as
       a stray colored line. */
    padding: 0.1rem 0.35rem;
    border-left: 2px solid var(--color-status-warn);
    border-radius: 0 0.2rem 0.2rem 0;
    background: color-mix(in oklch, var(--color-status-warn) 12%, transparent);
  }
  .mismatched .input,
  .mismatched .switch {
    border-color: var(--color-status-warn);
  }
  @media (prefers-reduced-motion: reduce) {
    .clear-btn,
    .input {
      transition: none;
    }
  }
</style>
