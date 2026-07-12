<script lang="ts">
  // Placeholder hub component required by the registry (sidebar-panel is the
  // primary surface). Mirrors silt-ai-summary's informational panel.
  import type { PluginContext } from '../../sdk'
  interface Props {
    ctx?: PluginContext
  }
  let { ctx: _ctx }: Props = $props()
</script>

<div class="p-6 text-text-muted text-type-md">
  <h2 class="text-text-primary text-type-xl font-bold mb-2">AI Q&amp;A</h2>
  <p>
    Open the <strong>Ask your notes</strong> panel in the sidebar to question your
    vault. Configure models under Settings → AI Provider, then enable this plugin
    and rebuild the index under Settings → AI Q&amp;A.
  </p>
</div>
