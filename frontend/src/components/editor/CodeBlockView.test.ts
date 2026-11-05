import { beforeEach, describe, expect, it, vi } from 'vitest'
import { mountNodeViewEditor } from '../../lib/editor/nodeview-test-harness'
import { themeState } from '../../theme/store.svelte'

const mocks = vi.hoisted(() => ({
  renderMermaid: vi.fn()
}))

vi.mock('$silt-app', () => createAppIpcMocks({}))

vi.mock('../../lib/editor/useMermaid', () => ({
  renderMermaid: mocks.renderMermaid
}))

async function mountMermaid(source: string) {
  const { editor, container, cleanup } = await mountNodeViewEditor([])
  editor.commands.setContent({
    type: 'doc',
    content: [
      {
        type: 'codeBlock',
        attrs: { language: 'mermaid' },
        content: [{ type: 'text', text: source }]
      }
    ]
  })
  await new Promise((resolve) => setTimeout(resolve, 0))
  return { editor, container, cleanup }
}

const settleRender = () => new Promise((resolve) => setTimeout(resolve, 230))

describe('CodeBlockView Mermaid integration', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('renders the Mermaid SVG from a code block using the active theme', async () => {
    mocks.renderMermaid.mockResolvedValue({
      svg: '<svg data-testid="diagram"></svg>',
      error: null
    })
    const { container, cleanup } = await mountMermaid('graph TD; A-->B')
    try {
      await settleRender()
      expect(mocks.renderMermaid).toHaveBeenCalledWith(
        'graph TD; A-->B',
        'dark'
      )
      expect(container.querySelector('.silt-mermaid-svg svg')).not.toBeNull()
      expect(container.querySelector('[role="alert"]')).toBeNull()
    } finally {
      cleanup()
    }
  })

  it('shows a renderer error for invalid diagram input', async () => {
    mocks.renderMermaid.mockResolvedValue({
      svg: '',
      error: 'Syntax error in graph'
    })
    const { container, cleanup } = await mountMermaid('not a diagram')
    try {
      await settleRender()
      expect(container.querySelector('[role="alert"]')?.textContent).toContain(
        'Syntax error in graph'
      )
      expect(container.querySelector('.silt-mermaid-svg')).toBeNull()
    } finally {
      cleanup()
    }
  })

  it('publishes only the latest result when renders resolve out of order', async () => {
    let resolveOld!: (value: { svg: string; error: null }) => void
    mocks.renderMermaid
      .mockImplementationOnce(
        () => new Promise((resolve) => (resolveOld = resolve))
      )
      .mockResolvedValueOnce({
        svg: '<svg data-version="new"></svg>',
        error: null
      })
    const { editor, container, cleanup } = await mountMermaid('graph TD; A-->B')
    try {
      await settleRender()
      editor.commands.setContent({
        type: 'doc',
        content: [
          {
            type: 'codeBlock',
            attrs: { language: 'mermaid' },
            content: [{ type: 'text', text: 'graph TD; A-->C' }]
          }
        ]
      })
      await settleRender()
      expect(container.querySelector('[data-version="new"]')).not.toBeNull()
      resolveOld({ svg: '<svg data-version="old"></svg>', error: null })
      await new Promise((resolve) => setTimeout(resolve, 0))
      expect(container.querySelector('[data-version="new"]')).not.toBeNull()
      expect(container.querySelector('[data-version="old"]')).toBeNull()
    } finally {
      cleanup()
    }
  })

  it('re-renders with the updated light theme', async () => {
    mocks.renderMermaid.mockResolvedValue({ svg: '<svg></svg>', error: null })
    const originalMode = themeState.mode
    const { container, cleanup } = await mountMermaid('graph TD; A-->B')
    try {
      await settleRender()
      themeState.mode = 'light'
      await settleRender()
      expect(mocks.renderMermaid).toHaveBeenLastCalledWith(
        'graph TD; A-->B',
        'default'
      )
      expect(container.querySelector('.silt-mermaid-svg svg')).not.toBeNull()
    } finally {
      themeState.mode = originalMode
      cleanup()
    }
  })
})
