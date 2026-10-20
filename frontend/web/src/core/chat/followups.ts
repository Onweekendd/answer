import type { SuggestionAdapter } from "@assistant-ui/react";

import { FOLLOWUPS_PART } from "./mapper";

/** The slice of a thread message the suggestions are read from. */
interface MessageLike {
  role: string;
  content: readonly { type: string; name?: string; data?: unknown }[];
}

/** The follow-ups the last answer came with; none when the last message is not an answer. */
export function followupsOf(messages: readonly MessageLike[]): string[] {
  const last = messages.at(-1);
  if (last?.role !== "assistant") {
    return [];
  }
  const part = last.content.find((p) => p.type === "data" && p.name === FOLLOWUPS_PART);
  return Array.isArray(part?.data) ? part.data.filter((f): f is string => typeof f === "string") : [];
}

/**
 * Shows the follow-ups the server sent with the answer as the thread's suggestions. No extra
 * request: the agent wrote them along with the answer. Tapping one sends it as the user's message.
 */
export const followupSuggestions: SuggestionAdapter = {
  generate: async ({ messages }) => followupsOf(messages).map((prompt) => ({ prompt }))
};
