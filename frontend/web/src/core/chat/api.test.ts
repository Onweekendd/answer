import { describe, expect, it, vi } from "vitest";

import { chatEvents, type ChatTransport, toChatMessages } from "./api";
import { ChatStreamError } from "./mapper";

async function collect<T>(it: AsyncIterable<T>) {
  const out: T[] = [];
  for await (const x of it) {
    out.push(x);
  }
  return out;
}

describe("toChatMessages", () => {
  it("keeps the text of each turn and the uploaded images of user turns", () => {
    const got = toChatMessages([
      {
        role: "user",
        content: [{ type: "text", text: " 值得买吗 " }],
        attachments: [{ content: [{ type: "image", image: "http://site/uploads/a.jpg" }] }]
      },
      {
        role: "assistant",
        content: [{ type: "reasoning", text: "想一想" }, { type: "tool-call" }, { type: "text", text: "**再等等**" }]
      },
      { role: "user", content: [{ type: "text", text: "那 Pro 呢" }] }
    ]);

    expect(got).toEqual([
      { role: "user", text: "值得买吗", images: ["http://site/uploads/a.jpg"] },
      { role: "assistant", text: "**再等等**", images: [] },
      { role: "user", text: "那 Pro 呢", images: [] }
    ]);
  });

  it("sends the cards after the answer, so a follow-up about the second one has something to point at", () => {
    const got = toChatMessages([
      {
        role: "assistant",
        content: [
          {
            type: "data",
            name: "products",
            data: [
              {
                id: 1,
                title: "羽绒被",
                shop: "无印良品旗舰店",
                price: "¥588",
                priceNote: "补贴后 ¥4??",
                url: "https://detail.tmall.com/item.htm?id=1"
              },
              { id: 2, title: "宜家被子", url: "https://www.ikea.cn/cn/zh/p/x" }
            ]
          },
          { type: "text", text: "**买** 选羽绒被" }
        ]
      }
    ]);

    expect(got).toEqual([
      {
        role: "assistant",
        text:
          "（回答下方给用户看的商品）\n" +
          "- 羽绒被 · 无印良品旗舰店 · ¥588（补贴后 ¥4??） · https://detail.tmall.com/item.htm?id=1\n" +
          "- 宜家被子 · https://www.ikea.cn/cn/zh/p/x\n" +
          "**买** 选羽绒被",
        images: []
      }
    ]);
  });

  it("drops an assistant turn with no text, such as one that failed", () => {
    const got = toChatMessages([
      { role: "user", content: [{ type: "text", text: "a" }] },
      { role: "assistant", content: [{ type: "tool-call" }] },
      { role: "user", content: [{ type: "text", text: "b" }] }
    ]);

    expect(got.map((m) => m.text)).toEqual(["a", "b"]);
  });

  it("sends at most the last 40 turns", () => {
    const many = Array.from({ length: 45 }, (_, i) => ({ role: "user", content: [{ type: "text", text: `${i}` }] }));

    const got = toChatMessages(many);

    expect(got).toHaveLength(40);
    expect(got.at(-1)?.text).toBe("44");
  });
});

const enc = new TextEncoder();

function transportWith(res: Response) {
  const onUnauthorized = vi.fn<() => void>();
  const fetch = vi.fn<typeof globalThis.fetch>(async () => res);
  const transport: ChatTransport = { baseUrl: "/answer/api/v1", getToken: () => "token-1", onUnauthorized, fetch };
  return { transport, onUnauthorized, fetch };
}

describe("chatEvents", () => {
  it("posts the conversation with the token and yields the streamed events", async () => {
    const body = new ReadableStream<Uint8Array>({
      start(c) {
        c.enqueue(
          enc.encode('data: {"type":"reasoning","delta":"想"}\n\n: ping\n\ndata: {"type":"done"}\n\ndata: [DONE]\n\n')
        );
        c.close();
      }
    });
    const { transport, fetch } = transportWith(
      new Response(body, { headers: { "Content-Type": "text/event-stream" } })
    );

    const got = await collect(chatEvents(transport, [{ role: "user", text: "hi", images: [] }]));

    expect(got).toEqual([{ type: "reasoning", delta: "想" }, { type: "done" }]);
    const [url, init] = fetch.mock.calls[0];
    expect(url).toBe("/answer/api/v1/purchase/ai/chat");
    expect(new Headers(init?.headers).get("Authorization")).toBe("token-1");
    expect(JSON.parse(String(init?.body))).toEqual({ messages: [{ role: "user", text: "hi", images: [] }] });
  });

  it("turns a JSON error the server sent before streaming into ChatStreamError with its message", async () => {
    const { transport, onUnauthorized } = transportWith(
      Response.json({ code: 400, reason: "base.request_format_error", msg: "请求格式错误" }, { status: 400 })
    );

    await expect(collect(chatEvents(transport, []))).rejects.toEqual(new ChatStreamError("请求格式错误"));
    expect(onUnauthorized).not.toHaveBeenCalled();
  });

  it("reports a 401 so the app can drop the stale token", async () => {
    const { transport, onUnauthorized } = transportWith(Response.json({ code: 401, msg: "未登录" }, { status: 401 }));

    await expect(collect(chatEvents(transport, []))).rejects.toBeInstanceOf(ChatStreamError);
    expect(onUnauthorized).toHaveBeenCalledOnce();
  });
});
