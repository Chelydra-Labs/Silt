<script lang="ts">
  import { onMount, onDestroy, getContext, setContext, tick } from 'svelte'
  import { Editor } from '@tiptap/core'
  import StarterKit from '@tiptap/starter-kit'
  import {
    ResolveBlockReference,
    MutateBlock
  } from '../../bindings/silt/app.js'
  import { Events } from '@wailsio/runtime'
  import RichText from './RichText.svelte'
  import { coerceIPCError } from '../lib/ipcError'
  import {
    SiltInlineMarkExtensions,
    BlockReferenceNode,
    PageLinkNode,
    MentionNode
  } from '../lib/editor/schema'
  import {
    legacyTokenizeInline,
    serializeInlineContent
  } from '../lib/editor/converters'
  import type { NodeJSON } from '../lib/editor/types'

  // Per-branch chain of embed UUIDs currently being rendered. Each
  // EmbedPortal inherits its ancestor's chain via Svelte context, checks
  // whether its own UUID is already on it, and then publishes a fresh
  // chain with its UUID appended for its own descendants. Siblings of the
  // same block share the parent chain, so a second sibling sees only the
  // chain above the parent — never its own UUID — and renders normally.
  //
  // This replaces a previous global Set which incorrectly flagged any
  // second mount of the same block as recursive even when the two embeds
  // were siblings rather than an ancestor/descendant pair.
  const EMBED_CHAIN_KEY = Symbol('embed-chain')
  type EmbedChain = { has(uuid: string): boolean }
  const parentChain = getContext<EmbedChain | undefined>(EMBED_CHAIN_KEY)

  interface Props {
    uuid: string
    hostNotebook?: string
    hostSection?: string
    hostPage?: string
    hostFileDate?: string
  }

  let {
    uuid,
    hostNotebook: _hostNotebook = '',
    hostSection: _hostSection = '',
    hostPage: _hostPage = '',
    hostFileDate: _hostFileDate = ''
  }: Props = $props()

  // Recursion guard: an embed is recursive only when it appears in its
  // own ancestor chain. Sibling embeds of the same block are not.
  let isRecursive = $state(false)

  type EmbedRef = {
    exists: boolean
    id?: string
    notebook?: string
    section?: string
    page?: string
    file_date?: string
    clean_text?: string
  }
  let ref = $state<EmbedRef | null>(null)
  let loading = $state(true)
  let editing = $state(false)
  let editorHost = $state<HTMLDivElement | null>(null)
  let nestedEditor: Editor | null = null
  let saveTimer: ReturnType<typeof setTimeout> | null = null
  let offEvent: (() => void) | null = null
  let persistError = $state('')

  async function load() {
    loading = true
    try {
      ref = (await ResolveBlockReference(uuid)) as EmbedRef
    } catch {
      ref = { exists: false }
    } finally {
      loading = false
    }
  }

  async function persist(text: string, attempt = 0) {
    try {
      await MutateBlock(uuid, text)
      persistError = ''
    } catch (e) {
      const msg = e instanceof Error ? e.message : String(e)
      // The source block is being edited in another view (focus lock held).
      // Retry shortly instead of silently overwriting or dropping the edit.
      // #478: map on the stable code first (resilient to backend wording
      // changes), with the substring as a legacy fallback for any unmigrated
      // return site.
      const isBusy =
        coerceIPCError(e).code === 'block_being_edited' ||
        msg.includes('being edited')
      if (isBusy && attempt < 5) {
        saveTimer = setTimeout(() => void persist(text, attempt + 1), 800)
      } else {
        // Exhausted retries or a non-transient error — surface it so the
        // user knows their edit didn't save.
        persistError = isBusy
          ? 'Source block is busy — edit not saved yet'
          : `Save failed: ${msg}`
      }
    }
  }

  // MutateBlock stores single-line clean_text; collapse any accidental
  // multi-paragraph output from the nested editor.
  function textFromEditor(ed: Editor): string {
    const json = ed.getJSON()
    const parts: string[] = []
    for (const block of json.content ?? []) {
      if (block.content) {
        parts.push(serializeInlineContent(block.content as NodeJSON[]))
      }
    }
    return parts.join(' ').replace(/[\r\n]+/g, ' ')
  }

  function scheduleSave(text: string) {
    if (ref) ref.clean_text = text
    persistError = ''
    if (saveTimer) clearTimeout(saveTimer)
    saveTimer = setTimeout(() => {
      saveTimer = null
      void persist(text)
    }, 500)
  }

  function destroyNestedEditor() {
    if (nestedEditor) {
      nestedEditor.destroy()
      nestedEditor = null
    }
  }

  function mountNestedEditor() {
    if (!editorHost || nestedEditor || !ref) return

    const inline = legacyTokenizeInline(ref.clean_text || '')
    nestedEditor = new Editor({
      element: editorHost,
      extensions: [
        // Inline-only subset: no EmbedNode (would re-enter portals).
        StarterKit.configure({
          heading: false,
          bulletList: false,
          orderedList: false,
          listItem: false,
          blockquote: false,
          codeBlock: false,
          horizontalRule: false,
          trailingNode: false
        }),
        ...SiltInlineMarkExtensions,
        BlockReferenceNode,
        PageLinkNode,
        MentionNode
      ],
      content: {
        type: 'doc',
        content: [
          {
            type: 'paragraph',
            content: inline.length > 0 ? inline : undefined
          }
        ]
      },
      editorProps: {
        attributes: {
          class:
            'text-text-primary text-sm leading-relaxed focus:outline-none min-h-5 whitespace-pre-wrap break-words',
          role: 'textbox',
          'aria-label': 'Edit embedded block'
        },
        // Keep clean_text single-line: Enter does not split paragraphs.
        handleKeyDown: (_view, event) => {
          if (event.key === 'Enter') {
            event.preventDefault()
            return true
          }
          return false
        }
      },
      onUpdate: ({ editor: ed }) => {
        scheduleSave(textFromEditor(ed))
      }
    })
    // scrollIntoView needs layout APIs jsdom lacks; skip scroll in tests.
    try {
      nestedEditor.commands.focus('end', { scrollIntoView: false })
    } catch {
      /* ignore focus failures in non-browser environments */
    }
  }

  async function startEditing() {
    if (editing || !ref?.exists) return
    editing = true
    await tick()
    mountNestedEditor()
  }

  function stopEditing() {
    if (!editing) return
    if (nestedEditor) {
      const text = textFromEditor(nestedEditor)
      if (ref) ref.clean_text = text
      if (saveTimer) {
        clearTimeout(saveTimer)
        saveTimer = null
      }
      void persist(text)
      destroyNestedEditor()
    }
    editing = false
  }

  function handleEditorFocusOut(e: FocusEvent) {
    const next = e.relatedTarget as Node | null
    if (editorHost && next && editorHost.contains(next)) return
    stopEditing()
  }

  function handlePreviewKeydown(e: KeyboardEvent) {
    if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault()
      void startEditing()
    }
  }

  onMount(() => {
    if (parentChain?.has(uuid)) {
      isRecursive = true
      loading = false
      return
    }
    // Publish a fresh chain to descendants with this UUID appended. The
    // chain object is captured by closure, so when this component unmounts
    // the chain it created simply goes out of scope — no explicit cleanup
    // or global mutation needed.
    setContext(EMBED_CHAIN_KEY, {
      has: (id: string) =>
        id === uuid || (parentChain ? parentChain.has(id) : false)
    } satisfies EmbedChain)
    load()
    // Live sync: refresh when the source block changes anywhere.
    offEvent = Events.On('block:changed', (event) => {
      const ev = event.data
      if (ev && ev.id === uuid && !editing && !saveTimer) {
        load()
      }
    })
  })

  onDestroy(() => {
    if (offEvent) offEvent()
    if (saveTimer) clearTimeout(saveTimer)
    destroyNestedEditor()
  })
