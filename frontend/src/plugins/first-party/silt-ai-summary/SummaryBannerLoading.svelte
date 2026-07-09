<script lang="ts">
  /**
   * SummaryBannerLoading — transient skeleton shown in the banner slot while
   * silt-ai-summary computes the content hash on a note switch (#488).
   *
   * Mounting the real SummaryBanner requires the content hash (so dismissal is
   * keyed by `${pageId}:${contentHash}` #455), and that hash is async (a SQLite
   * read + SHA-256). To avoid a stale flash of the PRIOR note's summary during
   * that window, index.ts tears the prior surface down immediately on note
   * switch and registers this placeholder only if the hash window is perceptible
   * (>100ms); for the common sub-100ms case the timer never fires and the user
   * sees no flicker. The real banner/chip replaces it the moment the hash
   * resolves.
   *
   * Intentionally inert: no controller reads, no dismiss affordance, no
   * generation — it is pure chrome reused from SummaryBanner so the slot reads
   * as the same surface kind while the real one materializes.
   */
  import type { PluginContext } from '../../sdk'

  interface Props {
    ctx: PluginContext
    onDismiss: () => void
  }
  let { ctx: _ctx, onDismiss: _onDismiss }: Props = $props()
</script>

<section class="summary-banner is-loading" aria-label="Loading AI summary">
  <header class="head">
    <span class="lead-icon material-symbols-outlined" aria-hidden="true"
      >auto_awesome</span
    >
    <div class="body">
      <div class="skeleton" aria-hidden="true">
        <div class="sk-line"></div>
        <div class="sk-line w70"></div>
      </div>
    </div>
  </header>
  <span class="sr-only" aria-live="polite">Loading summary…</span>
</section>

<style>
  /* Mirrors SummaryBanner's chrome + skeleton exactly so the loading slot is
     visually continuous with the real banner. Kept inline (not shared) because
     SummaryBanner owns its styles and this is the only other consumer. */
  .summary-banner {
    display: flex;
    flex-direction: column;
    gap: 8px;
    padding: 8px 12px;
    border-radius: 8px;
    background: color-mix(
      in srgb,
      var(--color-accent-primary-glow) 12%,
      var(--color-surface-card)
    );
    border: 1px solid
      color-mix(in srgb, var(--color-accent-primary-glow) 30%, transparent);
    color: var(--color-text-primary);
    font-size: 0.82rem;
    line-height: 1.45;
  }

  .head {
    display: flex;
    align-items: flex-start;
    gap: 8px;
  }

  .lead-icon {
    font-size: 18px;
    line-height: 1.45;
    color: var(--color-accent-primary-start);
    flex-shrink: 0;
  }

  .body {
    flex: 1;
    min-width: 0;
  }

  .skeleton {
    display: flex;
    flex-direction: column;
    gap: 6px;
    width: 100%;
    max-width: 540px;
    padding: 2px 0;
  }
  .sk-line {
    height: 10px;
    border-radius: 4px;
    background: linear-gradient(
      90deg,
      color-mix(in srgb, var(--color-accent-primary-start) 14%, transparent),
      color-mix(in srgb, var(--color-accent-primary-start) 30%, transparent),
      color-mix(in srgb, var(--color-accent-primary-start) 14%, transparent)
    );
    background-size: 200% 100%;
    animation: shimmer 1.6s linear infinite;
  }
  .sk-line.w70 {
    width: 70%;
  }

  @keyframes shimmer {
    0% {
      background-position: 200% 0;
    }
    100% {
      background-position: -200% 0;
    }
  }

  .sr-only {
    position: absolute;
    width: 1px;
    height: 1px;
    padding: 0;
    margin: -1px;
    overflow: hidden;
    clip: rect(0, 0, 0, 0);
    white-space: nowrap;
    border: 0;
  }

  @media (prefers-reduced-motion: reduce) {
    .sk-line {
      animation: none;
    }
  }
</style>
