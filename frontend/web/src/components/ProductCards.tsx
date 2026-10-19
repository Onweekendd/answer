import { makeAssistantDataUI } from "@assistant-ui/react";
import { ExternalLinkIcon, ImageOffIcon } from "lucide-react";

import { type ProductCard, PRODUCTS_PART } from "@/core/chat/mapper";

/**
 * The products the agent picked, one card each. The whole card opens the product page in a new
 * tab, so the user can check the price there themselves; the agent cannot always see it (Taobao
 * hides subsidised prices from the web page).
 */
export function ProductCards({ cards }: { cards: readonly ProductCard[] }) {
  return (
    <div className="my-3 grid grid-cols-2 gap-2 sm:grid-cols-3">
      {cards.map((card) => (
        <ProductCardView key={card.id} card={card} />
      ))}
    </div>
  );
}

function ProductCardView({ card }: { card: ProductCard }) {
  const host = hostOf(card.url);
  return (
    <a
      href={card.url}
      target="_blank"
      // noreferrer: the shop need not know which chat sent the user; nofollow: the link is the agent's pick, not ours
      rel="noopener noreferrer nofollow"
      className="group flex flex-col overflow-hidden rounded-xl border bg-card text-card-foreground transition-colors hover:border-foreground/25"
    >
      <div className="relative aspect-square w-full bg-muted">
        {card.image ? (
          <img src={card.image} alt="" loading="lazy" className="size-full object-cover" />
        ) : (
          <ImageOffIcon className="absolute inset-0 m-auto size-6 text-muted-foreground" aria-hidden />
        )}
      </div>
      <div className="flex flex-1 flex-col gap-1 p-2.5">
        <p className="line-clamp-2 text-sm leading-snug font-medium">{card.title}</p>
        {card.price && (
          <p className="text-sm">
            <span className="font-semibold text-primary tabular-nums">{card.price}</span>
            {card.priceNote && <span className="text-xs text-muted-foreground"> {card.priceNote}</span>}
          </p>
        )}
        {card.spec && <p className="text-xs text-muted-foreground">{card.spec}</p>}
        {card.note && <p className="line-clamp-2 text-xs text-muted-foreground">{card.note}</p>}
        <p className="mt-auto flex items-center gap-1 pt-1 text-xs text-muted-foreground">
          <span className="truncate">{card.shop || host}</span>
          <ExternalLinkIcon
            className="size-3 shrink-0 opacity-0 transition-opacity group-hover:opacity-100"
            aria-hidden
          />
        </p>
      </div>
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
