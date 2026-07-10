<script lang="ts">
  // No-op component used to stub children in App-level tests. Captures any
  // props/callbacks via rest so the parent mounts without prop warnings.
  let { ..._rest }: { [key: string]: unknown } = $props()
</script>
