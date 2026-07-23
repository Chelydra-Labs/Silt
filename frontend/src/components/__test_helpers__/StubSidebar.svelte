<script lang="ts">
  // Test-only Svelte component (#321). Renders a marker element so tests
  // can assert the routing layer actually rendered a registered
  // sidebarComponent instead of falling back to the page tree. Exposes
  // the props it received on `globalThis.__lastStubSidebarProps` so the
  // test can inspect the ctx + manifest plumbing.
  import type { PluginContext, PluginManifest } from '../../plugins/sdk'
  interface Props {
    ctx: PluginContext
    manifest?: PluginManifest | null
  }
  let { ctx, manifest }: Props = $props()
  // inspection only; capture props once on mount (mirrors PluginView.svelte).
  $effect.pre(() => {
    ;(
      globalThis as unknown as { __lastStubSidebarProps?: unknown }
    ).__lastStubSidebarProps = { ctx, manifest }
  })
</script>

<div data-test-stub-sidebar data-plugin-id={manifest?.id ?? ''}>
  STUB-SIDEBAR-RENDERED
</div>
