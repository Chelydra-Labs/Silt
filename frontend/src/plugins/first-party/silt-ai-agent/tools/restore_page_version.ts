// Agent tool — restore_page_version.
//
// Replaces the live page body with a stored snapshot via PluginContext.
// Mutating; staged by write-policy. Restore is reversible (pre-restore snapshot).

import type { PluginContext } from '../../../sdk'
import { asString } from '../../../../lib/asString'
import { coerceIPCError } from '../../../../lib/ipcError'
import { auditWrite } from './_util'
import type { ToolResult } from '../tool-registry'
import { breadcrumb } from './_util'

export const restorePageVersionToolDef = {
  name: 'restore_page_version',
  description:
    'Replace the live page body with a stored snapshot. Keeps current frontmatter ' +
    'and snapshots the pre-restore body so the restore can be undone. ' +
    'Use list_page_versions then get_page_version first. Mutating; the host confirms before applying.',
  parameters: {
    type: 'object',
    required: ['notebook', 'page', 'version_id'],
    properties: {
      notebook: { type: 'string', description: 'Notebook name.' },
      section: {
        type: 'string',
        description: 'Section path; empty or omit for a notebook-root page.'
      },
      page: { type: 'string', description: 'Page name without .md.' },
      version_id: {
        type: 'string',
        description: 'Version id from list_page_versions.'
      }
    }
  }
}

export async function handleRestorePageVersion(
  ctx: PluginContext,
  args: Record<string, unknown>
): Promise<ToolResult> {
  const notebook = asString(args.notebook).trim()
  const section = asString(args.section).trim()
  const page = asString(args.page).trim()
  const versionId = asString(args.version_id).trim()
  if (!notebook || !page) {
    auditWrite(ctx, 'restore_page_version', 'error')
    return { content: '', error: 'notebook and page are required' }
  }
  if (!versionId) {
    auditWrite(ctx, 'restore_page_version', 'error')
    return { content: '', error: 'version_id is required' }
  }
  try {
    await ctx.restorePageVersion(notebook, section, page, versionId)
  } catch (e) {
    auditWrite(ctx, 'restore_page_version', 'error')
    return {
      content: '',
      error: coerceIPCError(e).message
    }
  }
  auditWrite(ctx, 'restore_page_version', 'ok')
  const loc = breadcrumb(notebook, section, page)
  return {
    content: `Restored ${loc} to version ${versionId}. A snapshot of the previous page is kept when page history is on and the previous body was eligible to capture.`
  }
}
