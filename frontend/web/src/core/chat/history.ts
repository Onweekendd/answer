import type { ExportedMessageRepositoryItem, ThreadHistoryAdapter, ThreadMessage } from "@assistant-ui/react";

import type { Endpoints } from "../api/endpoints";

/** The endpoints the history needs; the tests pass a fake. */
export type ChatHistoryApi = Pick<Endpoints, "chatGet" | "chatCreate" | "chatSaveMessage">;

export interface ChatHistoryOptions {
  /** The saved conversation to load, or null for a new one. */
  chatId: string | null;
  api: ChatHistoryApi;
  /** Called once when a new conversation gets its id, on its first saved message. */
  onCreated?: (id: string) => void;
}

/**
 * Keeps one conversation on the server. The runtime calls append when a message settles (the
 * user's on send, the answer once it stops running) and update when one changes later; both
 * save the whole message under its id. A new conversation is created on the first save, titled
 * with what the user asked.
 */
export function createChatHistory({ chatId, api, onCreated }: ChatHistoryOptions): ThreadHistoryAdapter {
  // Two saves can be in flight at once; they share one create instead of making two conversations.
  let id: Promise<string> | null = chatId ? Promise.resolve(chatId) : null;

  const ensureId = (first: ThreadMessage) => {
    if (!id) {
      id = api.chatCreate(messageText(first)).then((r) => {
        onCreated?.(r.id);
        return r.id;
      });
      // A failed create must not stick: the next save tries again.
      id.catch(() => {
        id = null;
      });
    }
    return id;
  };

  const save = async ({ message, parentId }: ExportedMessageRepositoryItem) => {
    const chat = await ensureId(message);
    await api.chatSaveMessage(chat, message.id, parentId, toStored(message));
  };

  return {
    // The runtime reloads when this changes; it stays the same for the life of the adapter, even
    // after a new conversation gets its id, so the thread on screen is not thrown away.
    scopeId: chatId ?? "new",
    async load() {
      if (!chatId) {
        return { messages: [] };
      }
      const chat = await api.chatGet(chatId);
      return {
        messages: chat.messages.map((m) => ({
          parentId: m.parent_id || null,
          message: fromStored(m.message)
        }))
      };
    },
    append: save,
    update: save
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
 * The message as JSON can carry it. An attachment's File is a browser object that does not
 * survive JSON (it becomes {}), and its uploaded url is already in the attachment's content.
 */
export function toStored(message: ThreadMessage): unknown {
  return {
    ...message,
    attachments: message.attachments?.map(({ file: _file, ...rest }) => rest)
  };
}

/** Back from JSON: createdAt was a Date and comes back as a string. */
export function fromStored(stored: unknown): ThreadMessage {
  const m = stored as ThreadMessage & { createdAt: string | Date };
  return { ...m, createdAt: new Date(m.createdAt) } as ThreadMessage;
}
