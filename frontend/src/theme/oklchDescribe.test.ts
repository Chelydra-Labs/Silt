import { describe, expect, it } from 'vitest'
import { describeOklch, describeOklchPhrase } from './oklchDescribe'

describe('describeOklch', () => {
  it('describes a vivid blue', () => {
    const phrase = describeOklchPhrase({ L: 0.55, C: 0.18, H: 250 })
    expect(phrase).toMatch(/blue/)
    expect(phrase).toMatch(/vibrant|/)
  })

  it('describes near-gray as gray', () => {
    expect(describeOklchPhrase({ L: 0.5, C: 0.01, H: 0 })).toMatch(/gray/)
  })

  it('includes channel numbers in full valuetext', () => {
    const text = describeOklch({ L: 0.65, C: 0.12, H: 140 })
    expect(text).toMatch(/65\.0 percent lightness/)
    expect(text).toMatch(/chroma 0\.120/)
    expect(text).toMatch(/140 degrees/)
  })
})
