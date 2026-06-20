<script lang="ts">
  // silt-attachments — first-party plugin (#101). Lets the user attach files
  // (PDFs, images, docs) to a note via the /attach slash command. The chosen
  // file is COPIED into <notebook>/attachments/, and an embedBlock is inserted
  // at the cursor. The block is movable, deletable, and opens in the OS native
  // handler on activation. Uses the PluginContext SDK exclusively (SPECS §8.3).
  //
  // This component is the plugin's view slot (minimal — the main interaction is
  // the /attach slash command registered in onVaultOpen). It renders a short
  // help panel when the user opens the Attachments view.
  import type { PluginContext, PluginManifest } from '../../sdk'

  interface Props {
    ctx: PluginContext
    manifest: PluginManifest
  }
  let { ctx, manifest }: Props = $props()
</script>

<div class="p-6 max-w-2xl">
  <h2 class="font-headline text-text-primary text-xl mb-2">{manifest.name}</h2>
  <p class="text-text-muted text-[13px] font-body-md mb-4">
    Attach files (PDFs, images, documents) to your notes. Use the
    <code class="text-accent-primary-start">/attach</code> slash command in any note
    to pick a file — it is copied into the notebook and embedded as a block.
  </p>
  <div
    class="p-4 rounded-lg border border-border-muted bg-surface/50 space-y-2"
  >
    <div
      class="flex items-center gap-2 text-text-primary text-[12px] font-body-md"
    >
      <span
        class="material-symbols-outlined text-accent-primary-start text-[18px]"
        >attach_file</span
      >
      Attachments live in
      <code class="text-accent-primary-start"
        >&lt;notebook&gt;/attachments/</code
      >
    </div>
    <div
      class="flex items-center gap-2 text-text-muted text-[11px] font-body-md"
    >
      <span class="material-symbols-outlined text-[14px]">info</span>
      Files are copied (not linked), so the notebook stays self-contained and portable.
    </div>
    <p class="text-text-muted text-[11px] font-body-md pt-1">
      Active notebook: <span class="text-text-primary"
        >{ctx.activeNotebook || '—'}</span
      >
    </p>
  </div>
</div>
