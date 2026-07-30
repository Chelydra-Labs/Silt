<script lang="ts">
  // Test stub for TaskSubEditorModal — renders only the blockId the host opened
  // it with, so TaskEditorModalHost tests can observe which task the modal
  // landed on without mounting the real editor/metadata surface.
  let { blockId }: { blockId: string } = $props()
</script>

<div data-testid="task-sub-editor-modal" data-block-id={blockId}></div>
