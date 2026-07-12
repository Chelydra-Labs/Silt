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
  <h2 class="text-text-primary text-type-xl font-bold mb-2">AI Assistant</h2>
  <p>
    Open the <strong>AI Assistant</strong> drawer from the title-bar icon (next to
    Search) to search your vault and get cited answers. Configure models under Settings
    → AI Provider, then enable this plugin and rebuild the index under Settings →
    AI Assistant.
  </p>
</div>
