import { describe, expect, it } from 'vitest'
import type { ChatEvent } from './mapper'
import { eventsFromSSE, foldEvents } from './stream'

async function* of(events: ChatEvent[]) {
  for (const e of events) yield e
}

const enc = new TextEncoder()
const body = (text: string) =>
  new ReadableStream<Uint8Array>({
    start(c) {
      c.enqueue(enc.encode(text))
      c.close()
    },
  })

async function collect<T>(it: AsyncIterable<T>) {
  const out: T[] = []
  for await (const x of it) out.push(x)
  return out
}

describe('foldEvents', () => {
  it('yields the whole message so far after each event, not just the new piece', async () => {
    const got = await collect(
      foldEvents(
        of([
          { type: 'text', delta: 'He' },
          { type: 'text', delta: 'llo' },
        ]),
      ),
    )

    expect(got).toEqual([
      { content: [{ type: 'text', text: 'He' }] },
      { content: [{ type: 'text', text: 'Hello' }] },
    ])
  })

  it('carries tool calls along with their results', async () => {
    const got = await collect(
      foldEvents(
        of([
          { type: 'tool_start', id: 't', name: 'web_search', args: { q: 'x' } },
          { type: 'tool_end', id: 't', result: 'ok' },
        ]),
      ),
    )

    expect(got.at(-1)?.content[0]).toMatchObject({ type: 'tool-call', toolCallId: 't', result: 'ok' })
  })

  it('does not emit anything for done', async () => {
    const got = await collect(foldEvents(of([{ type: 'text', delta: 'a' }, { type: 'done' }])))

    expect(got).toHaveLength(1)
  })

  it('passes a server error through as a thrown ChatStreamError', async () => {
    const run = () => collect(foldEvents(of([{ type: 'text', delta: 'a' }, { type: 'error', message: '额度用完了' }])))

    await expect(run()).rejects.toThrow('额度用完了')
  })

  it('stops reading once the abort signal fires', async () => {
    const ctrl = new AbortController()
    let pulled = 0
    async function* endless() {
      while (true) {
        pulled++
        yield { type: 'text', delta: 'x' } as const
      }
    }

    const seen: number[] = []
    const run = async () => {
      for await (const m of foldEvents(endless(), ctrl.signal)) {
        seen.push(m.content.length)
        if (seen.length === 3) ctrl.abort()
      }
    }
    await run()

    expect(seen).toHaveLength(3)
    expect(pulled).toBeLessThan(10)
  })
})

describe('eventsFromSSE', () => {
  it('turns SSE data lines into chat events', async () => {
    const got = await collect(
      eventsFromSSE(body('data: {"type":"text","delta":"a"}\n\ndata: {"type":"text","delta":"b"}\n\ndata: [DONE]\n\n')),
    )

    expect(got).toEqual([
      { type: 'text', delta: 'a' },
      { type: 'text', delta: 'b' },
    ])
  })

  it('skips lines that are not valid events instead of failing the stream', async () => {
    const got = await collect(eventsFromSSE(body('data: garbage\n\ndata: {"type":"text","delta":"ok"}\n\n')))

    expect(got).toEqual([{ type: 'text', delta: 'ok' }])
  })
})
