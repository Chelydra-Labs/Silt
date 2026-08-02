<script lang="ts">
  // One typed-property editor. Dispatches the right control by `value.type`
  // and commits every edit through the optimistic pattern: snapshot → show new
  // value → await SetPageProperty → revert + report on rejection. The wire
  // value is what SetPageProperty receives; the input's displayed value is a
  // derived string/array off the optimistic field, so a rejected edit snaps
  // back to the last accepted value.
  import { untrack } from 'svelte'
  import { SetPageProperty } from '../../bindings/silt/app.js'
  import { optimisticField } from './optimisticField.svelte'
  import type { PageLocator, PagePropertyValue } from './types'

  interface Props {
    value: PagePropertyValue
    locator: PageLocator
    min?: number | null
    max?: number | null
    mismatched?: boolean
    onError: (message: string) => void
    onChanged: () => void
  }

  let {
    value,
    locator,
    min = null,
    max = null,
    mismatched = false,
    onError,
    onChanged
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
      onChanged: () => onChanged()
    })
  )

  // Re-seed when the controller refreshes the value (page switch, external
  // edit, post-commit re-sync). reset() does NOT invoke write.
  $effect(() => {
    field.reset(toWire(value.value))
  })

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

  let fieldId = $derived(`prop-${value.name}`)
  let hasOptions = $derived(!!value.options && value.options.length > 0)

  function commitText(e: Event): void {
    const target = e.currentTarget as HTMLInputElement
    void field.commit(target.value)
  }

  function commitNumber(e: Event): void {
    const target = e.currentTarget as HTMLInputElement
    const raw = target.value
    if (raw === '') return
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

  let isPending = $derived(field.pending)
</script>

<div class="field" class:mismatched>
  <label for={fieldId} class="label">
    {value.label || value.name}
    {#if value.required}<span class="req" aria-hidden="true">*</span>{/if}
  </label>

  {#if mismatched}
    <p class="warn" id={`${fieldId}-warn`}>
      Value doesn't fit this type — kept as-is.
    </p>
  {/if}

  <div class="control">
    {#if value.type === 'text' || value.type === 'page'}
      <input
        id={fieldId}
        type="text"
        class="input"
        value={textValue}
        onchange={commitText}
        disabled={isPending}
        aria-describedby={mismatched ? `${fieldId}-warn` : undefined}
      />
    {:else if value.type === 'pages'}
      <input
        id={fieldId}
        type="text"
        class="input"
        value={textValue}
        onchange={commitText}
        disabled={isPending}
        placeholder="page path, another page"
        aria-describedby={mismatched ? `${fieldId}-warn` : undefined}
      />
    {:else if value.type === 'number'}
      <input
        id={fieldId}
        type="number"
        class="input"
        value={numberValue}
        onchange={commitNumber}
        disabled={isPending}
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
        disabled={isPending}
        aria-describedby={mismatched ? `${fieldId}-warn` : undefined}
      />
    {:else if value.type === 'datetime'}
      <input
        id={fieldId}
        type="datetime-local"
        class="input"
        value={textValue}
        onchange={commitText}
        disabled={isPending}
        aria-describedby={mismatched ? `${fieldId}-warn` : undefined}
      />
    {:else if value.type === 'checkbox'}
      <button
        id={fieldId}
        type="button"
        role="switch"
        aria-checked={checked}
        aria-label={value.label || value.name}
        class="switch"
        class:on={checked}
        onclick={() => void field.commit(!checked)}
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
        disabled={isPending}
        aria-describedby={mismatched ? `${fieldId}-warn` : undefined}
      >
        <option value="">—</option>
        {#each value.options ?? [] as opt (opt)}
          <option value={opt}>{opt}</option>
        {/each}
      </select>
    {:else if value.type === 'multiselect'}
      {#if hasOptions}
        <div class="chips" role="group" aria-label={value.label || value.name}>
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
        <input
          id={fieldId}
          type="text"
          class="input"
          value={textValue}
          onchange={commitText}
          disabled={isPending}
          placeholder="value, another value"
          aria-describedby={mismatched ? `${fieldId}-warn` : undefined}
        />
      {/if}
    {:else}
      <input
        id={fieldId}
        type="text"
        class="input"
        value={textValue}
        onchange={commitText}
        disabled={isPending}
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
  .warn {
    font-size: var(--text-type-2xs);
    color: var(--color-status-warn);
  }
  .mismatched .input,
  .mismatched .switch {
    border-color: var(--color-status-warn);
  }
</style>
