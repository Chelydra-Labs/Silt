<script lang="ts">
</script>

<div class="p-6 max-w-2xl space-y-6">
  <div
    class="bg-surface/20 border border-border-muted rounded-xl p-5 space-y-4"
  >
    <h4
      class="font-label-sm-bold text-text-primary uppercase tracking-wider text-[10px]"
    >
      Developer Tools
    </h4>

    <div class="space-y-3">
      <p class="text-text-muted/70 text-[12px] font-body-md leading-relaxed">
        Dev Mode is enabled — the Dev tab appears in the settings sidebar.
        Disable it on the <strong>About</strong> page.
      </p>

      <div class="bg-surface/30 border border-border-muted rounded-lg p-4 space-y-3">
        <p class="text-text-primary text-[13px] font-body-md font-semibold">
          Chromium DevTools
        </p>
        <p class="text-text-muted/70 text-[11px] font-body-md leading-relaxed">
          Inspect the DOM, view console errors, and debug rendering issues.
        </p>
        <p class="text-text-muted/70 text-[11px] font-body-md leading-relaxed">
          Press <kbd
            class="inline-block px-1.5 py-0.5 rounded bg-surface border border-border-muted text-text-primary text-[10px] font-mono"
            >Ctrl+Shift+F12</kbd
          > to open DevTools at any time.
        </p>
      </div>

      <div class="bg-surface/30 border border-border-muted rounded-lg p-4 space-y-3">
        <p class="text-text-primary text-[13px] font-body-md font-semibold">
          State Debug Badge
        </p>
        <p class="text-text-muted/70 text-[11px] font-body-md leading-relaxed">
          A red badge appears at the bottom of the content area showing the
          current navigation state (<code>view</code>, <code>nb</code>, <code>pg</code>,
          <code>tab</code>, <code>dt</code>, <code>nr</code>) to help diagnose
          blank-page issues.
        </p>
      </div>
    </div>
  </div>
</div>
