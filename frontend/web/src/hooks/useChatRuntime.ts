import { type ChatModelAdapter, type ChatModelRunResult, useLocalRuntime } from "@assistant-ui/react";
import { useEffect, useRef, useState } from "react";

import { chatEvents, type EntryOf, toChatTurn } from "@/core/chat/api";
import { type ChatSession, createChatSession, messageText } from "@/core/chat/history";
import { mockReply } from "@/core/chat/mock";
import { lastUserText } from "@/core/chat/prompt";
import { foldEvents } from "@/core/chat/stream";
import { UploadImageAdapter } from "@/core/chat/upload";
import { api, tokenStore } from "@/lib/session";

/** VITE_CHAT_MOCK=true plays the scripted reply instead, for working on the UI without a backend. */
const useMock = import.meta.env.VITE_CHAT_MOCK === "true";

const transport = {
  baseUrl: "/answer/api/v1",
  getToken: tokenStore.get,
  onUnauthorized: tokenStore.clear
};

const custom = (m: { metadata?: unknown }, key: string): string | undefined => {
  const value = (m.metadata as { custom?: Record<string, unknown> } | undefined)?.custom?.[key];
  return typeof value === "string" && value !== "" ? value : undefined;
};

/**
 * The chat adapter for one conversation: sends the new message and what it follows, and folds the
 * streamed events into the message. The server keeps the conversation, so the request carries only
 * that. The mock feeds the same folding, so what the UI shows is the same code path.
 */
function chatAdapter(session: ChatSession | undefined): ChatModelAdapter {
  // A message sent in this tab learns its logged id from the saved event; a loaded one carries it
  // in its metadata. An answer keeps its leaf in its own metadata either way.
  const userEntries = new Map<string, string>();
  const entryOf: EntryOf = {
    user: (m) => custom(m, "entry") ?? userEntries.get(m.id),
    leaf: (m) => custom(m, "leaf")
  };

  return {
    async *run({ messages, abortSignal }) {
      const last = messages.at(-1);
      const events =
        useMock || !session || !last
          ? mockReply(lastUserText(messages), { signal: abortSignal })
          : chatEvents(transport, toChatTurn(await session.chatId(messageText(last)), messages, entryOf), abortSignal);
      // The cast is the one place our plain types meet the library's JSON types: every value in
      // a snapshot came from JSON.parse (or the mock's literals), so it is JSON by construction.
      for await (const snapshot of foldEvents(events, abortSignal)) {
        const user = snapshot.metadata?.custom.user;
        if (last && user) {
          userEntries.set(last.id, user);
        }
        yield snapshot as ChatModelRunResult;
      }
    }
  };
}

const attachments = new UploadImageAdapter(api.uploadImage);

/**
 * The runtime for one conversation: chatId is the saved one to load, or null for a new one,
 * which is created before its first message goes out and reported through onCreated. Mount it
 * again (a new key) to switch conversations. The mock has no server, so it keeps nothing.
 */
export function useChatRuntime(chatId: string | null, onCreated: (id: string) => void) {
  const onCreatedRef = useRef(onCreated);
  useEffect(() => {
    onCreatedRef.current = onCreated;
  });
  const [session] = useState(() =>
    useMock ? undefined : createChatSession({ chatId, api, onCreated: (id) => onCreatedRef.current(id) })
  );
  const [adapter] = useState(() => chatAdapter(session));
  return useLocalRuntime(adapter, { adapters: { attachments, history: session?.history } });
}
