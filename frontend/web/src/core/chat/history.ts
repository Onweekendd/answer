import type { ThreadHistoryAdapter, ThreadMessage } from "@assistant-ui/react";

import type { Endpoints, PageMessage } from "../api/endpoints";
import { type ChatEvent, toEvent } from "./mapper";
import { replayEvents } from "./stream";

/** The endpoints the history needs; the tests pass a fake. */
export type ChatHistoryApi = Pick<Endpoints, "chatHistory" | "chatCreate">;

export interface ChatSessionOptions {
  /** The saved conversation to load, or null for a new one. */
  chatId: string | null;
  api: ChatHistoryApi;
  /** Called once when a new conversation gets its id, before its first message is sent. */
  onCreated?: (id: string) => void;
}

/** One conversation: its id once it has one, and the history adapter that loads it. */
export interface ChatSession {
  /** The conversation's id, creating it (titled `title`) the first time a new one needs it. */
  chatId(title: string): Promise<string>;
  history: ThreadHistoryAdapter;
}

/**
 * One conversation on the server. The server logs every turn itself, so the page saves nothing:
 * loading rebuilds the messages from the log, and a new conversation only needs creating before
 * its first message goes out. The runtime's append (the user's message) and the chat adapter both
 * ask for the id; they share one create instead of making two conversations.
 */
export function createChatSession({ chatId, api, onCreated }: ChatSessionOptions): ChatSession {
  let id: Promise<string> | null = chatId ? Promise.resolve(chatId) : null;

  const ensureId = (title: string) => {
    if (!id) {
      id = api.chatCreate(title).then((r) => {
        onCreated?.(r.id);
        return r.id;
      });
      // A failed create must not stick: the next message tries again.
      id.catch(() => {
        id = null;
      });
    }
    return id;
  };

  return {
    chatId: ensureId,
    history: {
      // The runtime reloads when this changes; it stays the same for the life of the adapter, even
      // after a new conversation gets its id, so the thread on screen is not thrown away.
      scopeId: chatId ?? "new",
      async load() {
        if (!chatId) {
          return { messages: [] };
        }
        const page = await api.chatHistory(chatId);
        return {
          messages: page.map((m) => ({ parentId: m.parent_id || null, message: toThreadMessage(m) }))
        };
      },
      async append({ message }) {
        if (message.role === "user") {
          await ensureId(messageText(message));
        }
      },
      async update() {}
    }
  };
}

/** The user's words in a message, for the conversation's title. */
export function messageText(message: ThreadMessage): string {
  return message.content
    .flatMap((p) => (p.type === "text" ? [p.text] : []))
    .join("\n")
    .trim();
}

/**
 * A logged message as the thread shows it. Its id is the logged entry and its metadata says so
 * (`entry` for the user's, `leaf` for an answer), the same as a live turn after its saved event,
 * so continuing or regenerating a loaded conversation sends the right ids.
 */
export function toThreadMessage(m: PageMessage): ThreadMessage {
  const createdAt = new Date(m.created_at * 1000);
  if (m.role === "user") {
    return {
      id: m.id,
      role: "user",
      createdAt,
      content: [{ type: "text", text: m.text ?? "" }],
      attachments: (m.images ?? []).map((url, i) => ({
        id: `${m.id}-${i}`,
        type: "image",
        name: url.split("/").at(-1) ?? "image",
        contentType: "image/jpeg",
        status: { type: "complete" },
        content: [{ type: "image", image: url }]
      })),
      metadata: { custom: { entry: m.id } }
    } as ThreadMessage;
  }
  const events = (m.events ?? []).map(toEvent).filter((e): e is ChatEvent => e !== null);
  const { content, error } = replayEvents(events);
  return {
    id: m.id,
    role: "assistant",
    createdAt,
    content,
    status: error ? { type: "incomplete", reason: "error", error } : { type: "complete", reason: "stop" },
    metadata: { unstable_state: null, unstable_annotations: [], unstable_data: [], steps: [], custom: { leaf: m.id } }
  } as ThreadMessage;
}
