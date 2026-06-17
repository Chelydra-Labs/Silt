<script lang="ts">
  // NodeView wrapper for EmbedNode (#85). Threads the host context
  // (notebook, section, page, file_date) from the editor's options to the
  // EmbedPortal. The host fields are required for the portal's
  // block:changed event filtering and bidirectional PluginMutateBlock edits.
  import { NodeViewWrapper } from 'svelte-tiptap'
  import type { NodeViewProps } from '@tiptap/core'
  import EmbedPortal from '../EmbedPortal.svelte'

  let { editor, node }: NodeViewProps = $props()

  // Read the host context from the editor's storage (set by the
  // EditorHostContext extension registered in TipTapEditor.svelte).
  // Falls back to empty strings when the extension isn't present (tests).
  let host = $derived(
    ((editor as any)?.storage?.editorHostContext ?? {}) as {
      notebook?: string
      section?: string
      page?: string
      file_date?: string
    }
  )
</script>

<NodeViewWrapper>
  <EmbedPortal
    uuid={node.attrs.uuid}
    hostNotebook={host.notebook ?? ''}
    hostSection={host.section ?? ''}
    hostPage={host.page ?? ''}
    hostFileDate={host.file_date ?? ''}
  />
</NodeViewWrapper>
