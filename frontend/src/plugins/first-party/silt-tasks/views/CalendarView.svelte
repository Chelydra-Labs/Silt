<script lang="ts">
  // Calendar display mode stub (#424 shell / placeholder for #425). Same
  // role as BoardView.svelte: the mode switcher needs a render target while
  // the month/week grid is lifted out of the standalone silt-calendar plugin.
  // Replaced wholesale by the Calendar-mode issue (#425), which adds ctx back.
</script>

<div
  class="flex-1 flex items-center justify-center text-center px-6 py-16"
  data-testid="tasks-calendar-stub"
>
  <div class="max-w-sm">
    <span
      class="material-symbols-outlined text-accent-primary-start text-5xl mb-3"
      aria-hidden="true">calendar_month</span
    >
    <h2 class="font-headline-md text-text-primary mb-1">Calendar mode</h2>
    <p class="text-text-muted text-[13px] font-body-md">
      The month/week grid of tasks by due date lands here. Switch to List to
      keep working while Calendar is folded in.
    </p>
  </div>
</div>