</script>

{#if isRecursive}
  <span
    class="inline-flex items-center gap-1 mx-0.5 text-[0.8em] text-text-muted italic border border-dashed border-surface-popover-border rounded px-1.5 py-0.5"
    title="Recursive embed — stopped to avoid a loop"
  >
    <span class="material-symbols-outlined text-[0.9em]">block</span>recursive
    embed
  </span>
{:else if loading}
  <span class="text-text-muted italic text-[0.85em] mx-0.5">loading embed…</span
  >
{:else if !ref?.exists}
  <span
    class="inline-flex items-center gap-1 mx-0.5 text-[0.85em] text-text-muted italic"
    title="Embedded block not found"
  >
    <span class="material-symbols-outlined text-[0.9em]">hide_source</span
    >{`{{embed:${uuid.slice(0, 8)}…}}`}
  </span>
{:else}
  <div
    class="my-1 border border-accent-primary-start/30 bg-accent-primary-glow/40 rounded-lg p-2 pl-3 relative"
  >
    <div
      class="absolute left-0 top-0 bottom-0 w-0.5 bg-accent-primary-start/40 rounded-l"
    ></div>
    <div
      class="flex items-center gap-1 text-type-3xs uppercase tracking-widest font-label-sm-bold text-text-muted mb-1"
    >
      <span
        class="material-symbols-outlined text-type-2xs text-accent-primary-start"
        >clone</span
      >
      embed · {ref.notebook} › {ref.section} › {ref.page}
    </div>
    {#if editing}
      <!-- Nested TipTap mounts only while focused (#661). -->
      <div
        bind:this={editorHost}
        class="min-h-5"
        onfocusout={handleEditorFocusOut}
      ></div>
    {:else}
      <div
        role="textbox"
        tabindex="0"
        aria-label="Embedded block preview — activate to edit"
        onclick={() => void startEditing()}
        onkeydown={handlePreviewKeydown}
        class="text-text-primary text-sm leading-relaxed focus:outline-none min-h-5 whitespace-pre-wrap break-words cursor-text"
      >
        <RichText
          text={ref.clean_text || ''}
          notebook={ref.notebook ?? ''}
          section={ref.section ?? ''}
          page={ref.page ?? ''}
          fileDate={ref.file_date ?? ''}
        />
      </div>
    {/if}
    {#if persistError}
      <div class="text-type-2xs text-error mt-1 flex items-center gap-1">
        <span class="material-symbols-outlined text-type-xs">error</span>
        {persistError}
      </div>
    {/if}
  </div>
{/if}
