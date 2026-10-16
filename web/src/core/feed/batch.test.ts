import { describe, expect, it } from 'vitest'
import { dedupeById, patchItem } from './batch'

describe('dedupeById', () => {
  it('keeps the first occurrence and the original order', () => {
    const got = dedupeById([{ id: 'a' }, { id: 'b' }, { id: 'a' }, { id: 'c' }])

    expect(got.map((x) => x.id)).toEqual(['a', 'b', 'c'])
  })

  it('returns an empty list for an empty or missing batch', () => {
    expect(dedupeById([])).toEqual([])
    expect(dedupeById(undefined)).toEqual([])
    expect(dedupeById(null)).toEqual([])
  })
})

describe('patchItem', () => {
  const items = [
    { id: 'a', n: 1 },
    { id: 'b', n: 2 },
  ]

  it('replaces only the item with the given id', () => {
    const got = patchItem(items, 'b', (x) => ({ ...x, n: 9 }))

    expect(got).toEqual([
      { id: 'a', n: 1 },
      { id: 'b', n: 9 },
    ])
  })

  it('leaves untouched items as the same objects and does not mutate the input', () => {
    const got = patchItem(items, 'b', (x) => ({ ...x, n: 9 }))

    expect(got[0]).toBe(items[0])
    expect(items[1]).toEqual({ id: 'b', n: 2 })
  })

  it('returns an equal list when the id is not there', () => {
    expect(patchItem(items, 'zzz', (x) => ({ ...x, n: 0 }))).toEqual(items)
  })

  it('treats a missing list as empty', () => {
    expect(patchItem(undefined, 'a', (x) => x)).toEqual([])
  })
})
