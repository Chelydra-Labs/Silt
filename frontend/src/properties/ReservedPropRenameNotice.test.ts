import { describe, it, expect, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/svelte'
import ReservedPropRenameNotice from './ReservedPropRenameNotice.svelte'

describe('ReservedPropRenameNotice', () => {
  it('renders the migration notice when not dismissed', () => {
    render(ReservedPropRenameNotice, {
      props: {
        dismissed: false,
        renames: [
          { type_name: 'Book', from: 'created', to: 'created_value' },
          { type_name: 'Book', from: 'aliases', to: 'aliases_list' }
        ],
        onDismiss: vi.fn()
      }
    })
    const el = screen.getByTestId('reserved-prop-rename-notice')
    expect(el).toHaveAttribute('role', 'status')
    expect(el).toHaveAttribute('aria-live', 'polite')
    expect(el.textContent).toContain('Book: created → created_value')
    expect(el.textContent).toContain('aliases → aliases_list')
  })

  it('hides when dismissed', () => {
    render(ReservedPropRenameNotice, {
      props: {
        dismissed: true,
        renames: [{ from: 'created', to: 'created_value' }],
        onDismiss: vi.fn()
      }
    })
    expect(
      screen.queryByTestId('reserved-prop-rename-notice')
    ).not.toBeInTheDocument()
  })

  it('calls onDismiss when Got it is clicked', async () => {
    const onDismiss = vi.fn()
    render(ReservedPropRenameNotice, {
      props: {
        dismissed: false,
        renames: [],
        onDismiss
      }
    })
    await fireEvent.click(
      screen.getByRole('button', { name: 'Dismiss notice' })
    )
    expect(onDismiss).toHaveBeenCalledTimes(1)
  })

  it('falls back to generic copy when renames are empty', () => {
    render(ReservedPropRenameNotice, {
      props: {
        dismissed: false,
        renames: [],
        onDismiss: vi.fn()
      }
    })
    expect(
      screen.getByTestId('reserved-prop-rename-notice').textContent
    ).toContain('created and aliases are now core page fields')
  })
})
