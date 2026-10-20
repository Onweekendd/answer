import { describe, expect, it } from "vitest";

import type { ChatItem } from "../api/endpoints";
import { groupChats, searchChats } from "./groups";

// 2026-10-20 15:00 local time
const now = new Date(2026, 9, 20, 15, 0, 0);
const at = (y: number, m: number, d: number, h = 12) => new Date(y, m - 1, d, h).getTime() / 1000;
const chat = (id: string, title: string, updated: number): ChatItem => ({ id, title, updated_at: updated });

describe("groupChats", () => {
  it("puts each chat under 今天, 昨天, 7 天内, 30 天内 or 更早, newest first", () => {
    // Arrange
    const chats = [
      chat("old", "宜家沙发", at(2026, 8, 1)),
      chat("today2", "iPad", at(2026, 10, 20, 9)),
      chat("yesterday", "显示器", at(2026, 10, 19, 23)),
      chat("today1", "耳机", at(2026, 10, 20, 0)),
      chat("week", "被子", at(2026, 10, 14)),
      chat("month", "椅子", at(2026, 10, 1))
    ];

    // Act
    const got = groupChats(chats, now).map((g) => [g.label, g.chats.map((c) => c.id)]);

    // Assert
    expect(got).toEqual([
      ["今天", ["today2", "today1"]],
      ["昨天", ["yesterday"]],
      ["7 天内", ["week"]],
      ["30 天内", ["month"]],
      ["更早", ["old"]]
    ]);
  });

  it("leaves out empty groups", () => {
    // Act
    const got = groupChats([chat("a", "x", at(2026, 10, 20))], now);

    // Assert
    expect(got.map((g) => g.label)).toEqual(["今天"]);
  });
});

describe("searchChats", () => {
  const chats = [chat("1", "iPad Air M3 值不值", 1), chat("2", "宜家 沙发", 2)];

  it("keeps the chats whose title has every word, ignoring case", () => {
    expect(searchChats(chats, "ipad m3").map((c) => c.id)).toEqual(["1"]);
  });

  it("keeps everything for an empty query", () => {
    expect(searchChats(chats, "  ")).toHaveLength(2);
  });
});
