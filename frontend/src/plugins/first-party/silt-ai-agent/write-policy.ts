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
  'extract_and_save'
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
      return {
        kind: 'create_note',
        summary: path ? `Create note on ${path}` : 'Create a note',
        affectedCount: 1
      }
    }
    case 'create_task': {
      const title = str(args.text) || str(args.title) || str(args.content)
      const short =
        title.length > 60 ? `${title.slice(0, 57)}…` : title || 'task'
      return {
        kind: 'create_task',
        summary: `Create task: ${short}`,
        affectedCount: 1
      }
    }
    case 'update_block': {
      const id = str(args.block_id) || str(args.id)
      return {
        kind: 'update_block',
        summary: id ? `Update block ${id}` : 'Update a block',
        affectedCount: 1
      }
    }
    case 'update_task': {
      const id = str(args.task_id) || str(args.block_id) || str(args.id)
      return {
        kind: 'update_task',
        summary: id ? `Update task ${id}` : 'Update a task',
        affectedCount: 1
      }
    }
    case 'extract_and_save': {
      const mode = str(args.mode) || 'extract'
      const target = args.target as Record<string, unknown> | undefined
      const path = target
        ? [str(target.notebook), str(target.section), str(target.page)]
            .filter(Boolean)
            .join('/')
        : ''
      return {
        kind: 'extract_and_save',
        summary: path
          ? `Extract ${mode} → ${path}`
          : `Extract ${mode} to a new note`,
        affectedCount: 1
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
            : 'Rename a tag'
      }
    }
    default:
      return { kind: toolName, summary: toolName }
  }
}

function str(v: unknown): string {
  return typeof v === 'string' ? v.trim() : ''
}
