import { describe, expect, it } from 'vitest'
import { lastUserText } from './prompt'

const user = (...content: { type: string; text?: string }[]) => ({ role: 'user', content })
const assistant = (text: string) => ({ role: 'assistant', content: [{ type: 'text', text }] })

describe('lastUserText', () => {
  it('returns the text of the latest user message', () => {
    const got = lastUserText([user({ type: 'text', text: '第一个问题' }), assistant('回答'), user({ type: 'text', text: '第二个问题' })])

    expect(got).toBe('第二个问题')
  })

  it('joins several text parts and ignores images', () => {
    const got = lastUserText([user({ type: 'text', text: 'a' }, { type: 'image' }, { type: 'text', text: 'b' })])

    expect(got).toBe('a\nb')
  })

  it('is empty for an image-only message', () => {
    expect(lastUserText([user({ type: 'image' })])).toBe('')
  })

  it('is empty when there is no user message', () => {
    expect(lastUserText([])).toBe('')
    expect(lastUserText([assistant('hi')])).toBe('')
  })

  it('trims surrounding whitespace', () => {
    expect(lastUserText([user({ type: 'text', text: '  hello \n' })])).toBe('hello')
  })
})
