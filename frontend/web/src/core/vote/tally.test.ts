import { describe, expect, it } from "vitest";

import { applyVote, canVote, percent, type Tally, total } from "./tally";

const t = (buy: number, wait: number, skip: number): Tally => ({ buy, wait, skip });

describe("applyVote", () => {
  it("first vote adds one to the chosen option", () => {
    expect(applyVote(t(0, 0, 0), 0, 1)).toEqual(t(1, 0, 0));
    expect(applyVote(t(2, 3, 4), 0, 3)).toEqual(t(2, 3, 5));
  });

  it("changing a vote moves one from the old option to the new one", () => {
    expect(applyVote(t(1, 0, 0), 1, 3)).toEqual(t(0, 0, 1));
    expect(applyVote(t(5, 2, 1), 2, 1)).toEqual(t(6, 1, 1));
  });

  it("voting the same option again changes nothing", () => {
    expect(applyVote(t(1, 0, 0), 1, 1)).toEqual(t(1, 0, 0));
  });

  it("does not mutate the input", () => {
    const before = t(1, 0, 0);

    applyVote(before, 1, 2);

    expect(before).toEqual(t(1, 0, 0));
  });

  it("never goes below zero when the old tally is stale", () => {
    expect(applyVote(t(0, 0, 0), 1, 2)).toEqual(t(0, 1, 0));
  });
});

describe("total and percent", () => {
  it("sums the three options", () => {
    expect(total(t(1, 2, 3))).toBe(6);
  });

  it("rounds to whole percent", () => {
    expect(percent(1, 3)).toBe(33);
    expect(percent(2, 3)).toBe(67);
  });

  it("is 0 when nobody voted, not NaN", () => {
    expect(percent(0, 0)).toBe(0);
  });
});

describe("canVote", () => {
  it("allows an open post the viewer does not own", () => {
    expect(canVote({ closed: false, isAuthor: false })).toBe(true);
  });

  it("blocks closed posts and the author", () => {
    expect(canVote({ closed: true, isAuthor: false })).toBe(false);
    expect(canVote({ closed: false, isAuthor: true })).toBe(false);
  });
});
