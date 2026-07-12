<script lang="ts">
  // OKLCH-first color field: swatch + text input + popover with L/C/H sliders.
  // Popover opens on activate (click/Enter/Space), not mere focus. Keyboard
  // sliders use aria-valuetext for screen-reader feedback.
  import { onMount } from 'svelte'
  import { clampL, formatOklch, toHex, toOklch, type Oklch } from '../color'
  import { classifyContrast, contrastRatioWCAG } from '../contrast'
  import ContrastBadge from './ContrastBadge.svelte'

  type Props = {
    label: string
    value: string
    onchange: (next: string) => void
    /** When set, shows a small Reset control that calls this (parent decides dirty). */
    onReset?: () => void
    bgForContrast?: string
    disabled?: boolean
    id?: string
  }

  let {
    label,
    value,
    onchange,
    onReset,
    bgForContrast,
    disabled = false,
    id
  }: Props = $props()

  // Stable instance id when parent doesn't supply one (random once per mount).
  const autoId = `oklch-field-${Math.random().toString(36).slice(2, 10)}`
  const fieldId = $derived(id ?? autoId)
  const popoverId = $derived(`${fieldId}-popover`)

  let open = $state(false)
  let textDraft = $state('')
  let rootEl: HTMLDivElement | undefined = $state()
  let swatchBtn: HTMLButtonElement | undefined = $state()

  $effect(() => {
    textDraft = value
  })

  const lch = $derived(toOklch(value))
  const swatchHex = $derived(toHex(value) ?? value)
  const contrastRatio = $derived(
    bgForContrast ? contrastRatioWCAG(value, bgForContrast) : null
  )
  const contrastLevel = $derived(
    bgForContrast ? classifyContrast(contrastRatio, true) : null
  )

  function openPopover() {
    if (disabled) return
    open = true
  }

  function closePopover() {
    open = false
    swatchBtn?.focus()
  }

  function togglePopover() {
    if (open) closePopover()
    else openPopover()
  }

  function onSwatchKey(e: KeyboardEvent) {
    if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault()
      togglePopover()
    } else if (e.key === 'Escape' && open) {
      e.preventDefault()
      closePopover()
    }
  }

  function commitText() {
    const trimmed = textDraft.trim()
    if (!trimmed) {
      textDraft = value
      return
    }
    // Accept hex / oklch / rgb if culori can parse it.
    if (!toOklch(trimmed) && !toHex(trimmed)) {
      textDraft = value
      return
    }
    onchange(trimmed)
  }

  function onTextKey(e: KeyboardEvent) {
    if (e.key === 'Enter') {
      e.preventDefault()
      commitText()
    } else if (e.key === 'Escape') {
      textDraft = value
      ;(e.currentTarget as HTMLInputElement).blur()
    }
  }

  function applyLch(next: Oklch) {
    const formatted = formatOklch({
      L: clampL(next.L),
      C: Math.max(0, next.C),
      H: ((next.H % 360) + 360) % 360
    })
    onchange(formatted)
  }

  function setL(v: number) {
    if (!lch) return
    applyLch({ ...lch, L: v / 100 })
  }
  function setC(v: number) {
    if (!lch) return
    applyLch({ ...lch, C: v / 100 })
  }
  function setH(v: number) {
    if (!lch) return
    applyLch({ ...lch, H: v })
  }

  // L track: dark→light at current C/H. C: grey→vivid. H: full hue wheel.
  const lTrack = $derived.by(() => {
    if (!lch) return undefined
    const c = lch.C.toFixed(4)
    const h = lch.H.toFixed(2)
    return `linear-gradient(to right, oklch(0 ${c} ${h}), oklch(1 ${c} ${h}))`
  })
  const cTrack = $derived.by(() => {
    if (!lch) return undefined
    const l = lch.L.toFixed(4)
    const h = lch.H.toFixed(2)
    return `linear-gradient(to right, oklch(${l} 0 ${h}), oklch(${l} 0.4 ${h}))`
  })
  const hTrack = $derived(
    'linear-gradient(to right, oklch(0.7 0.15 0), oklch(0.7 0.15 60), oklch(0.7 0.15 120), oklch(0.7 0.15 180), oklch(0.7 0.15 240), oklch(0.7 0.15 300), oklch(0.7 0.15 360))'
  )

  function onDocPointer(e: PointerEvent) {
    if (!open || !rootEl) return
    if (e.target instanceof Node && rootEl.contains(e.target)) return
    closePopover()
  }

  function onDocKey(e: KeyboardEvent) {
    if (!open) return
    if (e.key === 'Escape') {
      e.preventDefault()
      e.stopPropagation()
      closePopover()
    }
  }

  onMount(() => {
    document.addEventListener('pointerdown', onDocPointer, true)
    document.addEventListener('keydown', onDocKey, true)
    return () => {
      document.removeEventListener('pointerdown', onDocPointer, true)
      document.removeEventListener('keydown', onDocKey, true)
    }
  })
</script>

