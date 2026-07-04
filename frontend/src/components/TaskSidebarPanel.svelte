<script lang="ts">
  // Lightweight sidebar for the Tasks view (#380). The Tasks view is a
  // full-page plugin surface that owns its own task list/quick-add UX, so the
  // notes navigation tree (notebook selector + page tree) carries no useful
  // context there. Rather than leak the notes UI, render an intentional panel
  // that points the user at the Tasks view and the global quick-add hotkey.
  //
  // The richer board/filter sidebar (the KanbanSidebar/CalendarSidebar #321
  // pattern) is intentionally deferred — it overlaps scope already owned by
  // the Kanban sidebar and is tracked as a separate enhancement.
</script>

<div class="flex-grow flex flex-col min-h-0 bg-surface">
  <div class="px-3 py-3 border-b border-border-muted flex-shrink-0">
    <div class="flex items-center gap-2">
      <span
        class="material-symbols-outlined text-accent-primary-start text-[20px]"
        aria-hidden="true">checklist</span
      >
      <h2 class="font-headline-md text-headline-md text-text-primary">Tasks</h2>
    </div>
    <p class="text-text-muted text-[11px] font-label-sm mt-1">
      Standalone &amp; recurring tasks
    </p>
  </div>

  <div class="flex-grow overflow-y-auto custom-scrollbar p-4">
    <div
      class="text-center py-10 px-3 rounded-xl border border-dashed border-border-muted bg-surface/10 select-none"
    >
      <span
        class="material-symbols-outlined text-accent-primary-start text-4xl mb-2"
        aria-hidden="true">task_alt</span
      >
      <p class="text-text-primary text-[13px] font-body-md mb-3">
        Your tasks live in the Tasks view.
      </p>
      <p class="text-text-muted text-[12px] font-body-md leading-relaxed">
        Capture one from anywhere with
        <kbd
          class="px-1.5 py-0.5 rounded bg-hover text-text-primary border border-border-zinc font-mono text-[11px]"
          >Ctrl+Shift+N</kbd
        >
      </p>
    </div>
  </div>
</div>
