import { useMutation, useQueryClient } from '@tanstack/react-query'
import type { FeedItem } from '@/core/api/endpoints'
import { patchItem } from '@/core/feed/batch'
import { applyVote, type Choice, type MyChoice, type Tally } from '@/core/vote/tally'
import { api } from '@/lib/session'
import { FEED_KEY } from './useFeed'

const tallyOf = (item: FeedItem): Tally => ({
  buy: item.purchase?.buy_count ?? 0,
  wait: item.purchase?.wait_count ?? 0,
  skip: item.purchase?.skip_count ?? 0,
})

const withVote = (item: FeedItem, tally: Tally, mine: MyChoice, closed?: boolean): FeedItem => ({
  ...item,
  purchase: item.purchase && {
    ...item.purchase,
    buy_count: tally.buy,
    wait_count: tally.wait,
    skip_count: tally.skip,
    my_vote: mine,
    vote_closed: closed ?? item.purchase.vote_closed,
  },
})

/**
 * Votes on a feed card. The tally moves at once (applyVote), the server's numbers replace it
 * when they arrive, and a failure puts the card back the way it was.
 */
export function useVote() {
  const qc = useQueryClient()

  return useMutation({
    mutationFn: ({ id, choice }: { id: string; choice: Choice }) => api.vote(id, choice),

    onMutate: async ({ id, choice }) => {
      await qc.cancelQueries({ queryKey: FEED_KEY })
      const previous = qc.getQueryData<FeedItem[]>(FEED_KEY)
      qc.setQueryData<FeedItem[]>(FEED_KEY, (items) =>
        patchItem(items, id, (item) =>
          withVote(item, applyVote(tallyOf(item), item.purchase?.my_vote ?? 0, choice), choice),
        ),
      )
      return { previous }
    },

    onError: (_err, _vars, ctx) => {
      if (ctx?.previous) qc.setQueryData(FEED_KEY, ctx.previous)
    },

    onSuccess: (resp, { id }) => {
      qc.setQueryData<FeedItem[]>(FEED_KEY, (items) =>
        patchItem(items, id, (item) =>
          withVote(
            item,
            { buy: resp.buy_count, wait: resp.wait_count, skip: resp.skip_count },
            resp.my_choice,
            resp.closed,
          ),
        ),
      )
    },
  })
}
