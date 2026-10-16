import { describe, expect, it, vi } from "vitest";

import { createTokenStore } from "./token";

function memoryStorage(initial: Record<string, string> = {}) {
  const data = new Map(Object.entries(initial));
  return {
    getItem: (k: string) => data.get(k) ?? null,
    setItem: (k: string, v: string) => void data.set(k, v),
    removeItem: (k: string) => void data.delete(k)
  };
}

const throwing = () => {
  const boom = () => {
    throw new Error("SecurityError");
  };
  return { getItem: boom, setItem: boom, removeItem: boom };
};

describe("createTokenStore", () => {
  it("starts empty and reports logged out", () => {
    const store = createTokenStore(memoryStorage());

    expect(store.get()).toBeNull();
    expect(store.isLoggedIn()).toBe(false);
  });

  it("reads a token persisted by an earlier session", () => {
    const store = createTokenStore(memoryStorage({ "answer.token": "abc" }));

    expect(store.get()).toBe("abc");
    expect(store.isLoggedIn()).toBe(true);
  });

  it("persists a token across store instances", () => {
    const storage = memoryStorage();
    createTokenStore(storage).set("abc");

    expect(createTokenStore(storage).get()).toBe("abc");
  });

  it("clear removes the token from memory and storage", () => {
    const storage = memoryStorage();
    const store = createTokenStore(storage);
    store.set("abc");

    store.clear();

    expect(store.get()).toBeNull();
    expect(createTokenStore(storage).get()).toBeNull();
  });

  it("notifies subscribers on set and clear, and stops after unsubscribe", () => {
    const store = createTokenStore(memoryStorage());
    const listener = vi.fn();
    const off = store.subscribe(listener);

    store.set("abc");
    store.clear();
    off();
    store.set("def");

    expect(listener).toHaveBeenCalledTimes(2);
  });

  it("does not notify when clearing an already empty store", () => {
    const store = createTokenStore(memoryStorage());
    const listener = vi.fn();
    store.subscribe(listener);

    store.clear();

    expect(listener).not.toHaveBeenCalled();
  });

  it("keeps working in memory when storage throws (private mode)", () => {
    const store = createTokenStore(throwing());

    expect(store.get()).toBeNull();
    store.set("abc");
    expect(store.get()).toBe("abc");
    store.clear();
    expect(store.get()).toBeNull();
  });
});
