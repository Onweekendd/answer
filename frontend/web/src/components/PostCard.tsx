import { Images, MessageCircle } from "lucide-react";
import { useState } from "react";

import { VoteBar } from "@/components/VoteBar";
import type { FeedItem } from "@/core/api/endpoints";
import type { Choice } from "@/core/vote/tally";

interface Props {
  item: FeedItem;
  pending?: boolean;
  onVote: (choice: Choice) => void;
}

const formatPrice = (price: number, currency?: string) =>
  `${currency === "CNY" || !currency ? "¥" : `${currency} `}${price.toLocaleString("zh-CN")}`;

/** An image-led community card; actions stay outside the detail link. */
export function PostCard({ item, pending, onVote }: Props) {
  const p = item.purchase;
  const cover = p?.images?.[0];
  const [failedCover, setFailedCover] = useState<string>();
  const [failedAvatar, setFailedAvatar] = useState<string>();
  const author = item.operator?.display_name || item.operator?.username || "社区朋友";
  const avatar = item.operator?.avatar;
  const category = item.tags?.[0]?.display_name;
  const href = `/questions/${encodeURIComponent(item.id)}`;

  return (
    <article className="community-card mb-7 break-inside-avoid" aria-label={item.title}>
      <a
        href={href}
        className="group relative block overflow-hidden rounded-2xl bg-muted focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-ring"
      >
        {cover && failedCover !== cover ? (
          <img
            src={cover}
            alt={item.title}
            loading="lazy"
            decoding="async"
            onError={() => setFailedCover(cover)}
            className="max-h-[30rem] min-h-52 w-full object-cover transition-transform duration-300 group-hover:scale-[1.025]"
          />
        ) : (
          <div className="flex aspect-[4/5] flex-col justify-between bg-gradient-to-br from-primary/15 via-muted to-wait/15 p-7">
            <span className="text-xs font-medium tracking-[0.2em] text-muted-foreground">买 / 不 / 买</span>
            <p className="text-3xl leading-relaxed font-semibold tracking-tight text-foreground">{item.title}</p>
            <span className="text-sm text-muted-foreground">有点心动，也有点纠结。</span>
          </div>
        )}
        {category && (
          <span className="absolute top-3 left-3 rounded-full bg-background/90 px-2.5 py-1 text-xs font-medium backdrop-blur">
            {category}
          </span>
        )}
        {p && p.price > 0 && (
          <span className="absolute bottom-3 left-3 rounded-full bg-background/95 px-3 py-1 text-sm font-semibold shadow-sm backdrop-blur">
            {formatPrice(p.price, p.currency)}
          </span>
        )}
        {p && p.images?.length > 1 && (
          <span className="absolute right-3 bottom-3 flex items-center gap-1 rounded-full bg-black/50 px-2 py-1 text-xs text-white">
            <Images className="size-3" />
            {p.images.length}
          </span>
        )}
      </a>

      <div className="space-y-3 px-1 pt-3">
        <a
          href={href}
          className="block rounded-sm hover:text-primary focus-visible:outline-2 focus-visible:outline-ring"
        >
          <h2 className="line-clamp-2 text-[15px] leading-relaxed font-semibold break-words">{item.title}</h2>
        </a>
        {item.description && (
          <p className="line-clamp-2 text-xs leading-relaxed text-muted-foreground">{item.description}</p>
        )}
        <div className="flex min-w-0 items-center justify-between gap-2 text-xs text-muted-foreground">
          <div className="flex min-w-0 items-center gap-2">
            {avatar && failedAvatar !== avatar ? (
              <img
                src={avatar}
                alt=""
                loading="lazy"
                onError={() => setFailedAvatar(avatar)}
                className="size-6 shrink-0 rounded-full object-cover"
              />
            ) : (
              <span
                aria-hidden="true"
                className="flex size-6 shrink-0 items-center justify-center rounded-full bg-primary/10 text-primary"
              >
                {[...author][0]}
              </span>
            )}
            <span className="truncate">{author}</span>
          </div>
          <a
            href={href}
            aria-label={`${item.answer_count ?? 0} 条讨论`}
            className="flex shrink-0 items-center gap-1 hover:text-foreground"
          >
            <MessageCircle className="size-3.5" />
            {item.answer_count ?? 0}
          </a>
        </div>
        {p && (
          <VoteBar
            tally={{ buy: p.buy_count ?? 0, wait: p.wait_count ?? 0, skip: p.skip_count ?? 0 }}
            myVote={p.my_vote ?? 0}
            closed={Boolean(p.vote_closed)}
            pending={pending}
            onVote={onVote}
          />
        )}
      </div>
    </article>
  );
}
