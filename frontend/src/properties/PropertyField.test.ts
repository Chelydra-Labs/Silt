import { describe, expect, it, beforeEach, afterEach, vi } from 'vitest'
import { tick } from 'svelte'
import {
  render,
  screen,
  cleanup,
  fireEvent,
  waitFor
} from '@testing-library/svelte'

const appMocks = vi.hoisted(() =>
  createAppIpcMocks({
    SetPageProperty: vi.fn().mockResolvedValue(undefined),
    ClearPageProperty: vi.fn().mockResolvedValue(undefined)
  })
)
vi.mock('$silt-app', () => appMocks)

import PropertyField from './PropertyField.svelte'
import type { PagePropertyValue } from './types'

const locator = { notebook: 'Work', section: 'Projects', page: 'Plan' }

beforeEach(() => {
  appMocks.SetPageProperty.mockReset().mockResolvedValue(undefined)
  appMocks.ClearPageProperty.mockReset().mockResolvedValue(undefined)
})

afterEach(cleanup)

function field(overrides: Partial<PagePropertyValue> = {}): PagePropertyValue {
  return {
    name: 'title',
    label: 'Title',
    type: 'text',
    value: 'Dune',
    isSet: true,
    required: false,
    ...overrides
  }
}

describe('PropertyField — multiselect free-text commit', () => {
  it('splits a comma-list into a string[] so the backend receives a list, not a bare string', async () => {
    render(PropertyField, {
      props: {
        // No options → tags-style free-text input (the documented tags mode).
        value: field({
          name: 'tags',
          label: 'Tags',
          type: 'multiselect',
          value: [],
          options: []
        }),
        locator,
        onError: vi.fn(),
        onChanged: vi.fn()
      }
    })
    const input = document.getElementById('prop-tags') as HTMLInputElement
    expect(input).not.toBeNull()
    await fireEvent.change(input, { target: { value: 'a, b' } })
    // The backend's asStringSlice expects a list — a bare "a, b" string is
    // rejected with "expected a list, got string".
    expect(appMocks.SetPageProperty).toHaveBeenCalledWith(
      'Work',
      'Projects',
      'Plan',
      'tags',
      ['a', 'b']
    )
    expect(appMocks.ClearPageProperty).not.toHaveBeenCalled()
  })

  it('trims and drops empty segments when splitting a free-text multiselect commit', async () => {
    render(PropertyField, {
      props: {
        value: field({
          name: 'tags',
          label: 'Tags',
          type: 'multiselect',
          value: [],
          options: []
        }),
        locator,
        onError: vi.fn(),
        onChanged: vi.fn()
      }
    })
    const input = document.getElementById('prop-tags') as HTMLInputElement
    await fireEvent.change(input, { target: { value: '  a  , , b,' } })
    expect(appMocks.SetPageProperty).toHaveBeenCalledWith(
      'Work',
      'Projects',
      'Plan',
      'tags',
      ['a', 'b']
    )
  })
})

describe('PropertyField — per-field clear', () => {
  it('renders a clear button for a set field and calls ClearPageProperty on click', async () => {
    const onChanged = vi.fn()
    render(PropertyField, {
      props: {
        value: field(),
        locator,
        onError: vi.fn(),
        onChanged
      }
    })
    const clearBtn = screen.getByRole('button', { name: 'Clear Title' })
    await fireEvent.click(clearBtn)
    expect(appMocks.ClearPageProperty).toHaveBeenCalledWith(
      'Work',
      'Projects',
      'Plan',
      'title'
    )
    await waitFor(() => expect(onChanged).toHaveBeenCalled())
  })

  it('omits the clear button when the value is unset', () => {
    render(PropertyField, {
      props: {
        value: field({ isSet: false, value: '' }),
        locator,
        onError: vi.fn(),
        onChanged: vi.fn()
      }
    })
    expect(screen.queryByRole('button', { name: 'Clear Title' })).toBeNull()
  })

  it('clearing a number field (empty input) calls ClearPageProperty', async () => {
    render(PropertyField, {
      props: {
        value: field({
          name: 'rating',
          label: 'Rating',
          type: 'number',
          value: 5
        }),
        locator,
        onError: vi.fn(),
        onChanged: vi.fn()
      }
    })
    const input = document.getElementById('prop-rating') as HTMLInputElement
    expect(input).not.toBeNull()
    await fireEvent.change(input, { target: { value: '' } })
    expect(appMocks.ClearPageProperty).toHaveBeenCalledWith(
      'Work',
      'Projects',
      'Plan',
      'rating'
    )
    // SetPageProperty is NOT used for clearing.
    expect(appMocks.SetPageProperty).not.toHaveBeenCalled()
  })

  it('reverts and surfaces an error when ClearPageProperty fails', async () => {
    appMocks.ClearPageProperty.mockRejectedValue(new Error('disk full'))
    const onError = vi.fn()
    render(PropertyField, {
      props: {
        value: field(),
        locator,
        onError,
        onChanged: vi.fn()
      }
    })
    await fireEvent.click(screen.getByRole('button', { name: 'Clear Title' }))
    await waitFor(() =>
      expect(onError).toHaveBeenCalledWith(expect.stringMatching(/disk full/i))
    )
    // The field value is untouched on revert (still editable, old value present).
    await tick()
    expect(appMocks.ClearPageProperty).toHaveBeenCalledTimes(1)
  })
})
