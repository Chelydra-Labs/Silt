<script lang="ts">
  let {
    blocks: _blocks,
    filePath,
    notebook: _notebook,
    section: _section,
    page: _page,
    onBlocksSaved: _onBlocksSaved,
    editable: _editable,
    conflictActive = false,
    onSaveRefused
  }: {
    blocks: unknown[]
    filePath: string
    notebook?: string
    section?: string
    page?: string
    onBlocksSaved?: (blocks: unknown[]) => void
    editable?: boolean
    conflictActive?: boolean
    onSaveRefused?: () => void
  } = $props()
</script>

<div
  data-testid="markdown-source-stub"
  data-file={filePath}
  data-conflict-active={conflictActive ? 'true' : 'false'}
>
  <button
    type="button"
    data-testid="msv-stub-emit-refused"
    onclick={() => onSaveRefused?.()}
  >
    emit refused
  </button>
</div>
