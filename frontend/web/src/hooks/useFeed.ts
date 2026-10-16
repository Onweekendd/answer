import { useQuery } from "@tanstack/react-query";

import { dedupeById } from "@/core/feed/batch";
import { api } from "@/lib/session";

export const FEED_KEY = ["community", "feed"] as const;

/**
 * One random batch. It never refetches by itself (no refocus, no staleness): the batch only
 * changes when the viewer asks for the next one, otherwise the cards they are reading would
 * be swapped out under them.
 */
export function useFeed() {
  const query = useQuery({
    queryKey: FEED_KEY,
    queryFn: ({ signal }) => api.communityFeed(signal),
    select: dedupeById,
    staleTime: Infinity,
    gcTime: Infinity,
    refetchOnWindowFocus: false,
    refetchOnReconnect: false
  });

  return {
    items: query.data ?? [],
    isLoading: query.isPending,
    isRefreshing: query.isFetching && !query.isPending,
    error: query.error,
    nextBatch: () => query.refetch()
  };
}
