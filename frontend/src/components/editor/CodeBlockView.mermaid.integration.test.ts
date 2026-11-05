import { afterEach, describe, expect, it, vi } from 'vitest'
import { mountNodeViewEditor } from '../../lib/editor/nodeview-test-harness'
import { themeState } from '../../theme/store.svelte'

vi.mock('$silt-app', () => createAppIpcMocks({}))

const getBBoxDescriptor = Object.getOwnPropertyDescriptor(
  SVGElement.prototype,
  'getBBox'
)

// jsdom has no SVG layout engine. This supplies deterministic geometry only;
// it exercises Mermaid's installed parser/renderer, not browser layout fidelity.
Object.defineProperty(SVGElement.prototype, 'getBBox', {
  configurable: true,
  value: () => ({ x: 0, y: 0, width: 100, height: 20 })
})

afterEach(() => {
  if (getBBoxDescriptor) {
    Object.defineProperty(SVGElement.prototype, 'getBBox', getBBoxDescriptor)
  } else {
    Reflect.deleteProperty(SVGElement.prototype, 'getBBox')
  }
})

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
  return { container, cleanup }
}

describe('CodeBlockView with installed Mermaid renderer', () => {
  const originalMode = themeState.mode

  afterEach(() => {
    themeState.mode = originalMode
  })

  it('renders a valid diagram through the Silt rendering boundary', async () => {
    themeState.mode = 'light'
    const { container, cleanup } = await mountMermaid(
      'graph TD\n  A[Start] --> B[End]'
    )
    try {
      await vi.waitFor(
        () => {
          expect(
            container.querySelector('.silt-mermaid-svg svg')
          ).not.toBeNull()
        },
        { timeout: 5000 }
      )
      const svg = container.querySelector('.silt-mermaid-svg svg')
      expect(svg).not.toBeNull()
      expect(svg?.querySelectorAll('g.node').length).toBeGreaterThan(0)
      expect(container.querySelector('[role="alert"]')).toBeNull()
    } finally {
      cleanup()
    }
  })

  it('surfaces invalid input from the installed Mermaid parser', async () => {
    themeState.mode = 'light'
    const { container, cleanup } = await mountMermaid('not a valid diagram')
    try {
      await vi.waitFor(
        () => {
          expect(container.querySelector('[role="alert"]')).not.toBeNull()
        },
        { timeout: 5000 }
      )
      expect(container.querySelector('[role="alert"]')?.textContent).toMatch(
        /no diagram type detected/i
      )
      expect(container.querySelector('.silt-mermaid-svg')).toBeNull()
    } finally {
      cleanup()
    }
  })
})
