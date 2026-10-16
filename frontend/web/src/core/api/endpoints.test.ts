import { describe, expect, it, vi } from "vitest";

import type { ApiClient } from "./client";
import { createEndpoints, type LoginResp, needsEmailVerification } from "./endpoints";

function fakeClient(reply: unknown = null) {
  const client = {
    get: vi.fn(async () => reply),
    post: vi.fn(async () => reply),
    upload: vi.fn(async () => reply)
  };
  return {
    client: client as unknown as ApiClient & typeof client,
    api: createEndpoints(client as unknown as ApiClient)
  };
}

describe("createEndpoints", () => {
  it("login posts the server field names e_mail and pass", async () => {
    const { client, api } = fakeClient({ access_token: "t" });

    await api.login("a@b.com", "secret123");

    expect(client.post).toHaveBeenCalledWith("/user/login/email", { e_mail: "a@b.com", pass: "secret123" });
  });

  it("register posts name, e_mail and pass", async () => {
    const { client, api } = fakeClient({});

    await api.register("alice", "a@b.com", "secret123");

    expect(client.post).toHaveBeenCalledWith("/user/register/email", {
      name: "alice",
      e_mail: "a@b.com",
      pass: "secret123"
    });
  });

  it("feed fetches the community feed and returns the list", async () => {
    const { client, api } = fakeClient([{ id: "1" }]);

    await expect(api.communityFeed()).resolves.toEqual([{ id: "1" }]);
    expect(client.get).toHaveBeenCalledWith("/community/feed", undefined, undefined);
  });

  it("feed turns a null reply into an empty list", async () => {
    const { api } = fakeClient(null);

    await expect(api.communityFeed()).resolves.toEqual([]);
  });

  it("feed forwards the abort signal", async () => {
    const { client, api } = fakeClient([]);
    const signal = new AbortController().signal;

    await api.communityFeed(signal);

    expect(client.get).toHaveBeenCalledWith("/community/feed", undefined, { signal });
  });

  it("vote posts question_id and choice", async () => {
    const { client, api } = fakeClient({});

    await api.vote("42", 3);

    expect(client.post).toHaveBeenCalledWith("/purchase/vote", { question_id: "42", choice: 3 });
  });

  it('uploadImage sends the file as multipart field "file" with source "post" and returns the url', async () => {
    // the server's data is the url string itself
    const { client, api } = fakeClient("/uploads/post/a.png");
    const file = new File(["x"], "a.png", { type: "image/png" });

    await expect(api.uploadImage(file)).resolves.toBe("/uploads/post/a.png");

    const form = (client.upload.mock.calls[0] as unknown[])[1] as FormData;
    expect(form.get("file")).toBeInstanceOf(File);
    expect(form.get("source")).toBe("post");
  });
});

describe("needsEmailVerification", () => {
  const resp = (over: Partial<LoginResp>): LoginResp => ({
    id: "1",
    username: "a",
    access_token: "t",
    mail_status: 1,
    ...over
  });

  it("is true while the mail is still to be verified (status 2)", () => {
    expect(needsEmailVerification(resp({ mail_status: 2 }))).toBe(true);
  });

  it("is false once verified or when no verification is required", () => {
    expect(needsEmailVerification(resp({ mail_status: 1 }))).toBe(false);
  });

  it("is true when the server gave no token to log in with", () => {
    expect(needsEmailVerification(resp({ access_token: "" }))).toBe(true);
  });
});
