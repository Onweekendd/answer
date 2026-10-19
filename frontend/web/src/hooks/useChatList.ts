import { useQuery } from "@tanstack/react-query";

import { api } from "@/lib/session";

export const CHAT_LIST_KEY = ["purchase", "chats"] as const;

/** The viewer's recent conversations, fetched while the list is open. */
export function useChatList(enabled: boolean) {
  const query = useQuery({
    queryKey: CHAT_LIST_KEY,
    queryFn: ({ signal }) => api.chatList(signal),
    enabled,
    staleTime: 0
  });
  return { chats: query.data ?? [], isLoading: query.isPending && enabled, error: query.error };
}
