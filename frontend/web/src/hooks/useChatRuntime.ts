import { type ChatModelAdapter, type ChatModelRunResult, useLocalRuntime } from "@assistant-ui/react";
import { useEffect, useRef, useState } from "react";

import { chatEvents, toChatMessages } from "@/core/chat/api";
import { followupSuggestions } from "@/core/chat/followups";
import { createChatHistory } from "@/core/chat/history";
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

/**
 * The chat adapter: posts the conversation to the Go chat endpoint and folds the streamed events
 * into the message. The mock feeds the same folding, so what the UI shows is the same code path.
 */
const adapter: ChatModelAdapter = {
  async *run({ messages, abortSignal }) {
    const events = useMock
      ? mockReply(lastUserText(messages), { signal: abortSignal })
      : chatEvents(transport, toChatMessages(messages), abortSignal);
    // The cast is the one place our plain types meet the library's JSON types: every value in
    // a snapshot came from JSON.parse (or the mock's literals), so it is JSON by construction.
    for await (const snapshot of foldEvents(events, abortSignal)) {
      yield snapshot as ChatModelRunResult;
    }
  }
};

const attachments = new UploadImageAdapter(api.uploadImage);

/**
 * The runtime for one conversation: chatId is the saved one to load, or null for a new one,
 * which is saved from its first message on and reported through onCreated. Mount it again (a
 * new key) to switch conversations. The mock has no server to save to, so it keeps nothing.
 */
export function useChatRuntime(chatId: string | null, onCreated: (id: string) => void) {
  const onCreatedRef = useRef(onCreated);
  useEffect(() => {
    onCreatedRef.current = onCreated;
  });
  const [history] = useState(() =>
    useMock ? undefined : createChatHistory({ chatId, api, onCreated: (id) => onCreatedRef.current(id) })
  );
  return useLocalRuntime(adapter, { adapters: { attachments, history, suggestion: followupSuggestions } });
}
