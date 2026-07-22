<script lang="ts">
  // OKLCH-first color field: swatch + text input + popover with 2D LC plane,
  // hue strip, and advanced L/C/H channel sliders (#528).
  // Popover opens on activate (click/Enter/Space), not mere focus. Keyboard
  // sliders / plane use aria-valuetext for screen-reader feedback.
  import { onMount } from 'svelte'
  import { clampL, formatOklch, toHex, toOklch, type Oklch } from '../color'
  import { classifyContrast, contrastRatioWCAG } from '../contrast'
  import { describeOklch } from '../oklchDescribe'
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
  let invalid = $state(false)
  let rootEl: HTMLDivElement | undefined = $state()
  let swatchBtn: HTMLButtonElement | undefined = $state()
  let planeDragging = $state(false)
  let hueDragging = $state(false)

  $effect(() => {
    textDraft = value
    invalid = false
  })

  const lch = $derived(toOklch(value))
  const swatchHex = $derived(toHex(value) ?? value)
  const contrastRatio = $derived(
    bgForContrast ? contrastRatioWCAG(value, bgForContrast) : null
  )
  const contrastLevel = $derived(
    bgForContrast ? classifyContrast(contrastRatio, true) : null
  )
  const valueText = $derived(lch ? describeOklch(lch) : '')

  // Chroma range for the plane (matches advanced C slider max of 0.4).
  const C_MAX = 0.4

  function openPopover() {
    if (disabled) return
    open = true
  }

  function closePopover() {
    open = false
    planeDragging = false
    hueDragging = false
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
      invalid = false
      return
    }
    // Accept hex / oklch / rgb if culori can parse it.
    if (!toOklch(trimmed) && !toHex(trimmed)) {
      // Keep typed value so the user can fix it; surface invalid state.
      invalid = true
      return
    }
    invalid = false
    onchange(trimmed)
  }

  function onTextKey(e: KeyboardEvent) {
    if (e.key === 'Enter') {
      e.preventDefault()
      commitText()
    } else if (e.key === 'Escape') {
      textDraft = value
      invalid = false
      ;(e.currentTarget as HTMLInputElement).blur()
    }
  }

  function onTextInput() {
    if (invalid) invalid = false
  }

  function applyLch(next: Oklch) {
    const formatted = formatOklch({
      L: clampL(next.L),
      C: Math.max(0, next.C),
      H: ((next.H % 360) + 360) % 360,
      // Preserve authored alpha on slider edits (formatOklch omits alpha === 1).
      ...(next.alpha !== undefined && next.alpha !== 1
        ? { alpha: next.alpha }
        : {})
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

  function clamp01(n: number): number {
    return n < 0 ? 0 : n > 1 ? 1 : n
  }

  function applyPlanePoint(e: PointerEvent, el: HTMLElement) {
    if (!lch) return
    const rect = el.getBoundingClientRect()
    if (rect.width <= 0 || rect.height <= 0) return
    const x = clamp01((e.clientX - rect.left) / rect.width)
    const y = clamp01((e.clientY - rect.top) / rect.height)
    // X = chroma 0–0.4, Y = lightness 1→0 (top light, bottom dark).
    applyLch({ ...lch, C: x * C_MAX, L: 1 - y })
  }

  function onPlanePointerDown(e: PointerEvent) {
    if (!lch || disabled) return
    e.preventDefault()
    const el = e.currentTarget as HTMLElement
    el.setPointerCapture?.(e.pointerId)
    planeDragging = true
    applyPlanePoint(e, el)
  }

  function onPlanePointerMove(e: PointerEvent) {
    if (!planeDragging || !lch) return
    applyPlanePoint(e, e.currentTarget as HTMLElement)
  }

  function onPlanePointerUp(e: PointerEvent) {
    if (!planeDragging) return
    planeDragging = false
    try {
      ;(e.currentTarget as HTMLElement).releasePointerCapture?.(e.pointerId)
    } catch {
      // already released
    }
  }

  function onPlaneKey(e: KeyboardEvent) {
    if (!lch || disabled) return
    const fine = e.shiftKey
    const lStep = fine ? 0.005 : 0.02
    const cStep = fine ? 0.002 : 0.01
    let next: Oklch
    switch (e.key) {
      case 'ArrowUp':
        next = { ...lch, L: clampL(lch.L + lStep) }
        break
      case 'ArrowDown':
        next = { ...lch, L: clampL(lch.L - lStep) }
        break
      case 'ArrowRight':
        next = { ...lch, C: Math.min(C_MAX, Math.max(0, lch.C + cStep)) }
        break
      case 'ArrowLeft':
        next = { ...lch, C: Math.min(C_MAX, Math.max(0, lch.C - cStep)) }
        break
      default:
        return
    }
    e.preventDefault()
    applyLch(next)
  }

  function applyHuePoint(e: PointerEvent, el: HTMLElement) {
    if (!lch) return
    const rect = el.getBoundingClientRect()
    if (rect.width <= 0) return
    const x = clamp01((e.clientX - rect.left) / rect.width)
    applyLch({ ...lch, H: x * 360 })
  }

  function onHuePointerDown(e: PointerEvent) {
    if (!lch || disabled) return
    e.preventDefault()
    const el = e.currentTarget as HTMLElement
    el.setPointerCapture?.(e.pointerId)
    hueDragging = true
    applyHuePoint(e, el)
  }

  function onHuePointerMove(e: PointerEvent) {
    if (!hueDragging || !lch) return
    applyHuePoint(e, e.currentTarget as HTMLElement)
  }

  function onHuePointerUp(e: PointerEvent) {
    if (!hueDragging) return
    hueDragging = false
    try {
      ;(e.currentTarget as HTMLElement).releasePointerCapture?.(e.pointerId)
    } catch {
      // already released
    }
  }

  function onHueKey(e: KeyboardEvent) {
    if (!lch || disabled) return
    const step = e.shiftKey ? 1 : 5
    let nextH: number
    switch (e.key) {
      case 'ArrowLeft':
      case 'ArrowDown':
        nextH = lch.H - step
        break
      case 'ArrowRight':
      case 'ArrowUp':
        nextH = lch.H + step
        break
      case 'Home':
        nextH = 0
        break
      case 'End':
        nextH = 360
        break
      default:
        return
    }
    e.preventDefault()
    applyLch({ ...lch, H: ((nextH % 360) + 360) % 360 })
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

  // 2D LC plane: X = chroma, Y = lightness (top light). Fixed current hue.
  const lcPlaneBg = $derived.by(() => {
    if (!lch) return undefined
    const h = lch.H.toFixed(2)
    return [
      `linear-gradient(to bottom, oklch(1 0 ${h} / 0), oklch(0 0 0))`,
      `linear-gradient(to right, oklch(0.85 0 ${h}), oklch(0.75 0.4 ${h}))`
    ].join(', ')
  })

  const planeThumbStyle = $derived.by(() => {
    if (!lch) return ''
    const x = Math.min(100, Math.max(0, (lch.C / C_MAX) * 100))
    const y = Math.min(100, Math.max(0, (1 - lch.L) * 100))
    return `left: ${x}%; top: ${y}%;`
  })

  const hueThumbStyle = $derived.by(() => {
    if (!lch) return ''
    const x = Math.min(100, Math.max(0, (lch.H / 360) * 100))
    return `left: ${x}%;`
  })

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
      class={invalid
        ? 'flex-1 min-w-0 h-9 px-2.5 rounded-md bg-surface-panel border border-status-danger text-text-primary font-mono text-type-xs focus:outline-none focus-visible:ring-2 focus-visible:ring-status-danger/60 ring-1 ring-status-danger disabled:opacity-40'
        : 'flex-1 min-w-0 h-9 px-2.5 rounded-md bg-surface-panel border border-surface-panel-border text-text-primary font-mono text-type-xs focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-primary-start/60 focus-visible:border-border-focus disabled:opacity-40'}
      bind:value={textDraft}
      {disabled}
      spellcheck="false"
      autocomplete="off"
      aria-label={`${label} color value`}
      aria-invalid={invalid}
      aria-describedby={invalid ? `${fieldId}-invalid` : undefined}
      oninput={onTextInput}
      onblur={commitText}
      onkeydown={onTextKey}
    />
  </div>
  {#if invalid}
    <p
      id={`${fieldId}-invalid`}
      class="text-type-2xs font-label-sm text-status-danger"
      role="status"
      aria-live="polite"
    >
      Unrecognized color
    </p>
  {/if}

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

      <!-- 2D LC plane: primary chroma × lightness control -->
      <div class="space-y-1">
        <div class="flex justify-between text-type-2xs font-label-sm">
          <span>Lightness · Chroma</span>
          <span class="font-mono text-text-muted"
            >{(lch.L * 100).toFixed(1)}% · {lch.C.toFixed(3)}</span
          >
        </div>
        <div
          data-testid="oklch-lc-plane"
          class="oklch-lc-plane relative w-full h-32 rounded-md border border-surface-panel-border cursor-crosshair touch-none focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-primary-start/60"
          style="background: {lcPlaneBg}"
          role="slider"
          tabindex="0"
          aria-label={`${label} lightness and chroma`}
          aria-valuemin={0}
          aria-valuemax={100}
          aria-valuenow={Math.round(lch.L * 100)}
          aria-valuetext={valueText}
          aria-orientation="vertical"
          onpointerdown={onPlanePointerDown}
          onpointermove={onPlanePointerMove}
          onpointerup={onPlanePointerUp}
          onpointercancel={onPlanePointerUp}
          onkeydown={onPlaneKey}
        >
          <span
            class="oklch-plane-thumb absolute w-3.5 h-3.5 rounded-full border-2 border-white shadow-md pointer-events-none -translate-x-1/2 -translate-y-1/2"
            style="{planeThumbStyle} background-color: {swatchHex};"
            aria-hidden="true"
          ></span>
        </div>
      </div>

      <!-- Hue strip: primary hue control -->
      <div class="space-y-1">
        <div class="flex justify-between text-type-2xs font-label-sm">
          <span>Hue</span>
          <span class="font-mono text-text-muted">{lch.H.toFixed(0)}°</span>
        </div>
        <div
          data-testid="oklch-hue-strip"
          class="oklch-hue-strip relative w-full h-4 rounded-full border border-surface-panel-border cursor-pointer touch-none focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-primary-start/60"
          style="background: {hTrack}"
          role="slider"
          tabindex="0"
          aria-label={`${label} hue`}
          aria-valuemin={0}
          aria-valuemax={360}
          aria-valuenow={Math.round(lch.H)}
          aria-valuetext={valueText}
          aria-orientation="horizontal"
          onpointerdown={onHuePointerDown}
          onpointermove={onHuePointerMove}
          onpointerup={onHuePointerUp}
          onpointercancel={onHuePointerUp}
          onkeydown={onHueKey}
        >
          <span
            class="oklch-hue-thumb absolute top-1/2 w-3.5 h-3.5 rounded-full border-2 border-white shadow-md pointer-events-none -translate-x-1/2 -translate-y-1/2"
            style="{hueThumbStyle} background-color: {swatchHex};"
            aria-hidden="true"
          ></span>
        </div>
      </div>

      <!-- Advanced: numeric channel sliders for precise keyboard / AT paths -->
      <details class="oklch-advanced group">
        <summary
          class="text-type-2xs font-label-sm text-text-muted hover:text-text-primary cursor-pointer select-none list-none flex items-center gap-1.5 py-0.5 focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-primary-start/60 rounded-sm"
        >
          <span
            class="oklch-details-chevron inline-block text-[0.65rem] leading-none transition-transform"
            aria-hidden="true">▸</span
          >
          Channel sliders
        </summary>
        <div class="mt-2 space-y-3">
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
              aria-valuetext={valueText}
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
              aria-valuetext={valueText}
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
              aria-valuetext={valueText}
              class="oklch-slider w-full"
              style="--track: {hTrack}"
              oninput={(e) =>
                setH(Number((e.currentTarget as HTMLInputElement).value))}
            />
          </div>
        </div>
      </details>
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

  .oklch-plane-thumb,
  .oklch-hue-thumb {
    transition:
      left 80ms ease,
      top 80ms ease;
  }

  @media (prefers-reduced-motion: reduce) {
    .oklch-plane-thumb,
    .oklch-hue-thumb,
    .oklch-details-chevron {
      transition: none;
    }
  }

  .oklch-advanced > summary::-webkit-details-marker {
    display: none;
  }
  .oklch-advanced[open] .oklch-details-chevron {
    transform: rotate(90deg);
  }
</style>
