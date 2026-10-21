import { describe, expect, it, vi } from "vitest";

import { chatEvents, type ChatTransport, type EntryOf, type ThreadMessageLike, toChatTurn } from "./api";
import { ChatStreamError } from "./mapper";

async function collect<T>(it: AsyncIterable<T>) {
  const out: T[] = [];
  for await (const x of it) {
    out.push(x);
  }
  return out;
}

// entries: the logged ids the page knows, by message id
function entryOf(entries: Record<string, { user?: string; leaf?: string }>): EntryOf {
  return { user: (m) => entries[m.id]?.user, leaf: (m) => entries[m.id]?.leaf };
}

const user = (id: string, text: string, images: string[] = []): ThreadMessageLike => ({
  id,
  role: "user",
  content: [{ type: "text", text }],
  attachments: images.map((image) => ({ content: [{ type: "image", image }] }))
});
const answer = (id: string): ThreadMessageLike => ({ id, role: "assistant", content: [{ type: "text", text: "…" }] });

describe("toChatTurn", () => {
  it("sends only the new message, after the answer before it", () => {
    // Arrange: one turn done, logged with leaf e_v1
    const thread = [user("m1", "值得买吗"), answer("m2"), user("m3", " 那 Pro 呢 ", ["http://site/uploads/a.jpg"])];

    // Act
    const got = toChatTurn("7", thread, entryOf({ m1: { user: "e_u1" }, m2: { leaf: "e_v1" } }));

    // Assert
    expect(got).toEqual({
      chat_id: "7",
      parent_id: "e_v1",
      message: { text: "那 Pro 呢", images: ["http://site/uploads/a.jpg"] }
    });
  });

  it("starts a new conversation with no parent", () => {
    const got = toChatTurn("7", [user("m1", "XM5 值不值")], entryOf({}));

    expect(got).toEqual({ chat_id: "7", parent_id: "", message: { text: "XM5 值不值", images: [] } });
  });

  it("regenerates a user message the server has logged: reruns it, sends nothing new", () => {
    // Arrange: assistant-ui drops the old answer and runs the same user message again
    const thread = [user("m1", "值得买吗")];

    // Act
    const got = toChatTurn("7", thread, entryOf({ m1: { user: "e_u1" } }));

    // Assert
    expect(got).toEqual({ chat_id: "7", parent_id: "e_u1" });
  });

  it("sends an edited message as new, after the answer the original followed", () => {
    // Arrange: m3 was edited into m4, a new message the server has not seen
    const thread = [user("m1", "a"), answer("m2"), user("m4", "改过的问题")];

    // Act
    const got = toChatTurn("7", thread, entryOf({ m1: { user: "e_u1" }, m2: { leaf: "e_v1" }, m3: { user: "e_u2" } }));

    // Assert
    expect(got.parent_id).toBe("e_v1");
    expect(got.message?.text).toBe("改过的问题");
  });

  it("follows a question left without an answer, keeping the conversation before it", () => {
    // Arrange: the server restarted mid-turn, so m3 was logged but never answered
    const thread = [user("m1", "a"), answer("m2"), user("m3", "那 bose 呢"), user("m4", "再看看淘宝")];

    // Act
    const got = toChatTurn("7", thread, entryOf({ m1: { user: "e_u1" }, m2: { leaf: "e_v1" }, m3: { user: "e_u2" } }));

    // Assert
    expect(got.parent_id).toBe("e_u2");
  });

  it("skips back past an answer that never got its ids", () => {
    // Arrange: m2's turn ended without a saved event
    const thread = [user("m1", "a"), answer("m2"), user("m3", "b")];

    // Act
    const got = toChatTurn("7", thread, entryOf({ m1: { user: "e_u1" } }));

    // Assert
    expect(got.parent_id).toBe("e_u1");
  });
});

const enc = new TextEncoder();

function transportWith(res: Response) {
  const onUnauthorized = vi.fn<() => void>();
  const fetch = vi.fn<typeof globalThis.fetch>(async () => res);
  const transport: ChatTransport = { baseUrl: "/answer/api/v1", getToken: () => "token-1", onUnauthorized, fetch };
  return { transport, onUnauthorized, fetch };
}

const noTurn = { chat_id: "7", parent_id: "" };

describe("chatEvents", () => {
  it("posts the turn with the token and yields the streamed events", async () => {
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

    const turn = { chat_id: "7", parent_id: "", message: { text: "hi", images: [] } };
    const got = await collect(chatEvents(transport, turn));

    expect(got).toEqual([{ type: "reasoning", delta: "想" }, { type: "done" }]);
    const [url, init] = fetch.mock.calls[0];
    expect(url).toBe("/answer/api/v1/purchase/ai/chat");
    expect(new Headers(init?.headers).get("Authorization")).toBe("token-1");
    expect(JSON.parse(String(init?.body))).toEqual(turn);
  });

  it("turns a JSON error the server sent before streaming into ChatStreamError with its message", async () => {
    const { transport, onUnauthorized } = transportWith(
      Response.json({ code: 400, reason: "base.request_format_error", msg: "请求格式错误" }, { status: 400 })
    );

    await expect(collect(chatEvents(transport, noTurn))).rejects.toEqual(new ChatStreamError("请求格式错误"));
    expect(onUnauthorized).not.toHaveBeenCalled();
  });

  it("reports a 401 so the app can drop the stale token", async () => {
    const { transport, onUnauthorized } = transportWith(Response.json({ code: 401, msg: "未登录" }, { status: 401 }));

    await expect(collect(chatEvents(transport, noTurn))).rejects.toBeInstanceOf(ChatStreamError);
    expect(onUnauthorized).toHaveBeenCalledOnce();
  });
});
