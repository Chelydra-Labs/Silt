<script lang="ts">
  // Pass / Warn / Fail badge for a contrast ratio. Icon + text (not color alone).
  import type { ContrastLevel } from '../contrast'

  type Props = {
    level: ContrastLevel
    ratio: number | null
    /** Optional compact label prefix for aria-label context. */
    label?: string
  }

  let { level, ratio, label = 'Contrast' }: Props = $props()

  const icon = $derived(
    level === 'pass' ? 'check_circle' : level === 'warn' ? 'warning' : 'error'
  )

  const levelText = $derived(
    level === 'pass' ? 'Pass' : level === 'warn' ? 'Warn' : 'Fail'
  )

  const ratioText = $derived(
    ratio === null || !Number.isFinite(ratio) ? '—' : `${ratio.toFixed(1)}:1`
  )

  const aria = $derived(
    `${label}: ${levelText}, ${ratio === null ? 'unknown ratio' : `${ratio.toFixed(2)} to 1`}`
  )

  const tone = $derived(
    level === 'pass'
      ? 'text-status-success border-status-success/30 bg-status-success/10'
      : level === 'warn'
        ? 'text-status-warn border-status-warn/30 bg-status-warn/10'
        : 'text-error border-error-border bg-error-bg'
  )
</script>

<span
  class="inline-flex items-center gap-1 rounded-md border px-1.5 py-0.5 text-type-2xs font-label-sm-bold uppercase tracking-wider {tone}"
  aria-label={aria}
  title={aria}
>
  <span class="material-symbols-outlined text-icon-sm" aria-hidden="true"
    >{icon}</span
  >
  <span>{levelText}</span>
  <span class="font-mono normal-case tracking-normal opacity-90"
    >{ratioText}</span
  >
</span>
