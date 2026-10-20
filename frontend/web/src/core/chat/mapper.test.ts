import { describe, expect, it } from "vitest";

import { applyEvent, type ChatEvent, ChatStreamError, type ContentPart, parseEvent } from "./mapper";

const run = (events: ChatEvent[], start: ContentPart[] = []) =>
  events.reduce((parts, e) => applyEvent(parts, e), start);

describe("applyEvent", () => {
  it("starts a text part on the first delta", () => {
    expect(run([{ type: "text", delta: "Hi" }])).toEqual([{ type: "text", text: "Hi" }]);
  });

  it("appends consecutive deltas to the same text part", () => {
    expect(
      run([
        { type: "text", delta: "Hel" },
        { type: "text", delta: "lo" }
      ])
    ).toEqual([{ type: "text", text: "Hello" }]);
  });

  it("adds a running tool call with parsed args and args text", () => {
    const parts = run([{ type: "tool_start", id: "t1", name: "web_search", args: { q: "iphone" } }]);

    expect(parts).toEqual([
      {
        type: "tool-call",
        toolCallId: "t1",
        toolName: "web_search",
        args: { q: "iphone" },
        argsText: '{"q":"iphone"}'
      }
    ]);
  });

  it("attaches the result to the matching tool call", () => {
    const parts = run([
      { type: "tool_start", id: "t1", name: "web_search", args: {} },
      { type: "tool_start", id: "t2", name: "fetch_page", args: {} },
      { type: "tool_end", id: "t1", result: { hits: 3 } }
    ]);

    expect(parts[0]).toMatchObject({ toolCallId: "t1", result: { hits: 3 } });
    expect(parts[1]).not.toHaveProperty("result");
  });

  it("starts a new text part after a tool call instead of editing the old one", () => {
    const parts = run([
      { type: "text", delta: "Let me check." },
      { type: "tool_start", id: "t1", name: "web_search", args: {} },
      { type: "tool_end", id: "t1", result: "ok" },
      { type: "text", delta: "Found it." }
    ]);

    expect(parts.map((p) => p.type)).toEqual(["text", "tool-call", "text"]);
    expect(parts[2]).toEqual({ type: "text", text: "Found it." });
  });

  it("ignores a tool_end for an unknown id", () => {
    const before = run([{ type: "text", delta: "a" }]);

    expect(applyEvent(before, { type: "tool_end", id: "ghost", result: 1 })).toEqual(before);
  });

  it("does not mutate the previous parts", () => {
    const before: ContentPart[] = [{ type: "text", text: "a" }];

    applyEvent(before, { type: "text", delta: "b" });

    expect(before).toEqual([{ type: "text", text: "a" }]);
  });

  it("throws ChatStreamError for an error event", () => {
    expect(() => applyEvent([], { type: "error", message: "quota exceeded" })).toThrow(ChatStreamError);
    expect(() => applyEvent([], { type: "error", message: "quota exceeded" })).toThrow("quota exceeded");
  });

  it("leaves parts unchanged on done", () => {
    const before = run([{ type: "text", delta: "a" }]);

    expect(applyEvent(before, { type: "done" })).toEqual(before);
  });

  it("keeps each step's thinking in its own part, between the tool calls", () => {
    const parts = run([
      { type: "reasoning", delta: "先打开" },
      { type: "reasoning", delta: "京东" },
      { type: "tool_start", id: "0-0", name: "open", args: { summary: "打开 www.jd.com" } },
      { type: "reasoning", delta: "看看页面" },
      { type: "text", delta: "**买**" }
    ]);

    expect(parts.map((p) => p.type)).toEqual(["reasoning", "tool-call", "reasoning", "text"]);
    expect(parts[0]).toEqual({ type: "reasoning", text: "先打开京东" });
  });
});

describe("parseEvent", () => {
  it("parses a known event from SSE data", () => {
    expect(parseEvent('{"type":"text","delta":"x"}')).toEqual({ type: "text", delta: "x" });
    expect(parseEvent('{"type":"reasoning","delta":"想"}')).toEqual({ type: "reasoning", delta: "想" });
  });

  it("turns tool args that are not an object into {} because the UI needs an object", () => {
    for (const args of ['"q"', "3", "null", "[1]"]) {
      expect(parseEvent(`{"type":"tool_start","id":"t","name":"n","args":${args}}`)).toMatchObject({ args: {} });
    }
    expect(parseEvent('{"type":"tool_start","id":"t","name":"n"}')).toMatchObject({ args: {} });
    expect(parseEvent('{"type":"tool_start","id":"t","name":"n","args":{"q":1}}')).toMatchObject({ args: { q: 1 } });
  });

  it("returns null for malformed JSON or an unknown type so the stream survives", () => {
    expect(parseEvent("not json")).toBeNull();
    expect(parseEvent('{"type":"mystery"}')).toBeNull();
    expect(parseEvent('{"type":"text"}')).toBeNull();
  });
});

describe("product cards", () => {
  const card = (id: number, title = `商品 ${id}`) => ({ id, title, url: `https://item.jd.com/${id}.html` });
  const step = (id: string): ChatEvent => ({ type: "tool_start", id, name: "click", args: { summary: id } });

  it("keeps every card in one products part, below the answer and above the follow-ups", () => {
    // Arrange: the cards come mid-run, the answer and the follow-ups after them
    const events: ChatEvent[] = [
      step("s1"),
      { type: "product", product: card(1) },
      step("s2"),
      { type: "product", product: card(2) },
      { type: "text", delta: "见下面的商品" },
      { type: "followups", followups: ["还有别的吗"] }
    ];

    // Act
    const parts = run(events);

    // Assert: the two steps stay together, so the process box does not break in two
    expect(parts.map((p) => (p.type === "tool-call" ? p.toolCallId : p.type === "data" ? p.name : p.type))).toEqual([
      "s1",
      "s2",
      "text",
      "products",
      "followups"
    ]);
    expect(parts[3]).toEqual({ type: "data", name: "products", data: [card(1), card(2)] });
  });

  it("replaces a card sent again with the same id", () => {
    // Act
    const parts = run([
      { type: "product", product: card(1, "旧") },
      { type: "product", product: card(2) },
      { type: "product", product: card(1, "新") }
    ]);

    // Assert
    expect(parts).toEqual([{ type: "data", name: "products", data: [card(1, "新"), card(2)] }]);
  });

  it("parses a product event", () => {
    expect(parseEvent(JSON.stringify({ type: "product", product: card(1) }))).toEqual({
      type: "product",
      product: card(1)
    });
  });

  it("drops a card whose link or image is not http(s), since it goes into an href or src", () => {
    // Act & Assert
    expect(
      parseEvent(JSON.stringify({ type: "product", product: { ...card(1), url: "javascript:alert(1)" } }))
    ).toBeNull();
    expect(
      parseEvent(JSON.stringify({ type: "product", product: { ...card(1), image: "data:text/html,x" } }))
    ).toBeNull();
    expect(parseEvent(JSON.stringify({ type: "product", product: { id: 1, url: "https://a.com" } }))).toBeNull();
  });
});
