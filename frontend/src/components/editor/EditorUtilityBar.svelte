<script lang="ts">
  import type { Editor } from 'svelte-tiptap'
  import FormatToolbar from './FormatToolbar.svelte'
  import { settings } from '../../settings/store.svelte'
  import { isSystemDark } from '../../lib/systemTheme.svelte'

  // EditorUtilityBar — extracted from VirtualScrollContainer (#202).
  // Now simply acts as a container for FormatToolbar since action controls
  // (View Mode, Zen Mode, Focus Mode) have been relocated to the TabStrip.

  interface Props {
    editor: Editor | null
    activeMarks: Set<string>
  }

  let { editor, activeMarks }: Props = $props()

  let isDark = $derived(isSystemDark())
  let colorEnabled = $derived(
    settings.config?.ui?.formatting?.color_enabled !== false
  )
</script>

<div class="unified-utility-bar">
  <FormatToolbar {editor} {activeMarks} {isDark} {colorEnabled} />
</div>

<style>
  .unified-utility-bar {
    display: flex;
    align-items: center;
    height: 38px;
    padding: 0 16px;
    background: color-mix(
      in srgb,
      var(--color-surface, #1a1d24) 95%,
      transparent
    );
    backdrop-filter: blur(8px);
    border-bottom: 1px solid var(--color-border-muted, #2a2e36);
    flex-shrink: 0;
    z-index: 15;
  }
</style>
