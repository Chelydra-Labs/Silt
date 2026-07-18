<script lang="ts">
  let {
    blocks,
    filePath,
    notebook: _notebook,
    section: _section,
    page: _page,
    onBlocksSaved: _onBlocksSaved,
    editable: _editable
  }: {
    blocks: unknown[]
    filePath: string
    notebook?: string
    section?: string
    page?: string
    onBlocksSaved?: (blocks: unknown[]) => void
    editable?: boolean
  } = $props()
</script>

<div data-testid="markdown-source-stub" data-file={filePath}></div>
