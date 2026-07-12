// Extract action items → proposed GFM tasks (#232).

import type { PluginAIChatMessage, PluginContext } from '../../../sdk'
import { systemPromptFor } from '../prompts/defaults'
import { createProposal } from '../proposal/model'
import { isDuplicateTask, parseJsonObject } from '../text'
import type { AssistantSettings, Proposal, ScopeContext } from '../types'
import { completeBuffered } from './runChat'

export function buildExtractMessages(
  input: string,
  settings: AssistantSettings
): PluginAIChatMessage[] {
  return [
    {
      role: 'system',
      content: systemPromptFor(
        'extract-tasks',
        settings.prompt_overrides['extract-tasks']
      )
    },
    { role: 'user', content: input }
  ]
}

export function parseExtractTasks(raw: string): string[] {
  const obj = parseJsonObject(raw)
  if (!obj) return []
  const tasks = obj.tasks
  if (!Array.isArray(tasks)) return []
  return tasks
    .filter((t): t is string => typeof t === 'string')
    .map((t) => t.trim())
    .filter(Boolean)
}

export function dedupeTasks(
  tasks: string[],
  existing: string[] | undefined
): string[] {
  const out: string[] = []
  const seen = new Set<string>()
  for (const t of tasks) {
    if (isDuplicateTask(t, existing)) continue
    const key = t.trim().toLowerCase()
    if (seen.has(key)) continue
    seen.add(key)
    out.push(t.trim())
  }
  return out
}

export async function runExtractTasks(
  ctx: PluginContext,
  scope: ScopeContext,
  settings: AssistantSettings
): Promise<Proposal> {
  const messages = buildExtractMessages(scope.inputText, settings)
  let raw = (await completeBuffered(ctx, messages, 800)).content
  let tasks = dedupeTasks(parseExtractTasks(raw), scope.existingTaskTitles)
  if (tasks.length === 0 && raw.trim()) {
    // One retry with a stricter nudge.
    const retry: PluginAIChatMessage[] = [
      ...messages,
      { role: 'assistant', content: raw },
      {
        role: 'user',
        content:
          'Return ONLY valid JSON: {"tasks":["..."]}. No markdown fences.'
      }
    ]
    raw = (await completeBuffered(ctx, retry, 800)).content
    tasks = dedupeTasks(parseExtractTasks(raw), scope.existingTaskTitles)
  }

  const md = tasks.map((t) => `- [ ] ${t}`).join('\n')
  return createProposal({
    actionId: 'extract-tasks',
    kind: 'insert-tasks',
    scope,
    proposedMarkdown: md,
    tasks,
    warning: scope.truncated
      ? `Input truncated to ${settings.max_input_chars} characters.`
      : tasks.length === 0
        ? 'No new action items found (or all matched existing tasks).'
        : undefined,
    status: 'ready'
  })
}
