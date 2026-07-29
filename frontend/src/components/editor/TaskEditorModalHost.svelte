<script lang="ts">
  import { onDestroy } from 'svelte'
  import { makePluginContext } from '../../plugins/context'
  import { getSessionToken } from '../../plugins/loader'
  import { loadedPlugins } from '../../plugins/store.svelte'
  import TaskSubEditorModal from '../../plugins/first-party/silt-tasks/components/TaskSubEditorModal.svelte'
  import { fetchTaskDetail } from '../../plugins/first-party/silt-tasks/query'
  import type { TaskDetail } from '../../plugins/first-party/silt-tasks/types'

  /**
   * Host for the in-page task-block modal (#781). Listens for the
   * `silt:open-task-editor` window event dispatched by TaskBlockView's pencil
   * button, the Shift-Enter keymap, and the editor context menu's "Edit task
   * in modal…" item. On open, fetches the full TaskDetail via
   * fetchTaskDetail and renders TaskSubEditorModal.
   *
   * Mirrors AIChatDrawer's PluginContext construction: builds a silt-tasks
   * context from the session token, gated on loadedPlugins.loadersReady so a
   * vault switch doesn't capture an empty token.
   */
  const PLUGIN_ID = 'silt-tasks'

  let openTask = $state<TaskDetail | null>(null)

  // Build the plugin context the same way AIChatDrawer does: cache per token
  // so closing and reopening doesn't create a new context.
  let cachedToken: string | undefined
  let cachedCtx: ReturnType<typeof makePluginContext> | null = null
  let ready = $derived(loadedPlugins.loadersReady)
  let ctx = $derived.by(() => {
    if (!ready) return null
    const token = getSessionToken(PLUGIN_ID) ?? undefined
    if (cachedCtx && token === cachedToken) return cachedCtx
    cachedCtx = makePluginContext(PLUGIN_ID, token)
    cachedToken = token
    return cachedCtx
  })

  function onOpenTaskEditor(event: Event): void {
    const detail = (event as CustomEvent<{ blockId: string }>).detail
    if (!detail?.blockId) return
    // Don't open if already showing this task.
    if (openTask?.id === detail.blockId) return
    void openTaskEditor(detail.blockId)
  }

  async function openTaskEditor(blockId: string): Promise<void> {
    const currentCtx = ctx
    if (!currentCtx) return
    try {
      openTask = await fetchTaskDetail(currentCtx, blockId)
    } catch {
      openTask = null
    }
  }

  function closeModal(): void {
    openTask = null
  }

  $effect(() => {
    window.addEventListener('silt:open-task-editor', onOpenTaskEditor)
    return () =>
      window.removeEventListener('silt:open-task-editor', onOpenTaskEditor)
  })

  onDestroy(() => {
    openTask = null
  })
</script>

{#if openTask && ctx}
  <TaskSubEditorModal
    blockId={openTask.id}
    notebook={openTask.notebook}
    section={openTask.section}
    page={openTask.page}
    parentTaskText={openTask.clean_content}
    {ctx}
    onClose={closeModal}
    onMetaChanged={() => {
      // Re-hydrate so the sidebar reflects the persisted metadata.
      void openTaskEditor(openTask!.id)
    }}
  />
{/if}
