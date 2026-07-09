<script lang="ts">
  /**
   * SummaryBannerLoading — transient skeleton shown in the banner slot while
   * silt-ai-summary computes the content hash on a note switch (#488).
   *
   * Mounting the real SummaryBanner requires the content hash (so dismissal is
   * keyed by `${pageId}:${contentHash}` #455), and that hash is async (a SQLite
   * read + SHA-256). To avoid a stale flash of the PRIOR note's summary during
   * that window, index.ts tears the prior surface down and registers this
   * placeholder immediately — it occupies the slot from t=0 so the editor
   * content below doesn't reflow (the banner stack has no min-height), while
   * staying invisible for LOADING_FADE_DELAY then fading in. A fast hash (the
   * common sub-delay case) resolves before the skeleton paints, so the user
   * sees no flicker; a slow read (large note / linked disk) gets a smooth
   * fade-in instead of a hard empty gap. The real banner/chip replaces it the
   * moment the hash resolves.
   *
   * Intentionally inert: no controller reads, no dismiss affordance, no
   * generation — it is pure chrome reused from SummaryBanner so the slot reads
   * as the same surface kind while the real one materializes. The CSS rules
   * mirror SummaryBanner's `.summary-banner`/`.head`/`.lead-icon`/`.body`/
   * `.skeleton` verbatim so the swap is visually continuous.
   */
  import type { PluginContext } from '../../sdk'

  interface Props {
    ctx: PluginContext
    onDismiss: () => void
  }
  let { ctx: _ctx, onDismiss: _onDismiss }: Props = $props()
</script>

<section class="summary-banner" aria-label="Loading AI summary">
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
</section>

<style>
  /* Mirrors SummaryBanner's chrome + skeleton so the loading slot is visually
     continuous with the real banner. The only addition is the delayed fade-in
     (below), which keeps the placeholder invisible for a fast hash. */
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
    position: relative;
    /* Stay invisible during the fade-in delay so a fast hash resolves before
       the skeleton ever paints. `forwards` holds opacity:1 once faded in. */
    opacity: 0;
    animation: loading-fade-in 160ms ease-out 100ms forwards;
  }

  .head {
    display: flex;
    align-items: flex-start;
    gap: 10px;
    min-width: 0;
  }

  .lead-icon {
    font-size: 18px;
    line-height: 1.4;
    color: var(--color-accent-primary-start);
    flex-shrink: 0;
    margin-top: 1px;
  }

  .body {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 4px;
    align-self: center;
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

  @keyframes loading-fade-in {
    to {
      opacity: 1;
    }
  }

  @media (prefers-reduced-motion: reduce) {
    .summary-banner {
      opacity: 1;
      animation: none;
    }
    .sk-line {
      animation: none;
    }
  }
</style>
