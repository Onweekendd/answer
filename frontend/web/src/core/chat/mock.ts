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
  if (/社区|发帖/.test(prompt)) {
    yield* mockDraft(sleep, opts.signal);
    return;
  }
  const subject = prompt.trim() || "这张截图里的商品";

  const script: [number, ChatEvent][] = [
    [300, { type: "text", delta: `好的，我来查一下「${subject}」。` }],
    [700, { type: "tool_start", id: "search-1", name: "web_search", args: { query: `${subject} 价格 口碑` } }],
    [1400, { type: "tool_end", id: "search-1", result: { hits: 8 } }],
    [300, { type: "tool_start", id: "page-1", name: "fetch_page", args: { url: "https://example.com/review" } }],
    [1000, { type: "tool_end", id: "page-1", result: { ok: true } }],
    [
      200,
      {
        type: "product",
        product: {
          id: 1,
          title: `${subject}（示例）`,
          shop: "示例旗舰店",
          price: "¥999",
          priceNote: "券后",
          url: "https://example.com/item/1"
        }
      }
    ]
  ];

  // the chat leaves buy / wait / skip to the community's vote, so the mock does too
  const verdict = [
    "\n\n现在的到手价在近三个月的中间位置，不算低。\n\n",
    "- 口碑整体不错，差评主要集中在售后；\n",
    "- 大促前后通常会再降一档；\n",
    "- 同价位还有一款参数相近的可以对比。\n\n",
    "拿不准的话，可以让我帮你发到社区，请大家投个票。"
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
  yield { type: "followups", followups: ["帮我发到社区，让大家投票", "京东和天猫哪家更划算？", "等双 11 能便宜多少？"] };
  yield { type: "done" };
}

/** The pictures of the mock draft: two the agent saved, as the local server has them. */
const mockPictures = () => {
  const origin = globalThis.location?.origin ?? "http://localhost";
  return [`${origin}/uploads/agent/5UmNQvVAoeW.jpg`, `${origin}/uploads/agent/5UmNVkmM1XA.jpg`];
};

/** "帮我发到社区": the agent drafts a post and hands it over (tool_suspend), as drafts.go does. */
async function* mockDraft(sleep: (ms: number) => Promise<void>, signal?: AbortSignal): AsyncGenerator<ChatEvent> {
  const pictures = mockPictures();
  const script: [number, ChatEvent][] = [
    [300, { type: "tool_start", id: "0-0", name: "draft_post", args: { summary: "整理帖子草稿" } }],
    [
      600,
      {
        type: "tool_suspend",
        id: "0-0",
        name: "draft_post",
        input: {
          draftId: "mock",
          title: "iPad Air M3 现在入手，还是等双 11？",
          content: [
            "想买来记笔记、看网课，预算 4500 以内。",
            "",
            "- 京东自营 128G WLAN 现在 4299，带 200 元券",
            "- 天猫旗舰店 4399，赠品是保护壳",
            "- 去年双 11 同款到手价 3999",
            "",
            "纠结点：现在买能早用两个月，等的话大概能省 300。"
          ].join("\n"),
          images: pictures.slice(0, 1),
          candidates: pictures,
          price: 4299,
          link: "https://item.jd.com/100000000000.html",
          category: "数码",
          vote: 2
        }
      }
    ],
    [200, { type: "tool_end", id: "0-0", result: { summary: "草稿交给你确认", ok: true } }],
    [
      200,
      {
        type: "text",
        delta:
          "我把这次的讨论整理成了一篇帖子草稿，就在下面。改好以后点「发布」，帖子会发到社区请大家投票，我会先投一票。"
      }
    ]
  ];
  for (const [wait, event] of script) {
    await sleep(wait);
    if (signal?.aborted) {
      return;
    }
    yield event;
  }
  yield { type: "done" };
}