<div class="oklch-field flex flex-col gap-1.5" bind:this={rootEl}>
  <div class="flex items-center justify-between gap-2">
    <label class="text-type-sm font-label-sm text-text-primary" for={fieldId}
      >{label}</label
    >
    <div class="flex items-center gap-2">
      {#if onReset}
        <button
          type="button"
          class="text-type-2xs font-label-sm text-text-muted hover:text-text-primary underline-offset-2 hover:underline bg-transparent border-none cursor-pointer px-0 py-0 focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-primary-start/60 rounded-sm disabled:opacity-40"
          aria-label={`Reset ${label}`}
          {disabled}
          onclick={() => onReset()}
        >
          Reset
        </button>
      {/if}
      {#if contrastLevel !== null}
        <ContrastBadge
          level={contrastLevel}
          ratio={contrastRatio}
          label={`${label} contrast`}
        />
      {/if}
    </div>
  </div>

  <div class="flex items-center gap-2">
    <button
      bind:this={swatchBtn}
      type="button"
      class="relative h-9 w-9 flex-shrink-0 rounded-md border border-surface-panel-border shadow-sm cursor-pointer focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-primary-start/60 disabled:opacity-40 disabled:cursor-not-allowed"
      style="background-color: {swatchHex}"
      aria-label={`${label} color swatch`}
      aria-haspopup="dialog"
      aria-expanded={open}
      aria-controls={popoverId}
      {disabled}
      onclick={togglePopover}
      onkeydown={onSwatchKey}
    ></button>

    <input
      id={fieldId}
      type="text"
      class="flex-1 min-w-0 h-9 px-2.5 rounded-md bg-surface-panel border border-surface-panel-border text-text-primary font-mono text-type-xs focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-primary-start/60 focus-visible:border-border-focus disabled:opacity-40"
      bind:value={textDraft}
      {disabled}
      spellcheck="false"
      autocomplete="off"
      aria-label={`${label} color value`}
      onblur={commitText}
      onkeydown={onTextKey}
    />
  </div>

  {#if open && lch}
    <div
      id={popoverId}
      role="dialog"
      aria-label={`${label} color picker`}
      class="mt-1 rounded-lg border border-surface-panel-border bg-surface-popover p-3 shadow-md space-y-3 z-10"
    >
      <div
        class="h-8 w-full rounded-md border border-surface-panel-border"
        style="background-color: {swatchHex}"
        aria-hidden="true"
      ></div>

      <div class="space-y-1">
        <div class="flex justify-between text-type-2xs font-label-sm">
          <label for={`${fieldId}-l`}>Lightness</label>
          <span class="font-mono text-text-muted"
            >{(lch.L * 100).toFixed(1)}%</span
          >
        </div>
        <input
          id={`${fieldId}-l`}
          type="range"
          min="0"
          max="100"
          step="0.1"
          value={lch.L * 100}
          aria-valuetext={`${(lch.L * 100).toFixed(1)} percent lightness`}
          class="oklch-slider w-full"
          style="--track: {lTrack}"
          oninput={(e) =>
            setL(Number((e.currentTarget as HTMLInputElement).value))}
        />
      </div>

      <div class="space-y-1">
        <div class="flex justify-between text-type-2xs font-label-sm">
          <label for={`${fieldId}-c`}>Chroma</label>
          <span class="font-mono text-text-muted">{lch.C.toFixed(3)}</span>
        </div>
        <input
          id={`${fieldId}-c`}
          type="range"
          min="0"
          max="40"
          step="0.1"
          value={lch.C * 100}
          aria-valuetext={`${lch.C.toFixed(3)} chroma`}
          class="oklch-slider w-full"
          style="--track: {cTrack}"
          oninput={(e) =>
            setC(Number((e.currentTarget as HTMLInputElement).value))}
        />
      </div>

      <div class="space-y-1">
        <div class="flex justify-between text-type-2xs font-label-sm">
          <label for={`${fieldId}-h`}>Hue</label>
          <span class="font-mono text-text-muted">{lch.H.toFixed(0)}°</span>
        </div>
        <input
          id={`${fieldId}-h`}
          type="range"
          min="0"
          max="360"
          step="1"
          value={lch.H}
          aria-valuetext={`${lch.H.toFixed(0)} degrees hue`}
          class="oklch-slider w-full"
          style="--track: {hTrack}"
          oninput={(e) =>
            setH(Number((e.currentTarget as HTMLInputElement).value))}
        />
      </div>
    </div>
  {:else if open && !lch}
    <div
      id={popoverId}
      role="dialog"
      aria-label={`${label} color picker`}
      class="mt-1 rounded-lg border border-surface-panel-border bg-surface-popover p-3 text-type-sm text-text-muted"
    >
      Enter a valid hex or oklch() color to use the sliders.
    </div>
  {/if}
</div>

<style>
  .oklch-slider {
    -webkit-appearance: none;
    appearance: none;
    height: 0.5rem;
    border-radius: 9999px;
    background: var(--track, var(--color-surface-panel));
    cursor: pointer;
  }
  .oklch-slider:focus {
    outline: none;
  }
  .oklch-slider:focus-visible {
    box-shadow: 0 0 0 2px
      color-mix(in oklch, var(--color-accent-primary-start) 60%, transparent);
  }
  .oklch-slider::-webkit-slider-thumb {
    -webkit-appearance: none;
    appearance: none;
    width: 1rem;
    height: 1rem;
    border-radius: 9999px;
    background: var(--color-surface-app-text);
    border: 2px solid var(--color-surface-panel);
    box-shadow: 0 1px 2px
      color-mix(in oklch, var(--color-surface-app) 40%, transparent);
  }
  .oklch-slider::-moz-range-thumb {
    width: 1rem;
    height: 1rem;
    border-radius: 9999px;
    background: var(--color-surface-app-text);
    border: 2px solid var(--color-surface-panel);
    box-shadow: 0 1px 2px
      color-mix(in oklch, var(--color-surface-app) 40%, transparent);
  }
</style>
