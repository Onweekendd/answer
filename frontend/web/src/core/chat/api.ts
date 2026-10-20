import { type ChatEvent, ChatStreamError } from "./mapper";
import { eventsFromSSE } from "./stream";

/**
 * One turn as POST /purchase/ai/chat takes it. The server keeps the conversation, so the page
 * sends only the new message and the logged entry it follows; without a message the turn
 * regenerates the answer to the user message `parent_id` names. Images are urls from POST /file.
 */
export interface ChatTurnReq {
  chat_id: string;
  parent_id: string;
  message?: { text: string; images: string[] };
}

/** The slice of an assistant-ui thread message we read; ThreadMessage satisfies it structurally. */
export interface ThreadMessageLike {
  id: string;
  role: string;
  content: readonly { type: string; text?: string }[];
  attachments?: readonly { content?: readonly { type: string; image?: string }[] }[];
  /** `custom.entry` / `custom.leaf` hold the logged ids, see EntryOf. */
  metadata?: unknown;
}

/**
 * The logged ids the page knows for a message: a user message's own entry once the server has
 * logged it, an answer's leaf (what the next message follows). Unknown ones are undefined.
 */
export interface EntryOf {
  user(message: ThreadMessageLike): string | undefined;
  leaf(message: ThreadMessageLike): string | undefined;
}

/**
 * The turn to send for the thread assistant-ui hands the adapter, which always ends with a user
 * message. One the server has logged already is being regenerated: rerun it, send nothing new.
 * Otherwise it is new (sent, or edited into a new branch): it follows the answer before it, or
 * starts the conversation.
 */
export function toChatTurn(chatId: string, messages: readonly ThreadMessageLike[], entryOf: EntryOf): ChatTurnReq {
  const last = messages.at(-1);
  if (!last) {
    throw new Error("toChatTurn: no message to send");
  }
  const logged = entryOf.user(last);
  if (logged) {
    return { chat_id: chatId, parent_id: logged };
  }
  const before = messages.at(-2);
  return {
    chat_id: chatId,
    parent_id: (before && entryOf.leaf(before)) ?? "",
    message: { text: textOf(last), images: imagesOf(last) }
  };
}

function textOf(m: ThreadMessageLike): string {
  return m.content
    .flatMap((p) => (p.type === "text" && typeof p.text === "string" ? [p.text] : []))
    .join("\n")
    .trim();
}

function imagesOf(m: ThreadMessageLike): string[] {
  return (m.attachments ?? []).flatMap((a) =>
    (a.content ?? []).flatMap((p) => (p.type === "image" && typeof p.image === "string" ? [p.image] : []))
  );
}

export interface ChatTransport {
  baseUrl: string;
  getToken: () => string | null;
  onUnauthorized: () => void;
  fetch?: typeof fetch;
}

/**
 * Posts one turn and yields the events the server streams back. A request the server
 * turns down before streaming (bad input, not logged in) answers with the usual JSON envelope
 * instead, which becomes a ChatStreamError carrying the server's message.
 */
export async function* chatEvents(
  transport: ChatTransport,
  turn: ChatTurnReq,
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
      body: JSON.stringify(turn),
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
