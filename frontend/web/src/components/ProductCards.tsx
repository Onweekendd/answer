import { makeAssistantDataUI } from "@assistant-ui/react";
import { ExternalLinkIcon, ImageOffIcon } from "lucide-react";

import { type ProductCard, PRODUCTS_PART } from "@/core/chat/mapper";

/**
 * The products the answer talked about, below it, one row each: a small picture, the title, the
 * price and the agent's note. A row opens the product page in a new tab, so the user can check
 * the price there themselves; the agent cannot always see it (Taobao hides subsidised prices from
 * the web page).
 */
export function ProductCards({ cards }: { cards: readonly ProductCard[] }) {
  if (cards.length === 0) {
    return null;
  }
  return (
    <section aria-label="提到的商品" className="mt-4 flex flex-col gap-1.5">
      <h3 className="text-xs text-muted-foreground">提到的商品</h3>
      <ul className="flex flex-col divide-y overflow-hidden rounded-xl border bg-card text-card-foreground">
        {cards.map((card) => (
          <li key={card.id}>
            <ProductRow card={card} />
          </li>
        ))}
      </ul>
    </section>
  );
}

function ProductRow({ card }: { card: ProductCard }) {
  const where = [card.shop || hostOf(card.url), card.spec].filter(Boolean).join(" · ");
  return (
    <a
      href={card.url}
      target="_blank"
      // noreferrer: the shop need not know which chat sent the user; nofollow: the link is the agent's pick, not ours
      rel="noopener noreferrer nofollow"
      className="group flex items-center gap-3 p-2.5 transition-colors hover:bg-muted/60"
    >
      <div className="relative size-16 shrink-0 overflow-hidden rounded-lg bg-muted">
        {card.image ? (
          <img src={card.image} alt="" loading="lazy" className="size-full object-cover" />
        ) : (
          <ImageOffIcon className="absolute inset-0 m-auto size-5 text-muted-foreground" aria-hidden />
        )}
      </div>
      <div className="flex min-w-0 flex-1 flex-col gap-0.5 text-xs leading-snug">
        <p className="truncate text-sm font-medium">{card.title}</p>
        {card.price && (
          <p className="truncate">
            <span className="text-sm font-semibold text-primary tabular-nums">{card.price}</span>
            {card.priceNote && <span className="text-muted-foreground"> {card.priceNote}</span>}
          </p>
        )}
        {card.note && <p className="truncate text-muted-foreground">{card.note}</p>}
        {where && <p className="truncate text-muted-foreground">{where}</p>}
      </div>
      <ExternalLinkIcon
        className="size-3.5 shrink-0 text-muted-foreground opacity-40 transition-opacity group-hover:opacity-100"
        aria-hidden
      />
    </a>
  );
}

function hostOf(url: string): string {
  try {
    return new URL(url).hostname;
  } catch {
    return "";
  }
}

/** Renders the message's products part; mount it inside the runtime provider. */
export const ProductCardsUI = makeAssistantDataUI<ProductCard[]>({
  name: PRODUCTS_PART,
  render: ({ data }) => <ProductCards cards={data} />
});
