import { describe, expect, it } from 'vitest'
import { resolveSettings } from '../settings'
import { filterTagSuggestions, parseSuggestedTags } from './suggestTags'

describe('suggest tags', () => {
  it('parses tags JSON', () => {
    expect(parseSuggestedTags('{"tags":["work/project","life"]}')).toEqual([
      'work/project',
      'life'
    ])
  })

  it('filters to existing vocab when enabled', () => {
    const settings = resolveSettings({
      existing_vocab_only: true,
      max_tag_suggestions: 5
    })
    const out = filterTagSuggestions(
      ['work/project', 'brand-new-top', 'life'],
      ['work/project', 'life/health'],
      settings
    )
    expect(out.map((t) => t.tag)).toEqual(['work/project'])
    expect(out[0].existing).toBe(true)
  })

  it('allows new tags when existing_vocab_only is false', () => {
    const settings = resolveSettings({
      existing_vocab_only: false,
      max_tag_suggestions: 5
    })
    const out = filterTagSuggestions(['brand-new'], ['work'], settings)
    expect(out.map((t) => t.tag)).toEqual(['brand-new'])
    expect(out[0].existing).toBe(false)
  })
})
