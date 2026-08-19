// Agent tool — get_page_version.
//
// Previews a stored snapshot body. Does not mutate the live page.

import type { PluginContext } from '../../../sdk'
import { asString } from '../../../../lib/asString'
import type { ToolResult } from '../tool-registry'
import { breadcrumb } from './_util'

const MAX_OUTPUT_CHARS = 16_000

export const getPageVersionToolDef = {
  name: 'get_page_version',
  description:
    'Read a stored page-history snapshot as markdown body (no frontmatter). ' +
    'Does not change the live page. Pass version_id from list_page_versions.',
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

export async function handleGetPageVersion(
  ctx: PluginContext,
  args: Record<string, unknown>
): Promise<ToolResult> {
  const notebook = asString(args.notebook).trim()
  const section = asString(args.section).trim()
  const page = asString(args.page).trim()
  const versionId = asString(args.version_id).trim()
  if (!notebook || !page) {
    return { content: '', error: 'notebook and page are required' }
  }
  if (!versionId) {
    return { content: '', error: 'version_id is required' }
  }
  const body = await ctx.getPageVersion(notebook, section, page, versionId)
  const loc = breadcrumb(notebook, section, page)
  const clipped =
    body.length > MAX_OUTPUT_CHARS
      ? `${body.slice(0, MAX_OUTPUT_CHARS)}\n\n[output truncated: size limit reached]`
      : body
  return {
    content: `Version ${versionId} of ${loc} (read-only):\n\n${clipped}`
  }
}
