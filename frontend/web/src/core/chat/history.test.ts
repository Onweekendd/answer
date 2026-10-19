import { describe, expect, it, vi } from "vitest";

import type { ThreadMessage } from "@assistant-ui/react";

import { type ChatHistoryApi, createChatHistory, fromStored, toStored } from "./history";

function userMessage(id: string, text: string): ThreadMessage {
  return {
    id,
    role: "user",
    createdAt: new Date("2026-10-19T08:00:00Z"),
    content: [{ type: "text", text }],
    attachments: [
      {
        id: "a1",
        type: "image",
        name: "shot.png",
        contentType: "image/png",
        status: { type: "complete" },
        file: new File(["x"], "shot.png"),
        content: [{ type: "image", image: "http://site/uploads/post/a.jpg" }]
      }
    ],
    metadata: { custom: {} }
  } as ThreadMessage;
}

function fakeApi(): ChatHistoryApi & { saved: [string, string, string | null, unknown][] } {
  const saved: [string, string, string | null, unknown][] = [];
  return {
    saved,
    chatGet: vi.fn(),
    chatCreate: vi.fn(async () => ({ id: "7" })),
    chatSaveMessage: vi.fn(async (chat: string, id: string, parent: string | null, message: unknown) => {
      saved.push([chat, id, parent, message]);
      return null;
    })
  };
}

describe("createChatHistory", () => {
  it("creates the conversation once, titled with the question, then saves into it", async () => {
    // Arrange
    const api = fakeApi();
    const onCreated = vi.fn();
    const history = createChatHistory({ chatId: null, api, onCreated });

    // Act: the user's message and the answer settle close together
    await Promise.all([
      history.append({ parentId: null, message: userMessage("u1", " 这个显示器值不值 ") }),
      history.append({ parentId: "u1", message: userMessage("a1", "") })
    ]);

    // Assert
    expect(api.chatCreate).toHaveBeenCalledTimes(1);
    expect(api.chatCreate).toHaveBeenCalledWith("这个显示器值不值");
    expect(onCreated).toHaveBeenCalledWith("7");
    expect(api.saved.map(([chat, id, parent]) => [chat, id, parent])).toEqual([
      ["7", "u1", null],
      ["7", "a1", "u1"]
    ]);
  });

  it("tries to create again after a failed create", async () => {
    // Arrange
    const api = fakeApi();
    vi.mocked(api.chatCreate).mockRejectedValueOnce(new Error("offline"));
    const history = createChatHistory({ chatId: null, api });

    // Act
    await expect(history.append({ parentId: null, message: userMessage("u1", "hi") })).rejects.toThrow("offline");
    await history.append({ parentId: null, message: userMessage("u1", "hi") });

    // Assert
    expect(api.chatCreate).toHaveBeenCalledTimes(2);
    expect(api.saved).toHaveLength(1);
  });

  it("loads a saved conversation with the first message as the root", async () => {
    // Arrange
    const api = fakeApi();
    const stored = toStored(userMessage("u1", "hi"));
    vi.mocked(api.chatGet).mockResolvedValue({
      id: "7",
      title: "hi",
      messages: [{ parent_id: "", message: JSON.parse(JSON.stringify(stored)) }]
    });
    const history = createChatHistory({ chatId: "7", api });

    // Act
    const repo = await history.load();

    // Assert
    expect(history.scopeId).toBe("7");
    expect(repo.messages).toHaveLength(1);
    expect(repo.messages[0].parentId).toBeNull();
    expect(repo.messages[0].message.createdAt).toEqual(new Date("2026-10-19T08:00:00Z"));
  });

  it("loads nothing for a new conversation", async () => {
    // Arrange
    const api = fakeApi();
    const history = createChatHistory({ chatId: null, api });

    // Act
    const repo = await history.load();

    // Assert
    expect(repo.messages).toEqual([]);
    expect(api.chatGet).not.toHaveBeenCalled();
  });
});

describe("toStored / fromStored", () => {
  it("drops the browser File but keeps the uploaded image url", () => {
    // Act
    const back = fromStored(JSON.parse(JSON.stringify(toStored(userMessage("u1", "hi")))));

    // Assert
    const attachment = back.attachments?.[0];
    expect(attachment).not.toHaveProperty("file");
    expect(attachment?.content).toEqual([{ type: "image", image: "http://site/uploads/post/a.jpg" }]);
  });
});
