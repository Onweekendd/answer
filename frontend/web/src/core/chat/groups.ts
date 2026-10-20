import type { ChatItem } from "../api/endpoints";

export interface ChatGroup {
  label: string;
  chats: ChatItem[];
}

const DAY = 24 * 60 * 60 * 1000;

/** Midnight of the day `now` falls on, in the viewer's time zone. */
function startOfDay(now: Date): number {
  return new Date(now.getFullYear(), now.getMonth(), now.getDate()).getTime();
}

/**
 * The chats grouped by when they were last used, newest first, as the drawer lists them:
 * 今天, 昨天, 7 天内, 30 天内, 更早. Empty groups are left out.
 */
export function groupChats(chats: readonly ChatItem[], now: Date): ChatGroup[] {
  const today = startOfDay(now);
  const bounds: [string, number][] = [
    ["今天", today],
    ["昨天", today - DAY],
    ["7 天内", today - 6 * DAY],
    ["30 天内", today - 29 * DAY],
    ["更早", -Infinity]
  ];
  const groups = bounds.map(([label]) => ({ label, chats: [] as ChatItem[] }));
  const sorted = [...chats].sort((a, b) => b.updated_at - a.updated_at);
  for (const chat of sorted) {
    const at = chat.updated_at * 1000;
    groups[bounds.findIndex(([, from]) => at >= from)].chats.push(chat);
  }
  return groups.filter((g) => g.chats.length > 0);
}

/** The chats whose title holds every word of the query, ignoring case. */
export function searchChats(chats: readonly ChatItem[], query: string): ChatItem[] {
  const words = query.trim().toLowerCase().split(/\s+/).filter(Boolean);
  if (words.length === 0) {
    return [...chats];
  }
  return chats.filter((c) => {
    const title = c.title.toLowerCase();
    return words.every((w) => title.includes(w));
  });
}
