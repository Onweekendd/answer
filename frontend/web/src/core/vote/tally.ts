/** 1 buy, 2 wait, 3 skip; same numbers as entity.PurchaseVoteBuy / Wait / Skip in the Go backend. */
export type Choice = 1 | 2 | 3;

/** 0 means the viewer has not voted. */
export type MyChoice = Choice | 0;

export interface Tally {
  buy: number;
  wait: number;
  skip: number;
}

const KEY_OF: Record<Choice, keyof Tally> = { 1: "buy", 2: "wait", 3: "skip" };

export const CHOICES: readonly { choice: Choice; key: keyof Tally; label: string }[] = [
  { choice: 1, key: "buy", label: "买" },
  { choice: 2, key: "wait", label: "再等等" },
  { choice: 3, key: "skip", label: "不买" }
];

/**
 * The tally after the viewer moves from `prev` to `next`. Mirrors PurchaseRepo.Vote: a first
 * vote adds one, a change moves one across, and repeating the same choice does nothing. It is
 * used for the optimistic update, so the server's reply replaces it either way.
 */
export function applyVote(tally: Tally, prev: MyChoice, next: Choice): Tally {
  if (prev === next) {
    return { ...tally };
  }
  const out = { ...tally };
  if (prev !== 0) {
    out[KEY_OF[prev]] = Math.max(0, out[KEY_OF[prev]] - 1);
  }
  out[KEY_OF[next]] += 1;
  return out;
}

export const total = (t: Tally) => t.buy + t.wait + t.skip;

/** Whole percent of `sum`, 0 when nobody voted. */
export const percent = (count: number, sum: number) => (sum === 0 ? 0 : Math.round((count * 100) / sum));

/** The author may not vote on their own post, and nobody may once voting has closed. */
export const canVote = ({ closed, isAuthor }: { closed: boolean; isAuthor: boolean }) => !closed && !isAuthor;
