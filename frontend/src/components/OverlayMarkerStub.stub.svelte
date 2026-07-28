<script lang="ts">
  // Renders a distinctive marker so App-level hotkey/dispatch tests can observe
  // that a gated overlay mounted without depending on the real component's
  // internal behavior. Captures props via rest so the parent mounts cleanly.
  let {
    testid = 'overlay-marker',
    ..._rest
  }: { testid?: string; [key: string]: unknown } = $props()
</script>

<div {testid} data-testid={testid}></div>
