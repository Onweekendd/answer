import { describe, expect, it, vi } from "vitest";

import type { ThreadMessage } from "@assistant-ui/react";

import type { PageMessage } from "../api/endpoints";
import { type ChatHistoryApi, createChatSession, toThreadMessage } from "./history";

function userMessage(id: string, text: string): ThreadMessage {
  return {
    id,
    role: "user",
    createdAt: new Date("2026-10-19T08:00:00Z"),
    content: [{ type: "text", text }],
    attachments: [],
    metadata: { custom: {} }
  } as ThreadMessage;
}

function fakeApi(page: PageMessage[] = []): ChatHistoryApi {
  return {
    chatHistory: vi.fn(async () => page),
    chatCreate: vi.fn(async () => ({ id: "7" }))
  };
}

describe("createChatSession", () => {
  it("creates the conversation once, titled with the question, shared by the history and the chat", async () => {
    // Arrange
    const api = fakeApi();
    const onCreated = vi.fn();
    const session = createChatSession({ chatId: null, api, onCreated });

    // Act: the runtime appends the user's message while the adapter asks for the id to send it
    const [, id] = await Promise.all([
      session.history.append({ parentId: null, message: userMessage("u1", " 这个显示器值不值 ") }),
      session.chatId("这个显示器值不值")
    ]);

    // Assert
    expect(id).toBe("7");
    expect(api.chatCreate).toHaveBeenCalledTimes(1);
    expect(api.chatCreate).toHaveBeenCalledWith("这个显示器值不值");
    expect(onCreated).toHaveBeenCalledWith("7");
  });

  it("saves nothing itself: the server logs the turns", async () => {
    // Arrange
    const api = fakeApi();
    const session = createChatSession({ chatId: "7", api });

    // Act
    await session.history.append({
      parentId: "u1",
      message: { ...userMessage("a1", ""), role: "assistant" } as ThreadMessage
    });
    await session.history.update?.({ parentId: "u1", message: userMessage("a1", "") });

    // Assert
    expect(api.chatCreate).not.toHaveBeenCalled();
  });

  it("tries to create again after a failed create", async () => {
    // Arrange
    const api = fakeApi();
    vi.mocked(api.chatCreate).mockRejectedValueOnce(new Error("offline"));
    const session = createChatSession({ chatId: null, api });

    // Act
    await expect(session.chatId("hi")).rejects.toThrow("offline");
    const id = await session.chatId("hi");

    // Assert
    expect(id).toBe("7");
    expect(api.chatCreate).toHaveBeenCalledTimes(2);
  });

  it("loads a conversation from its log, every branch with its parent", async () => {
    // Arrange: a question, its answer, and a regenerated answer next to it
    const api = fakeApi([
      { id: "e_u1", parent_id: "", role: "user", text: "hi", created_at: 1_760_860_800 },
      {
        id: "e_v1",
        parent_id: "e_u1",
        role: "assistant",
        events: [{ type: "text", delta: "你好" }],
        created_at: 1_760_860_810
      },
      {
        id: "e_v2",
        parent_id: "e_u1",
        role: "assistant",
        events: [{ type: "text", delta: "嗨" }],
        created_at: 1_760_860_820
      }
    ]);
    const session = createChatSession({ chatId: "7", api });

    // Act
    const repo = await session.history.load();

    // Assert
    expect(session.history.scopeId).toBe("7");
    expect(api.chatHistory).toHaveBeenCalledWith("7");
    expect(repo.messages.map((m) => [m.message.id, m.parentId])).toEqual([
      ["e_u1", null],
      ["e_v1", "e_u1"],
      ["e_v2", "e_u1"]
    ]);
  });

  it("loads nothing for a new conversation", async () => {
    // Arrange
    const api = fakeApi();
    const session = createChatSession({ chatId: null, api });

    // Act
    const repo = await session.history.load();

    // Assert
    expect(repo.messages).toEqual([]);
    expect(api.chatHistory).not.toHaveBeenCalled();
  });
});

describe("toThreadMessage", () => {
  it("gives a user message its text, its images and its logged id", () => {
    // Act
    const m = toThreadMessage({
      id: "e_u1",
      parent_id: "",
      role: "user",
      text: "值不值",
      images: ["http://site/uploads/post/a.jpg"],
      created_at: 1_760_860_800
    });

    // Assert
    expect(m.content).toEqual([{ type: "text", text: "值不值" }]);
    expect(m.attachments?.[0]?.content).toEqual([{ type: "image", image: "http://site/uploads/post/a.jpg" }]);
    expect(m.metadata.custom).toEqual({ entry: "e_u1" });
    expect(m.createdAt).toEqual(new Date(1_760_860_800_000));
  });

  it("replays an answer's events into its parts, the products below the answer", () => {
    // Act
    const m = toThreadMessage({
      id: "e_v1",
      parent_id: "e_u1",
      role: "assistant",
      events: [
        { type: "tool_start", id: "0-0", name: "open", args: { summary: "打开 jd.com" } },
        { type: "tool_end", id: "0-0", result: { summary: "打开了", ok: true } },
        { type: "product", product: { id: 1, title: "XM5", url: "https://item.jd.com/1.html" } },
        { type: "saved", saved: { user: "e_u1", leaf: "e_v1" } },
        { type: "text", delta: "券后 2299" },
        { type: "something_new" }
      ],
      created_at: 1_760_860_810
    });

    // Assert: a malformed or unknown event is skipped, the rest fold as they did live
    expect(m.content.map((p) => p.type)).toEqual(["tool-call", "text", "data"]);
    expect(m.status).toEqual({ type: "complete", reason: "stop" });
    expect(m.metadata.custom).toEqual({ leaf: "e_v1" });
  });

  it("keeps what came before an error and marks the answer failed", () => {
    // Act
    const m = toThreadMessage({
      id: "e_v1",
      parent_id: "e_u1",
      role: "assistant",
      events: [
        { type: "text", delta: "查到一半" },
        { type: "error", message: "这次没能完成，请稍后再试。" }
      ],
      created_at: 1_760_860_810
    });

    // Assert
    expect(m.content).toEqual([{ type: "text", text: "查到一半" }]);
    expect(m.status).toEqual({ type: "incomplete", reason: "error", error: "这次没能完成，请稍后再试。" });
  });
});
