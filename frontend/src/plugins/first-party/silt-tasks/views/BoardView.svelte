<script lang="ts">
  // Board display mode stub (#424 shell / placeholder for #421). The hub's
  // mode switcher can land here while the dimension-aware Board renderer is
  // under construction; selecting Board shows this affordance instead of a
  // blank pane. Replaced wholesale by the Board-mode issue (#421), which
  // adds the ctx prop back.
</script>

<div
  class="flex-1 flex items-center justify-center text-center px-6 py-16"
  data-testid="tasks-board-stub"
>
  <div class="max-w-sm">
    <span
      class="material-symbols-outlined text-accent-primary-start text-5xl mb-3"
      aria-hidden="true">view_kanban</span
    >
    <h2 class="font-headline-md text-text-primary mb-1">Board mode</h2>
    <p class="text-text-muted text-[13px] font-body-md">
      Drag-and-drop columns grouped by any dimension land here. Switch to List
      to keep working while Board is built.
    </p>
  </div>
</div>
