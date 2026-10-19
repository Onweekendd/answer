import { afterEach, beforeEach, describe, expect, it } from "vitest";

import { attachmentId, UploadImageAdapter } from "./upload";

// The demo site is plain http on an IP, not a secure context, so the browser has no
// crypto.randomUUID there. These tests take it away the same way.
const original = Object.getOwnPropertyDescriptor(Object.getPrototypeOf(crypto), "randomUUID");

beforeEach(() => {
  Object.defineProperty(crypto, "randomUUID", { value: undefined, configurable: true });
});

afterEach(() => {
  delete (crypto as { randomUUID?: unknown }).randomUUID;
  if (original && !("randomUUID" in crypto)) {
    Object.defineProperty(Object.getPrototypeOf(crypto), "randomUUID", original);
  }
});

describe("attachmentId", () => {
  it("works without crypto.randomUUID", () => {
    // Act
    const a = attachmentId();
    const b = attachmentId();

    // Assert
    expect(a).toMatch(/^[0-9a-f]{32}$/);
    expect(a).not.toBe(b);
  });
});

describe("UploadImageAdapter", () => {
  it("adds a picked image even when crypto.randomUUID is missing", async () => {
    // Arrange
    const adapter = new UploadImageAdapter(async () => "http://site/uploads/post/a.jpg");
    const file = new File(["x"], "shot.png", { type: "image/png" });

    // Act
    const pending = await adapter.add({ file });
    const done = await adapter.send(pending);

    // Assert
    expect(pending.id).toMatch(/^[0-9a-f]{32}$/);
    expect(done.content).toEqual([{ type: "image", image: "http://site/uploads/post/a.jpg" }]);
  });
});
