<script lang="ts">
  // Test harness: lets a Vitest test drive <Popover> with a fixed content
  // snippet + an externally-created anchor element (jsdom has no layout, so
  // the test mocks getBoundingClientRect / window dims where positioning
  // matters). Mirrors the StubSidebar harness pattern.
  import Popover from '../Popover.svelte'

  let {
    anchor,
    open,
    onClose,
    matchWidth = false
  }: {
    anchor: HTMLElement | null
    open: boolean
    onClose: () => void
    matchWidth?: boolean
  } = $props()
</script>

<Popover {open} {onClose} {anchor} {matchWidth}>
  {#snippet content()}
    <div data-test-popover-content>popover body</div>
  {/snippet}
</Popover>
