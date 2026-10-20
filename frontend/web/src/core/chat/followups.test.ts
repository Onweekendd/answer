import { describe, expect, it } from "vitest";

import { followupsOf } from "./followups";
import { applyEvent, type ContentPart, parseEvent } from "./mapper";

describe("followups", () => {
  it("parses at most three non-empty suggestions, and drops an empty event", () => {
    expect(parseEvent(JSON.stringify({ type: "followups", followups: ["a", "", 3, "b", "c", "d"] }))).toEqual({
      type: "followups",
      followups: ["a", "b", "c"]
    });
    expect(parseEvent(JSON.stringify({ type: "followups", followups: [] }))).toBeNull();
  });

  it("keeps them in a part of their own that does not disturb the cards", () => {
    // Arrange
    let parts: ContentPart[] = [];

    // Act
    parts = applyEvent(parts, { type: "text", delta: "价格偏高" });
    parts = applyEvent(parts, { type: "followups", followups: ["帮我发到社区，让大家投票"] });
    parts = applyEvent(parts, { type: "product", product: { id: 1, title: "t", url: "https://item.jd.com/1.html" } });

    // Assert
    expect(followupsOf([{ role: "assistant", content: parts }])).toEqual(["帮我发到社区，让大家投票"]);
    expect(parts.filter((p) => p.type === "data")).toHaveLength(2);
  });

  it("offers nothing when the last message is the user's", () => {
    expect(followupsOf([{ role: "user", content: [{ type: "text" }] }])).toEqual([]);
  });
});
