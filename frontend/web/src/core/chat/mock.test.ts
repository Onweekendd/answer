import { describe, expect, it } from "vitest";

import type { ChatEvent } from "./mapper";
import { mockReply } from "./mock";

async function collect(it: AsyncIterable<ChatEvent>) {
  const out: ChatEvent[] = [];
  for await (const e of it) {
    out.push(e);
  }
  return out;
}

const noSleep = async () => {};

describe("mockReply", () => {
  it("searches before it answers, and every tool start is matched by an end", async () => {
    const events = await collect(mockReply("iPhone 16 值得买吗", { sleep: noSleep }));

    const starts = events.filter((e) => e.type === "tool_start");
    const ends = events.filter((e) => e.type === "tool_end");
    expect(starts.length).toBeGreaterThan(0);
    expect(ends.map((e) => (e as { id: string }).id)).toEqual(starts.map((e) => (e as { id: string }).id));
  });

  it("only gives its verdict after the last tool call has finished", async () => {
    const events = await collect(mockReply("x", { sleep: noSleep }));

    const lastEnd = events.findLastIndex((e) => e.type === "tool_end");
    const firstVerdictText = events.findIndex((e, i) => e.type === "text" && i > lastEnd);
    expect(lastEnd).toBeGreaterThan(-1);
    expect(firstVerdictText).toBeGreaterThan(lastEnd);
  });

  it("mentions what the user asked about", async () => {
    const events = await collect(mockReply("戴森吹风机", { sleep: noSleep }));
    const text = events.flatMap((e) => (e.type === "text" ? [e.delta] : [])).join("");

    expect(text).toContain("戴森吹风机");
  });

  it("streams the answer in several pieces and ends with done", async () => {
    const events = await collect(mockReply("x", { sleep: noSleep }));

    expect(events.filter((e) => e.type === "text").length).toBeGreaterThan(3);
    expect(events.at(-1)).toEqual({ type: "done" });
  });

  it("waits between events so the UI really streams", async () => {
    let waits = 0;
    await collect(mockReply("x", { sleep: async () => void waits++ }));

    expect(waits).toBeGreaterThan(5);
  });

  it("stops early when aborted", async () => {
    const ctrl = new AbortController();
    let n = 0;
    const events: ChatEvent[] = [];
    for await (const e of mockReply("x", {
      sleep: async () => {},
      signal: ctrl.signal
    })) {
      events.push(e);
      if (++n === 2) {
        ctrl.abort();
      }
    }

    expect(events.length).toBeLessThan(5);
  });

  it("has something to say for an image-only message", async () => {
    const events = await collect(mockReply("", { sleep: noSleep }));

    expect(events.some((e) => e.type === "text")).toBe(true);
  });
});
