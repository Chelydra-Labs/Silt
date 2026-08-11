// Agent tool — search_product_docs (#928).
//
// Read-only keyword search over the shipped product-help corpus. Not RAG-gated.
// Evidence uses sourceKind product_help so the chat UI does not navigate to vault blocks.

import type { PluginContext } from '../../../sdk'
import { asString } from '../../../../lib/asString'
import type { ToolResult } from '../tool-registry'
import {
  NO_MATCH_MESSAGE,
  searchProductDocs
} from '../product-docs/searchProductDocs'

export const searchProductDocsToolDef = {
  name: 'search_product_docs',
  description:
    'Search shipped Silt product help (setup, UI, templates, theming, backup, ' +
    'AI features). Use for how-to-use-Silt questions before inventing UI steps. ' +
    "Not for the user's personal notes — use search_notes / read_blocks for those.",
  parameters: {
    type: 'object',
    required: ['query'],
    properties: {
      query: {
        type: 'string',
        description: 'Natural-language or keyword query about using Silt.'
      },
      top_k: {
        type: 'integer',
        description: 'Max help excerpts to return (default 5, max 10).',
        minimum: 1,
        maximum: 10
      }
    }
  }
}

export function handleSearchProductDocs(
  _ctx: PluginContext,
  args: Record<string, unknown>
): Promise<ToolResult> {
  const query = asString(args.query).trim()
  if (!query) {
    return Promise.resolve({ content: '', error: 'query must not be empty' })
  }

  const hits = searchProductDocs(query, args.top_k)
  if (hits.length === 0) {
    return Promise.resolve({ content: NO_MATCH_MESSAGE })
  }

  const lines = hits.map((h, i) => {
    const idx = i + 1
    // displayTitle already includes "Silt help: Title › Section" when sectioned.
    return [
      `[${idx}] ${h.displayTitle}`,
      `    ${h.excerpt.replace(/\n/g, '\n    ')}`
    ].join('\n')
  })

  return Promise.resolve({
    content: `${hits.length} Silt help topic(s):\n\n${lines.join('\n\n')}`,
    evidence: hits.map((h, i) => ({
      citationIndex: i + 1,
      blockId: h.helpId,
      sourceKind: 'product_help' as const,
      title: h.displayTitle,
      snippet: h.excerpt.slice(0, 200)
    }))
  })
}
