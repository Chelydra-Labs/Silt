<script lang="ts">
  let {
    notebook,
    section,
    page,
    blocks,
    activeFocusedBlockAncestors,
    onBlockFocus,
    onBlockBlur,
    onUpdate,
    editorInstance,
    activeMarks,
    onSaveStateChange,
    onReady
  } = $props()

  // Mirror the real editor's onCreate readiness signal so the parent's
  // post-mount work (scroll restore across Edit↔Source, #319) runs in tests.
  $effect(() => {
    onReady?.()
  })
</script>

<div data-testid="tiptap-stub" data-page={page}>
  <!-- Test seams: drive parent save-state wiring without a real editor. -->
  <button
    type="button"
    data-testid="tiptap-stub-emit-dirty"
    onclick={() =>
      onSaveStateChange?.({ phase: 'pending', dirty: true, error: null })}
  >
    emit dirty
  </button>
  <button
    type="button"
    data-testid="tiptap-stub-emit-saving"
    onclick={() =>
      onSaveStateChange?.({ phase: 'saving', dirty: true, error: null })}
  >
    emit saving
  </button>
  <button
    type="button"
    data-testid="tiptap-stub-emit-saved"
    onclick={() =>
      onSaveStateChange?.({ phase: 'saved', dirty: false, error: null })}
  >
    emit saved
  </button>
  <button
    type="button"
    data-testid="tiptap-stub-emit-error"
    onclick={() =>
      onSaveStateChange?.({ phase: 'error', dirty: false, error: 'disk full' })}
  >
    emit error
  </button>
  <button
    type="button"
    data-testid="tiptap-stub-emit-clean"
    onclick={() =>
      onSaveStateChange?.({ phase: 'idle', dirty: false, error: null })}
  >
    emit clean
  </button>
</div>
