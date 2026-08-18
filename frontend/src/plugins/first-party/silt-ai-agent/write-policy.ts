// Agent vault write policy (#924).
//
// Modes come from ai.features.agent_writes (Settings). Mutating tools are
// either refused (read_only), staged for HITL (confirm / always-confirm set),
// or run immediately (auto, except bulk rename + extract).

import { readAIFeatures } from '../../shared/ai-chat/availability'
import type { AgentToolDef, StagedPreview } from './tool-registry'

export type AgentWritesMode = 'read_only' | 'confirm' | 'auto'

export const MUTATING_TOOLS = new Set([
  'create_note',
  'create_task',
  'update_block',
  'update_task',
  'rename_tag',
  'extract_and_save',
  'restore_page_version'
])

/** Always require confirmation even in auto mode (bulk / nested-model writes). */
export const ALWAYS_CONFIRM_TOOLS = new Set(['rename_tag', 'extract_and_save'])

export function isMutatingTool(name: string): boolean {
  return MUTATING_TOOLS.has(name)
}

/**
 * Whether dispatch should stage instead of running the write immediately.
 * read_only never stages (caller refuses first). confirm stages all mutators.
 * auto stages only ALWAYS_CONFIRM_TOOLS.
 */
export function shouldStageTool(name: string, mode: AgentWritesMode): boolean {
  if (mode === 'read_only') return false
  if (!isMutatingTool(name)) return false
  if (mode === 'confirm') return true
  return ALWAYS_CONFIRM_TOOLS.has(name)
}

export function readAgentWritesMode(): AgentWritesMode {
  return readAIFeatures().agent_writes
}

/** Drop mutators from the model catalog when vault writes are read-only. */
export function filterToolsForWritePolicy(
  tools: AgentToolDef[],
  mode: AgentWritesMode
): AgentToolDef[] {
  if (mode !== 'read_only') return tools
  return tools.filter((t) => !isMutatingTool(t.name))
}

function clip(s: string, max = 240): string {
  const t = s.trim()
  if (t.length <= max) return t
  return `${t.slice(0, max - 1)}…`
}

/** Short human summary for the staging card when dispatch stages a mutator. */
export function previewForMutation(
  toolName: string,
  args: Record<string, unknown>
): StagedPreview {
  switch (toolName) {
    case 'create_note': {
      const page = str(args.page)
      const notebook = str(args.notebook)
      const section = str(args.section)
      const path = [notebook, section, page].filter(Boolean).join('/')
      const body = str(args.content)
      return {
        kind: 'create_note',
        summary: path ? `Create note on ${path}` : 'Create a note',
        details: body ? clip(body) : undefined,
        affectedCount: 1,
        severity: 'normal'
      }
    }
    case 'create_task': {
      const title = str(args.text) || str(args.title) || str(args.content)
      const short =
        title.length > 60 ? `${title.slice(0, 57)}…` : title || 'task'
      return {
        kind: 'create_task',
        summary: `Create task: ${short}`,
        details: title && title.length > 60 ? clip(title) : undefined,
        affectedCount: 1,
        severity: 'normal'
      }
    }
    case 'update_block': {
      const id = str(args.block_id) || str(args.id)
      const body = str(args.content)
      return {
        kind: 'update_block',
        summary: id ? `Update block ${id}` : 'Update a block',
        details: body ? clip(body) : undefined,
        affectedCount: 1,
        severity: 'normal'
      }
    }
    case 'update_task': {
      const id = str(args.task_id) || str(args.block_id) || str(args.id)
      const bits: string[] = []
      if (args.status != null) bits.push(`status=${str(args.status)}`)
      if (args.due != null) bits.push(`due=${str(args.due)}`)
      if (args.title != null) bits.push(`title=${clip(str(args.title), 80)}`)
      if (args.owner != null) bits.push(`owner=${str(args.owner)}`)
      return {
        kind: 'update_task',
        summary: id ? `Update task ${id}` : 'Update a task',
        details: bits.length > 0 ? bits.join('\n') : undefined,
        affectedCount: 1,
        severity: 'normal'
      }
    }
    case 'extract_and_save': {
      // Handler-stage owns the real content preview; this is dispatch fallback only.
      const mode = str(args.mode) || 'extract'
      const target = args.target as Record<string, unknown> | undefined
      const path = target
        ? [str(target.notebook), str(target.section), str(target.page)]
            .filter(Boolean)
            .join('/')
        : ''
      const blockCount = Array.isArray(args.blocks) ? args.blocks.length : 0
      const ids = Array.isArray(args.source_block_ids)
        ? args.source_block_ids.length
        : 0
      const detailsParts: string[] = [`Mode: ${mode}`]
      if (ids > 0) detailsParts.push(`Source blocks: ${ids}`)
      if (blockCount > 0) {
        detailsParts.push(`${blockCount} extracted block(s) ready to write.`)
      } else {
        detailsParts.push(
          'Confirm writes the staged extraction to the target page.'
        )
      }
      return {
        kind: 'extract_and_save',
        summary: path
          ? `Extract ${mode} → ${path}`
          : `Extract ${mode} to a new note`,
        details: detailsParts.join('\n'),
        affectedCount: blockCount > 0 ? blockCount : 1,
        severity: 'danger'
      }
    }
    case 'restore_page_version': {
      const notebook = str(args.notebook)
      const section = str(args.section)
      const page = str(args.page)
      const id = str(args.version_id)
      const path = [notebook, section, page].filter(Boolean).join('/')
      return {
        kind: 'restore_page_version',
        summary: path
          ? `Restore ${path} to version ${id || '…'}`
          : 'Restore a page version',
        details: id ? `Version ${id}` : undefined,
        affectedCount: 1,
        severity: 'danger'
      }
    }
    case 'rename_tag': {
      const oldTag = str(args.old_tag)
      const newTag = str(args.new_tag)
      return {
        kind: 'rename_tag',
        summary:
          oldTag && newTag
            ? `Rename tag #${oldTag} → #${newTag}`
            : 'Rename a tag',
        details: 'Rewrites the hashtag across matching blocks in the vault.',
        severity: 'danger'
      }
    }
    default:
      return { kind: toolName, summary: toolName, severity: 'danger' }
  }
}

function str(v: unknown): string {
  return typeof v === 'string' ? v.trim() : ''
}
