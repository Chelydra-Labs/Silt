import { describe, expect, it, afterEach, vi } from 'vitest'
import { render, screen, cleanup, fireEvent } from '@testing-library/svelte'
import PresetControl from './PresetControl.svelte'

afterEach(() => cleanup())

const numberOptions = [
  { value: 0.2, label: 'Precise', description: 'Consistent answers.' },
  { value: 0.5, label: 'Natural', description: 'Conversational answers.' },
  { value: 0.9, label: 'Creative', description: 'Varied answers.' }
]

const stringOptions = [
  { value: 'none', label: 'Quick', description: 'Fast responses.' },
  { value: 'medium', label: 'Standard', description: 'Balanced reasoning.' },
  { value: 'high', label: 'Deep', description: 'Thorough analysis.' }
]

describe('PresetControl', () => {
  it('renders options and fires onchange on selection', async () => {
    const onchange = vi.fn()
    render(PresetControl, {
      props: {
        label: 'Answer Style',
        tooltipText: 'How creative?',
        tooltipTechnical: 'Technical: Temperature (0.0-2.0).',
        options: numberOptions,
        value: 0.5,
        customMin: 0,
        customMax: 2,
        customStep: 0.1,
        customLabel: 'Temperature',
        onchange
      }
    })
    expect(screen.getByRole('radiogroup')).toBeInTheDocument()
    expect(screen.getByText('Conversational answers.')).toBeInTheDocument()
    await fireEvent.click(screen.getByRole('radio', { name: 'Precise' }))
    expect(onchange).toHaveBeenCalledWith(0.2)
  })

  it('shows Custom pill when value matches no preset', () => {
    render(PresetControl, {
      props: {
        label: 'Answer Style',
        tooltipText: 'How creative?',
        options: numberOptions,
        value: 0.7,
        customMin: 0,
        customMax: 2,
        onchange: vi.fn()
      }
    })
    expect(screen.getByText('Custom')).toBeInTheDocument()
    expect(screen.getByText(/Custom value/)).toBeInTheDocument()
  })

  it('Advanced disclosure reveals raw input and fires onchange', async () => {
    const onchange = vi.fn()
    render(PresetControl, {
      props: {
        label: 'Answer Style',
        tooltipText: 'How creative?',
        options: numberOptions,
        value: 0.5,
        customMin: 0,
        customMax: 2,
        customStep: 0.1,
        customLabel: 'Temperature',
        onchange
      }
    })
    const details = screen
      .getByText('Advanced')
      .closest('details') as HTMLDetailsElement
    details.open = true
    // Force re-render visibility of the advanced input.
    await fireEvent.click(screen.getByText('Advanced'))
    const input = document.getElementById(
      'preset-answer-style-custom'
    ) as HTMLInputElement
    expect(input).toBeTruthy()
    await fireEvent.change(input, { target: { value: '1.2' } })
    expect(onchange).toHaveBeenCalledWith(1.2)
  })

  it('supports string-typed presets with select Advanced', async () => {
    const onchange = vi.fn()
    render(PresetControl, {
      props: {
        label: 'Thinking Depth',
        tooltipText: 'How deep?',
        options: stringOptions,
        value: 'medium',
        customLabel: 'Reasoning effort',
        customSelectOptions: [
          { value: 'none', label: 'none' },
          { value: 'minimal', label: 'minimal' },
          { value: 'low', label: 'low' },
          { value: 'medium', label: 'medium' },
          { value: 'high', label: 'high' },
          { value: 'xhigh', label: 'xhigh' },
          { value: 'max', label: 'max' }
        ],
        onchange
      }
    })
    await fireEvent.click(screen.getByRole('radio', { name: 'Deep' }))
    expect(onchange).toHaveBeenCalledWith('high')
  })

  it('composes InfoTooltip for the label', () => {
    render(PresetControl, {
      props: {
        label: 'Search Balance',
        tooltipText: 'Keyword vs meaning.',
        options: numberOptions,
        value: 0.5,
        onchange: vi.fn()
      }
    })
    expect(
      screen.getByRole('button', { name: 'What is Search Balance?' })
    ).toBeInTheDocument()
  })
})
