<script lang="ts" generics="T extends string | number">
  import InfoTooltip from './InfoTooltip.svelte'

  interface PresetOption<V extends string | number> {
    value: V
    label: string
    description: string
  }

  interface Props {
    label: string
    /** Plain-language tooltip text. */
    tooltipText: string
    /** Technical tooltip line. */
    tooltipTechnical?: string
    options: PresetOption<T>[]
    value: T
    /** Label for the Advanced raw input. */
    customLabel?: string
    customMin?: number
    customMax?: number
    customStep?: number
    customSuffix?: string
    /** When true, Advanced uses a range slider instead of number input. */
    customRange?: boolean
    /** When set, Advanced uses a select with these string options. */
    customSelectOptions?: { value: string; label: string }[]
    onchange: (value: T) => void
  }

  let {
    label,
    tooltipText,
    tooltipTechnical,
    options,
    value,
    customLabel,
    customMin,
    customMax,
    customStep,
    customSuffix,
    customRange = false,
    customSelectOptions,
    onchange
  }: Props = $props()

  // Stable ids derived from the (static) label string for a11y wiring.
  let groupId = $derived(`preset-${label.replace(/\s+/g, '-').toLowerCase()}`)
  let labelId = $derived(`${groupId}-label`)

  let matched = $derived(options.find((o) => o.value === value) ?? null)
  let isCustom = $derived(matched === null)
  let description = $derived(
    matched?.description ?? 'Custom value — set below in Advanced.'
  )

  // Hide Advanced when neither numeric bounds nor select options are provided.
  let showAdvanced = $derived(
    customSelectOptions != null ||
      customMin != null ||
      customMax != null ||
      customRange
  )

  function selectPreset(v: T) {
    onchange(v)
  }

  function onRadioKeydown(e: KeyboardEvent, index: number) {
    if (
      e.key !== 'ArrowRight' &&
      e.key !== 'ArrowLeft' &&
      e.key !== 'ArrowDown' &&
      e.key !== 'ArrowUp'
    ) {
      return
    }
    e.preventDefault()
    const dir = e.key === 'ArrowRight' || e.key === 'ArrowDown' ? 1 : -1
    const next = (index + dir + options.length) % options.length
    selectPreset(options[next].value)
    // Focus the next radio after Svelte re-renders.
    queueMicrotask(() => {
      const el = document.getElementById(`${groupId}-opt-${next}`)
      el?.focus()
    })
  }

  function onNumberInput(raw: string) {
    const n = Number(raw)
    if (Number.isNaN(n)) return
    onchange(n as T)
  }

  function onSelectInput(raw: string) {
    onchange(raw as T)
  }
</script>

<div class="preset-control flex flex-col gap-2">
  <div class="flex items-center gap-1.5">
    <span
      id={labelId}
      class="text-text-muted text-type-2xs font-semibold uppercase tracking-wider"
    >
      {label}
    </span>
    <InfoTooltip
      text={tooltipText}
      technical={tooltipTechnical}
      label="What is {label}?"
    />
  </div>

  <div
    role="radiogroup"
    aria-labelledby={labelId}
    class="flex flex-wrap gap-1.5"
  >
    {#each options as opt, i (String(opt.value))}
      {@const selected = !isCustom && opt.value === value}
      <button
        type="button"
        id="{groupId}-opt-{i}"
        role="radio"
        aria-checked={selected}
        tabindex={selected || (isCustom && i === 0) ? 0 : -1}
        class="px-3 py-1.5 rounded-lg border text-type-xs font-label-sm-bold transition-all duration-150 cursor-pointer focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-primary-start/60 {selected
          ? 'bg-accent-primary-glow/15 border-accent-primary-start text-accent-primary-start shadow-sm'
          : 'bg-surface-panel/40 border-surface-panel-border text-text-muted hover:border-border-active hover:text-text-primary'}"
        onclick={() => selectPreset(opt.value)}
        onkeydown={(e) => onRadioKeydown(e, i)}
      >
        {opt.label}
      </button>
    {/each}
    {#if isCustom}
      <span
        class="px-3 py-1.5 rounded-lg border border-dashed border-status-warn/50 bg-status-warn/10 text-status-warn text-type-xs font-label-sm-bold"
        aria-hidden="true"
      >
        Custom
      </span>
    {/if}
  </div>

  <p class="text-type-xs text-text-muted font-label-sm leading-snug m-0">
    {description}
  </p>

  {#if showAdvanced}
    <details class="group mt-0.5">
      <summary
        class="text-type-2xs font-semibold uppercase tracking-wider text-text-muted cursor-pointer select-none list-none flex items-center gap-1 hover:text-text-primary"
      >
        <span
          class="material-symbols-outlined text-icon-sm transition-transform group-open:rotate-90"
          aria-hidden="true">chevron_right</span
        >
        Advanced
      </summary>
      <div class="mt-2 flex flex-col gap-1.5 pl-1">
        {#if customLabel}
          <label class="text-type-2xs text-text-muted" for="{groupId}-custom">
            {customLabel}{customSuffix ? ` (${customSuffix})` : ''}
          </label>
        {/if}
        {#if customSelectOptions}
          <select
            id="{groupId}-custom"
            class="w-full max-w-xs rounded-lg border border-surface-panel-border bg-surface-panel/40 px-2.5 py-1.5 text-type-sm text-text-primary focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-primary-start/60"
            value={String(value)}
            onchange={(e) =>
              onSelectInput((e.currentTarget as HTMLSelectElement).value)}
          >
            {#each customSelectOptions as so (so.value)}
              <option value={so.value}>{so.label}</option>
            {/each}
          </select>
        {:else if customRange}
          <div class="flex items-center gap-3 max-w-md">
            <input
              id="{groupId}-custom"
              type="range"
              class="flex-1 accent-accent-primary-start"
              min={customMin ?? 0}
              max={customMax ?? 1}
              step={customStep ?? 0.05}
              value={typeof value === 'number' ? value : Number(value)}
              onchange={(e) =>
                onNumberInput((e.currentTarget as HTMLInputElement).value)}
            />
            <span
              class="text-type-xs text-text-muted tabular-nums min-w-[3rem] text-right"
            >
              {typeof value === 'number' ? value.toFixed(2) : value}
            </span>
          </div>
        {:else}
          <input
            id="{groupId}-custom"
            type="number"
            class="w-full max-w-xs rounded-lg border border-surface-panel-border bg-surface-panel/40 px-2.5 py-1.5 text-type-sm text-text-primary focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-primary-start/60"
            min={customMin}
            max={customMax}
            step={customStep ?? 1}
            value={typeof value === 'number' ? value : Number(value) || ''}
            onchange={(e) =>
              onNumberInput((e.currentTarget as HTMLInputElement).value)}
          />
        {/if}
      </div>
    </details>
  {/if}
</div>
