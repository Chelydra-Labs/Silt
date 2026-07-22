<script lang="ts">
  let {
    notebook: _notebook,
    section: _section,
    page,
    blocks: _blocks,
    activeFocusedBlockAncestors: _activeFocusedBlockAncestors,
    onBlockFocus: _onBlockFocus,
    onBlockBlur: _onBlockBlur,
    onUpdate: _onUpdate,
    // eslint-disable-next-line no-useless-assignment -- $bindable out-param for parent
    editorInstance = $bindable(null),
    activeMarks: _activeMarks,
    onSaveStateChange,
    onReady
  } = $props()

  // Minimal editor surface for #319/#331 restore tests.
  // Tests seed capture/resolve via:
  //   globalThis.__tiptapStubSeed  — selection shape for snapshotEditCaret
  //   globalThis.__tiptapStubDoc   — doc.descendants for resolveCaretInDoc
  //   globalThis.__tiptapStubSelection — last setTextSelection pos
  let selectionFrom = $state(1)

  function buildStub() {
    const seed = (globalThis as unknown as Record<string, unknown>)
      .__tiptapStubSeed as
      | {
          from: number
          $from: {
            depth: number
            pos: number
            start: (d: number) => number
            node: (d: number) => {
              type: { name: string }
              attrs?: { id?: string }
            }
          }
        }
      | undefined

    const from = seed?.from ?? selectionFrom
    selectionFrom = from

    return {
      state: {
        get selection() {
          if (seed?.$from) {
            return {
              from: selectionFrom,
              to: selectionFrom,
              $from: seed.$from
            }
          }
          return { from: selectionFrom, to: selectionFrom }
        },
        get doc() {
          return (
            (globalThis as unknown as Record<string, unknown>)
              .__tiptapStubDoc ?? {
              descendants() {
                /* empty */
              }
            }
          )
        }
      },
      commands: {
        setTextSelection(pos: number) {
          selectionFrom = pos
          ;(
            globalThis as unknown as Record<string, unknown>
          ).__tiptapStubSelection = pos
          return true
        },
        focus() {
          return true
        }
      }
    }
  }

  // Mirror the real editor's onCreate readiness signal so the parent's
  // post-mount work (scroll/caret restore across Edit↔Source) runs in tests.
  $effect(() => {
    editorInstance = buildStub() as never
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
