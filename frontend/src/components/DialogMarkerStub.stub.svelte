<script lang="ts">
  // Renders a marker only while `open` is true, so App-level tests can observe
  // a gated dialog's open/close transitions without depending on the real
  // component. Captures props via rest so the parent mounts cleanly.
  let {
    open = false,
    testid = 'dialog-marker',
    ..._rest
  }: { open?: boolean; testid?: string; [key: string]: unknown } = $props()
</script>

{#if open}
  <div data-testid={testid}></div>
{/if}
