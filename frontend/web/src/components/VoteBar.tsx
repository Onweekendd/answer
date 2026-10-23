import { Check } from "lucide-react";

import { Button } from "@/components/ui/button";
import { canVote, type Choice, CHOICES, type MyChoice, percent, type Tally, total } from "@/core/vote/tally";
import { cn } from "@/lib/utils";

// Full class names so Tailwind can see them; the colours are the buy/wait/skip theme tokens.
const SEGMENT: Record<keyof Tally, string> = {
  buy: "bg-buy",
  wait: "bg-wait",
  skip: "bg-skip"
};

interface Props {
  tally: Tally;
  myVote: MyChoice;
  closed: boolean;
  isAuthor?: boolean;
  pending?: boolean;
  onVote: (choice: Choice) => void;
}

/** Three-colour ratio bar plus one button per choice. Pure display: all state comes from props. */
export function VoteBar({ tally, myVote, closed, isAuthor = false, pending = false, onVote }: Props) {
  const sum = total(tally);
  const enabled = canVote({ closed, isAuthor }) && !pending;

  return (
    <div className="space-y-3">
      <div
        className="flex h-2 overflow-hidden rounded-full bg-muted"
        role="img"
        aria-label={`买 ${tally.buy}，再等等 ${tally.wait}，不买 ${tally.skip}`}
      >
        {CHOICES.map(({ key }) =>
          tally[key] > 0 ? (
            <div
              key={key}
              className={cn("transition-all", SEGMENT[key])}
              style={{ width: `${(tally[key] * 100) / sum}%` }}
            />
          ) : null
        )}
      </div>

      <div className="grid grid-cols-3 gap-2">
        {CHOICES.map(({ choice, key, label }) => (
          <Button
            key={key}
            variant="outline"
            size="lg"
            disabled={!enabled}
            aria-pressed={myVote === choice}
            className={cn("purchase-vote-button h-12 min-w-0 flex-col gap-1 py-1.5", `purchase-vote-${key}`)}
            onClick={() => onVote(choice)}
          >
            <span className="flex items-center gap-1 text-sm font-semibold leading-none">
              {myVote === choice && <Check aria-hidden="true" className="size-3.5" />}
              {label}
            </span>
            <span className="text-xs leading-none tabular-nums opacity-80">
              {tally[key]}
              {sum > 0 && ` · ${percent(tally[key], sum)}%`}
            </span>
          </Button>
        ))}
      </div>

      {closed && <p className="text-xs text-muted-foreground">投票已结束，共 {sum} 票</p>}
    </div>
  );
}
