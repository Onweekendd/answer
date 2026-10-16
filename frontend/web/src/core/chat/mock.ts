import type { ChatEvent } from "./mapper";

export interface MockOptions {
  /** injectable so tests do not really wait */
  sleep?: (ms: number) => Promise<void>;
  signal?: AbortSignal;
}

const realSleep = (ms: number) => new Promise<void>((resolve) => setTimeout(resolve, ms));

/**
 * A scripted reply that looks like the real agent's: a search, a page read, then a streamed
 * verdict. Stands in for the chat endpoint until the Go side exists; it emits the same
 * ChatEvent shapes, so swapping it for the real stream changes nothing in the UI.
 */
export async function* mockReply(prompt: string, opts: MockOptions = {}): AsyncGenerator<ChatEvent> {
  const sleep = opts.sleep ?? realSleep;
  const subject = prompt.trim() || "这张截图里的商品";

  const script: [number, ChatEvent][] = [
    [300, { type: "text", delta: `好的，我来查一下「${subject}」。` }],
    [700, { type: "tool_start", id: "search-1", name: "web_search", args: { query: `${subject} 价格 口碑` } }],
    [1400, { type: "tool_end", id: "search-1", result: { hits: 8 } }],
    [300, { type: "tool_start", id: "page-1", name: "fetch_page", args: { url: "https://example.com/review" } }],
    [1000, { type: "tool_end", id: "page-1", result: { ok: true } }]
  ];

  const verdict = [
    "\n\n**结论：可以买，但别急着下单。**\n\n",
    "- 当前价格在近三个月的中间位置，不算低；\n",
    "- 口碑整体不错，差评主要集中在售后；\n",
    "- 大促前后通常会再降一档，**能等就再等等**。\n\n",
    "要不要我帮你发到社区，让大家也投个票？"
  ];

  for (const [wait, event] of script) {
    await sleep(wait);
    if (opts.signal?.aborted) {
      return;
    }
    yield event;
  }
  for (const piece of verdict) {
    for (const delta of piece.match(/.{1,6}/gs) ?? []) {
      await sleep(40);
      if (opts.signal?.aborted) {
        return;
      }
      yield { type: "text", delta };
    }
  }
  yield { type: "done" };
}
