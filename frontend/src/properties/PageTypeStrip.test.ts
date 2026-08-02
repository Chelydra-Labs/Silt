import { describe, expect, it, afterEach, vi } from 'vitest'
import { cleanup, render, screen, fireEvent } from '@testing-library/svelte'
import PageTypeStrip from './PageTypeStrip.svelte'
import type { PageTypeInfo } from './types'

const typedInfo: PageTypeInfo = {
  typeId: 'book',
  type: { id: 'book', name: 'Book', icon: 'menu_book', heroField: 'title' },
  isSet: true,
  rawType: ''
}

const untypedInfo: PageTypeInfo = {
  typeId: '',
  type: { id: '', name: '' },
  isSet: false,
  rawType: ''
}

const rawInfo: PageTypeInfo = {
  typeId: '',
  type: { id: '', name: '' },
  isSet: false,
  rawType: 'mystery'
}

afterEach(cleanup)

describe('PageTypeStrip', () => {
  it('renders the type chip and hero value for a typed page', () => {
    render(PageTypeStrip, {
      props: { info: typedInfo, heroValue: 'Dune', onOpen: () => {} }
    })
    const chip = screen.getByRole('button')
    expect(chip.textContent).toContain('Book')
    expect(chip.textContent).toContain('Dune')
  })

  it('renders nothing for an untyped page', () => {
    render(PageTypeStrip, {
      props: { info: untypedInfo, heroValue: '', onOpen: () => {} }
    })
    expect(screen.queryByRole('button')).toBeNull()
  })

  it('renders a subdued raw chip for an unknown type and no hero', () => {
    render(PageTypeStrip, {
      props: { info: rawInfo, heroValue: '', onOpen: () => {} }
    })
    const chip = screen.getByRole('button')
    expect(chip.textContent).toContain('mystery')
    expect(chip.textContent).not.toContain('Book')
    expect(chip).toHaveClass('raw')
  })

  it('opens the panel when the chip is clicked', async () => {
    const onOpen = vi.fn()
    render(PageTypeStrip, {
      props: { info: typedInfo, heroValue: 'Dune', onOpen }
    })
    await fireEvent.click(screen.getByRole('button'))
    expect(onOpen).toHaveBeenCalledOnce()
  })

  it('omits the hero span when the hero value is empty', () => {
    const { container } = render(PageTypeStrip, {
      props: { info: typedInfo, heroValue: '', onOpen: () => {} }
    })
    // Chip present, but no hero text node rendered.
    const chip = screen.getByRole('button')
    expect(chip.textContent).toContain('Book')
    expect(container.querySelector('.hero')).toBeNull()
  })
})
