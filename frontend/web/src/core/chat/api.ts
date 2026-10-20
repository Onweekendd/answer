import { isDraftResult, PUBLISH_TOOL } from "./draft";
import { type ChatEvent, ChatStreamError, type ProductCard, PRODUCTS_PART } from "./mapper";
import { eventsFromSSE } from "./stream";

/** One message as POST /purchase/ai/chat takes it; images are urls from POST /file. */
export interface ChatMessageReq {
  role: "user" | "assistant";
  text: string;
  images: string[];
}

/** The slice of an assistant-ui thread message we read; ThreadMessage satisfies it structurally. */
export interface ThreadMessageLike {
  role: string;
  content: readonly {
    type: string;
    text?: string;
    name?: string;
    data?: unknown;
    toolName?: string;
    result?: unknown;
  }[];
  attachments?: readonly { content?: readonly { type: string; image?: string }[] }[];
}

/** The server takes at most 40; older turns matter least, so they go first. */
const MAX_MESSAGES = 40;

/**
 * The conversation as the server wants it: user and assistant turns with their text, user turns
 * with their uploaded images. Thinking and tool calls stay behind, and an assistant turn with no
 * text (a failed one) is dropped, since it tells the model nothing. The cards go along as text
 * after the answer, so a follow-up such as "第二个呢" has something to point at.
 */
export function toChatMessages(messages: readonly ThreadMessageLike[]): ChatMessageReq[] {
  const out: ChatMessageReq[] = [];
  for (const m of messages) {
    if (m.role !== "user" && m.role !== "assistant") {
      continue;
    }
    const text = m.content
      .flatMap((p) => {
        if (p.type === "text" && typeof p.text === "string") {
          return [p.text];
        }
        if (p.type === "data" && p.name === PRODUCTS_PART && Array.isArray(p.data)) {
          return [cardsText(p.data as ProductCard[])];
        }
        // what became of a draft, so "改一下标题" after it has something to go on
        if (p.type === "tool-call" && p.toolName === PUBLISH_TOOL && isDraftResult(p.result)) {
          return [
            p.result.status === "published"
              ? `（用户已经把帖子「${p.result.title}」发到了社区）`
              : "（用户放弃了这份帖子草稿）"
          ];
        }
        return [];
      })
      .join("\n")
      .trim();
    const images = (m.attachments ?? []).flatMap((a) =>
      (a.content ?? []).flatMap((p) => (p.type === "image" && typeof p.image === "string" ? [p.image] : []))
    );
    if (m.role === "assistant" && !text) {
      continue;
    }
    out.push({ role: m.role, text, images: m.role === "user" ? images : [] });
  }
  return out.slice(-MAX_MESSAGES);
}

/** The cards as the model saw them when it made them: one line each, with the link. */
export function cardsText(cards: readonly ProductCard[]): string {
  const lines = cards.map((c) =>
    [`- ${c.title}`, c.shop, c.spec, c.price && `${c.price}${c.priceNote ? `（${c.priceNote}）` : ""}`, c.url]
      .filter(Boolean)
      .join(" · ")
  );
  return lines.length > 0 ? `（回答下方给用户看的商品）\n${lines.join("\n")}` : "";
}

export interface ChatTransport {
  baseUrl: string;
  getToken: () => string | null;
  onUnauthorized: () => void;
  fetch?: typeof fetch;
}

/**
 * Posts the conversation and yields the events the server streams back. A request the server
 * turns down before streaming (bad input, not logged in) answers with the usual JSON envelope
 * instead, which becomes a ChatStreamError carrying the server's message.
 */
export async function* chatEvents(
  transport: ChatTransport,
  messages: ChatMessageReq[],
  signal?: AbortSignal
): AsyncGenerator<ChatEvent> {
  const headers = new Headers({ "Content-Type": "application/json" });
  const token = transport.getToken();
  if (token) {
    headers.set("Authorization", token);
  }
  const doFetch = transport.fetch ?? ((...args) => fetch(...args));

  let res: Response;
  try {
    res = await doFetch(`${transport.baseUrl}/purchase/ai/chat`, {
      method: "POST",
      headers,
      body: JSON.stringify({ messages }),
      signal
    });
  } catch (e) {
    if (e instanceof DOMException && e.name === "AbortError") {
      throw e;
    }
    throw new ChatStreamError("网络连接失败，请稍后重试");
  }

  if (!(res.headers.get("Content-Type") ?? "").startsWith("text/event-stream") || !res.body) {
    const envelope = (await res.json().catch(() => null)) as { code?: number; msg?: string } | null;
    if (res.status === 401 || envelope?.code === 401) {
      transport.onUnauthorized();
    }
    throw new ChatStreamError(envelope?.msg || "出错了，请稍后再试");
  }
  yield* eventsFromSSE(res.body);
}
