<script lang="ts">
  // AISummaryPanel — the plugin's informational main-view tab.
  //
  // silt-ai-summary is a banner-only plugin: its output lives at the top of
  // each note (the note-banner surface, wired in Phase 3), not in a dedicated
  // panel. This component is the placeholder the registry requires so the
  // plugin has a view when opened from the Plugins tab — it explains where
  // summaries appear and how to enable/configure them. The live per-note
  // surface is the SummaryBanner component.
</script>

<section class="ai-summary-panel" aria-labelledby="ai-summary-title">
  <span class="material-symbols-outlined icon" aria-hidden="true"
    >auto_awesome</span
  >
  <h2 id="ai-summary-title">AI Summary</h2>
  <p>
    Summaries appear as a dismissible highlight at the top of each note. Open
    any note to see a 2–3 sentence brief plus any new tasks, risks, and
    decisions the LLM extracted.
  </p>
  <p class="muted">
    Configure the model in <strong>Settings → AI</strong>. Tune facets, length,
    and timing in <strong>Settings → AI → Note summaries</strong>.
  </p>
</section>

<style>
  .ai-summary-panel {
    padding: 2rem 2.5rem;
    max-width: 46rem;
    display: flex;
    flex-direction: column;
    gap: 0.75rem;
    color: var(--color-text-primary);
  }
  .icon {
    font-size: 2rem;
    color: var(--color-accent-primary, var(--color-text-primary));
  }
  h2 {
    margin: 0;
    font-size: 1.25rem;
  }
  p {
    margin: 0;
    line-height: 1.5;
  }
  .muted {
    color: var(--color-text-muted);
    font-size: 0.9rem;
  }
</style>
