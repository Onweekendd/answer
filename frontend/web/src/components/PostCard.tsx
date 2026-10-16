import { VoteBar } from '@/components/VoteBar'
import { Card, CardContent } from '@/components/ui/card'
import type { FeedItem } from '@/core/api/endpoints'
import type { Choice } from '@/core/vote/tally'

interface Props {
  item: FeedItem
  pending?: boolean
  onVote: (choice: Choice) => void
}

const formatPrice = (price: number, currency?: string) =>
  price > 0 ? `${currency === 'CNY' || !currency ? '¥' : `${currency} `}${price}` : '未标价'

/** One community post: what it is, how much, and the vote. */
export function PostCard({ item, pending, onVote }: Props) {
  const p = item.purchase
  const images = p?.images.slice(0, 3) ?? []

  return (
    <Card>
      <CardContent className="space-y-4">
        <div className="space-y-1.5">
          <div className="flex items-start justify-between gap-3">
            <h2 className="text-base font-semibold leading-snug break-words">{item.title}</h2>
            {p && <span className="shrink-0 text-base font-semibold text-primary">{formatPrice(p.price, p.currency)}</span>}
          </div>
          {item.description && <p className="line-clamp-2 text-sm text-muted-foreground">{item.description}</p>}
        </div>

        {images.length > 0 && (
          <div className="grid grid-cols-3 gap-2">
            {images.map((src) => (
              <img key={src} src={src} alt="" loading="lazy" className="aspect-square w-full rounded-lg bg-muted object-cover" />
            ))}
          </div>
        )}

        {p && (
          <VoteBar
            tally={{ buy: p.buy_count ?? 0, wait: p.wait_count ?? 0, skip: p.skip_count ?? 0 }}
            myVote={p.my_vote ?? 0}
            closed={Boolean(p.vote_closed)}
            pending={pending}
            onVote={onVote}
          />
        )}
      </CardContent>
    </Card>
  )
}
