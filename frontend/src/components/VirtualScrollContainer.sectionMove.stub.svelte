<script lang="ts">
  // Test-only pass-through harness: updates to `section` must reach the
  // container as an ordinary fine-grained prop update (exactly what a tab
  // repoint after a section move does in App.svelte). Rendering the
  // container directly and rerendering it replaces the whole props object,
  // which re-runs every effect and hides section-only dependencies.
  import VirtualScrollContainer from './VirtualScrollContainer.svelte'

  let { section = '' }: { section?: string } = $props()
</script>

<VirtualScrollContainer notebook="NB" {section} page="PG" viewMode="edit" />
