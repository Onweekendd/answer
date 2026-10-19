import { type ChatEvent, ChatStreamError } from "./mapper";
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
  content: readonly { type: string; text?: string }[];
  attachments?: readonly { content?: readonly { type: string; image?: string }[] }[];
}

/** The server takes at most 40; older turns matter least, so they go first. */
const MAX_MESSAGES = 40;

/**
 * The conversation as the server wants it: user and assistant turns with their text, user turns
 * with their uploaded images. Thinking and tool calls stay behind, and an assistant turn with no
 * text (a failed one) is dropped, since it tells the model nothing.
 */
export function toChatMessages(messages: readonly ThreadMessageLike[]): ChatMessageReq[] {
  const out: ChatMessageReq[] = [];
  for (const m of messages) {
    if (m.role !== "user" && m.role !== "assistant") {
      continue;
    }
    const text = m.content
      .flatMap((p) => (p.type === "text" && typeof p.text === "string" ? [p.text] : []))
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
