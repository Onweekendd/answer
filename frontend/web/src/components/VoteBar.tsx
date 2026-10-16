import { Button } from "@/components/ui/button";
import { canVote, type Choice, CHOICES, type MyChoice, percent, type Tally, total } from "@/core/vote/tally";
import { cn } from "@/lib/utils";

// Full class names so Tailwind can see them; the colours are the buy/wait/skip theme tokens.
const SEGMENT: Record<keyof Tally, string> = {
  buy: "bg-buy",
  wait: "bg-wait",
  skip: "bg-skip"
};
const ACTIVE: Record<keyof Tally, string> = {
  buy: "border-buy bg-buy text-buy-foreground hover:bg-buy/90",
  wait: "border-wait bg-wait text-wait-foreground hover:bg-wait/90",
  skip: "border-skip bg-skip text-skip-foreground hover:bg-skip/90"
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
            className={cn("h-10 flex-col gap-0 py-1", myVote === choice && ACTIVE[key])}
            onClick={() => onVote(choice)}
          >
            <span className="text-sm font-medium leading-none">{label}</span>
            <span className="text-xs leading-none opacity-80">
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
