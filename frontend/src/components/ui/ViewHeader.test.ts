import { createRawSnippet } from 'svelte'
import { afterEach, describe, expect, it } from 'vitest'
import { cleanup, render, screen } from '@testing-library/svelte'
import ViewHeader from './ViewHeader.svelte'

function htmlSnippet(html: string) {
  return createRawSnippet(() => ({
    render: () => html
  }))
}

describe('ViewHeader primitive', () => {
  afterEach(() => {
    cleanup()
  })

  it('renders title with default h2 level and token border', () => {
    render(ViewHeader, {
      props: {
        title: 'Settings',
        dataTestId: 'header'
      }
    })

    const header = screen.getByTestId('header')
    expect(header).toBeInTheDocument()
    expect(header.className).toContain('border-b')
    expect(header.className).toContain('border-surface-panel-border')

    const heading = screen.getByRole('heading', { level: 2, name: 'Settings' })
    expect(heading).toBeInTheDocument()
    expect(heading.className).toContain('text-type-lg')
    expect(heading.className).toContain('text-text-primary')
  })

  it('supports h1 heading level, titleId, and custom icon', () => {
    render(ViewHeader, {
      props: {
        title: 'Tags Explorer',
        titleId: 'tags-title',
        headingLevel: 'h1',
        icon: 'label',
        dataTestId: 'tags-header'
      }
    })

    const heading = screen.getByRole('heading', {
      level: 1,
      name: 'Tags Explorer'
    })
    expect(heading).toBeInTheDocument()
    expect(heading.id).toBe('tags-title')

    const icon = screen.getByText('label')
    expect(icon).toBeInTheDocument()
    expect(icon).toHaveClass('material-symbols-outlined')
    expect(icon).toHaveAttribute('aria-hidden', 'true')
  })

  it('renders subtitle when provided', () => {
    render(ViewHeader, {
      props: {
        title: 'Editor Settings',
        subtitle: 'Configure typography and line wrapping',
        dataTestId: 'header'
      }
    })

    const subtitle = screen.getByText('Configure typography and line wrapping')
    expect(subtitle).toBeInTheDocument()
    expect(subtitle.className).toContain('text-text-muted')
    expect(subtitle.className).toContain('text-type-sm')
  })

  it('renders leading, meta, and actions snippets in standard layout positions', () => {
    render(ViewHeader, {
      props: {
        title: 'Type Dashboard',
        leading: htmlSnippet('<button data-testid="back-btn">Back</button>'),
        meta: htmlSnippet('<span data-testid="count-badge">42 items</span>'),
        actions: htmlSnippet('<button data-testid="action-btn">New</button>'),
        children: htmlSnippet(
          '<div data-testid="secondary-row">Filter tabs</div>'
        ),
        dataTestId: 'dashboard-header'
      }
    })

    expect(screen.getByTestId('back-btn')).toBeInTheDocument()
    expect(screen.getByTestId('count-badge')).toBeInTheDocument()
    expect(screen.getByTestId('action-btn')).toBeInTheDocument()
    expect(screen.getByTestId('secondary-row')).toBeInTheDocument()
  })
})
