// ChoiceDialog: open, primary, secondary, cancel, Esc (#664).
import { describe, it, expect, vi } from 'vitest'
import { render, fireEvent } from '@testing-library/svelte'
import ChoiceDialog from './ChoiceDialog.svelte'

describe('ChoiceDialog', () => {
  it('calls onPrimary when primary is clicked', async () => {
    const onPrimary = vi.fn()
    const onSecondary = vi.fn()
    const onCancel = vi.fn()
    const { getByTestId } = render(ChoiceDialog, {
      props: {
        title: 'Insert?',
        message: 'Choose where',
        primaryLabel: 'At cursor',
        secondaryLabel: 'Append',
        onPrimary,
        onSecondary,
        onCancel,
        dataTestId: 'choice'
      }
    })
    await fireEvent.click(getByTestId('choice-primary'))
    expect(onPrimary).toHaveBeenCalledOnce()
  })

  it('calls onSecondary when secondary is clicked', async () => {
    const onPrimary = vi.fn()
    const onSecondary = vi.fn()
    const onCancel = vi.fn()
    const { getByTestId } = render(ChoiceDialog, {
      props: {
        title: 'Insert?',
        message: 'Choose where',
        primaryLabel: 'At cursor',
        secondaryLabel: 'Append',
        onPrimary,
        onSecondary,
        onCancel,
        dataTestId: 'choice'
      }
    })
    await fireEvent.click(getByTestId('choice-secondary'))
    expect(onSecondary).toHaveBeenCalledOnce()
  })

  it('calls onCancel on Esc', async () => {
    const onCancel = vi.fn()
    render(ChoiceDialog, {
      props: {
        title: 'Insert?',
        message: 'Choose where',
        primaryLabel: 'At cursor',
        secondaryLabel: 'Append',
        onPrimary: vi.fn(),
        onSecondary: vi.fn(),
        onCancel,
        dataTestId: 'choice'
      }
    })
    await fireEvent.keyDown(window, { key: 'Escape' })
    expect(onCancel).toHaveBeenCalledOnce()
  })
})
