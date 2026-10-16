// @vitest-environment jsdom
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, renderHook, waitFor } from '@testing-library/react'
import type { ReactNode } from 'react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { FeedItem, VoteResp } from '@/core/api/endpoints'

const vote = vi.hoisted(() => vi.fn())
vi.mock('@/lib/session', () => ({ api: { vote }, tokenStore: {} }))

import { FEED_KEY } from './useFeed'
import { useVote } from './useVote'

const item = (over: Partial<FeedItem['purchase']> = {}): FeedItem => ({
  id: '1',
  title: 't',
  url_title: 't',
  description: '',
  purchase: { images: [], price: 1, buy_count: 2, wait_count: 0, skip_count: 0, my_vote: 0, ...over },
})

function setup(initial: FeedItem[]) {
  const qc = new QueryClient({ defaultOptions: { mutations: { retry: false } } })
  qc.setQueryData(FEED_KEY, initial)
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={qc}>{children}</QueryClientProvider>
  )
  const feed = () => qc.getQueryData<FeedItem[]>(FEED_KEY)![0].purchase!
  return { ...renderHook(() => useVote(), { wrapper }), feed }
}

// braces matter: a returned function would be run by vitest as the teardown
beforeEach(() => {
  vote.mockReset()
})

describe('useVote', () => {
  it('moves the tally before the server answers', async () => {
    vote.mockReturnValue(new Promise(() => {})) // never settles
    const { result, feed } = setup([item()])

    act(() => result.current.mutate({ id: '1', choice: 1 }))

    await waitFor(() => expect(feed()).toMatchObject({ buy_count: 3, my_vote: 1 }))
  })

  it('replaces the guess with the numbers the server returns', async () => {
    const reply: VoteResp = { buy_count: 9, wait_count: 1, skip_count: 0, my_choice: 1, closed: true }
    vote.mockResolvedValue(reply)
    const { result, feed } = setup([item()])

    await act(() => result.current.mutateAsync({ id: '1', choice: 1 }))

    expect(feed()).toMatchObject({ buy_count: 9, wait_count: 1, my_vote: 1, vote_closed: true })
  })

  it('puts the card back when the vote is rejected', async () => {
    vote.mockRejectedValue(new Error('vote closed'))
    const { result, feed } = setup([item()])

    await act(() => result.current.mutateAsync({ id: '1', choice: 2 }).catch(() => {}))

    expect(feed()).toMatchObject({ buy_count: 2, wait_count: 0, my_vote: 0 })
  })

  it('changing a vote moves one across instead of adding', async () => {
    vote.mockReturnValue(new Promise(() => {}))
    const { result, feed } = setup([item({ buy_count: 1, my_vote: 1 })])

    act(() => result.current.mutate({ id: '1', choice: 3 }))

    await waitFor(() => expect(feed()).toMatchObject({ buy_count: 0, skip_count: 1, my_vote: 3 }))
  })
})
