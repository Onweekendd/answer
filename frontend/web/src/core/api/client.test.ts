import { describe, expect, it, vi } from "vitest";

import { ApiError, createApiClient } from "./client";

type FetchMock = ReturnType<typeof vi.fn<typeof fetch>>;

const json = (status: number, body: unknown) =>
  new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" }
  });

const ok = (data: unknown) => json(200, { code: 200, reason: "base.success", msg: "ok", data });

function setup(opts: { token?: string | null; response?: Response | Error } = {}) {
  const fetchMock: FetchMock = vi.fn<typeof fetch>(async () => {
    const r = opts.response ?? ok(null);
    if (r instanceof Error) {
      throw r;
    }
    return r;
  });
  const onUnauthorized = vi.fn();
  const client = createApiClient({
    baseUrl: "/answer/api/v1",
    getToken: () => opts.token ?? null,
    onUnauthorized,
    fetch: fetchMock
  });
  return { client, fetchMock, onUnauthorized };
}

const lastCall = (m: FetchMock) => {
  const [url, init] = m.mock.calls.at(-1)!;
  return { url: String(url), init: init ?? {}, headers: new Headers(init?.headers) };
};

describe("createApiClient", () => {
  it("unwraps data from the {code, reason, msg, data} envelope", async () => {
    const { client } = setup({ response: ok({ id: "7" }) });

    await expect(client.get("/community/feed")).resolves.toEqual({ id: "7" });
  });

  it("sends the raw token in the Authorization header when logged in", async () => {
    const { client, fetchMock } = setup({ token: "abc123" });

    await client.get("/community/feed");

    expect(lastCall(fetchMock).headers.get("Authorization")).toBe("abc123");
  });

  it("sends no Authorization header when logged out", async () => {
    const { client, fetchMock } = setup({ token: null });

    await client.get("/community/feed");

    expect(lastCall(fetchMock).headers.has("Authorization")).toBe(false);
  });

  it("joins the base url and appends query params, skipping undefined ones", async () => {
    const { client, fetchMock } = setup();

    await client.get("/question/page", { page: 1, order: undefined, q: "a b" });

    expect(lastCall(fetchMock).url).toBe("/answer/api/v1/question/page?page=1&q=a+b");
  });

  it("posts a JSON body with a JSON content type", async () => {
    const { client, fetchMock } = setup();

    await client.post("/purchase/vote", { question_id: "1", choice: 1 });

    const { init, headers } = lastCall(fetchMock);
    expect(init.method).toBe("POST");
    expect(headers.get("Content-Type")).toBe("application/json");
    expect(init.body).toBe(JSON.stringify({ question_id: "1", choice: 1 }));
  });

  it("uploads FormData without forcing a content type", async () => {
    const { client, fetchMock } = setup();
    const form = new FormData();
    form.append("file", new Blob(["x"]), "a.png");

    await client.upload("/file", form);

    const { init, headers } = lastCall(fetchMock);
    expect(init.method).toBe("POST");
    expect(init.body).toBe(form);
    // the browser must set the multipart boundary itself
    expect(headers.has("Content-Type")).toBe(false);
  });

  it("throws ApiError carrying status, reason and message on a business error", async () => {
    const { client } = setup({
      response: json(400, { code: 400, reason: "purchase.vote_closed", msg: "投票已结束", data: null })
    });

    const err = await client.post("/purchase/vote", {}).catch((e: unknown) => e);

    expect(err).toBeInstanceOf(ApiError);
    expect(err).toMatchObject({ status: 400, reason: "purchase.vote_closed", message: "投票已结束" });
  });

  it("keeps the field errors the server returns in data", async () => {
    const fields = [{ error_field: "e_mail", error_msg: "邮箱或密码错误" }];
    const { client } = setup({
      response: json(400, { code: 400, reason: "error.object.email_or_password_incorrect", msg: "", data: fields })
    });

    const err = (await client.post("/user/login/email", {}).catch((e: unknown) => e)) as ApiError;

    expect(err.data).toEqual(fields);
  });

  it("calls onUnauthorized on 401 and still throws", async () => {
    const { client, onUnauthorized } = setup({
      token: "stale",
      response: json(401, { code: 401, reason: "base.unauthorized_error", msg: "", data: null })
    });

    await expect(client.get("/community/feed")).rejects.toMatchObject({ status: 401 });

    expect(onUnauthorized).toHaveBeenCalledTimes(1);
  });

  it("does not call onUnauthorized for other errors", async () => {
    const { client, onUnauthorized } = setup({
      response: json(500, { code: 500, reason: "base.database_error", msg: "", data: null })
    });

    await expect(client.get("/x")).rejects.toBeInstanceOf(ApiError);

    expect(onUnauthorized).not.toHaveBeenCalled();
  });

  it("treats HTTP 200 with a non-200 body code as an error", async () => {
    const { client } = setup({ response: json(200, { code: 400, reason: "x", msg: "bad", data: null }) });

    await expect(client.get("/x")).rejects.toMatchObject({ status: 400, reason: "x" });
  });

  it("wraps a network failure as ApiError with status 0", async () => {
    const { client } = setup({ response: new TypeError("Failed to fetch") });

    await expect(client.get("/x")).rejects.toMatchObject({ status: 0, reason: "network_error" });
  });

  it("wraps a non-JSON response as ApiError instead of a SyntaxError", async () => {
    const { client } = setup({ response: new Response("<html>502</html>", { status: 502 }) });

    await expect(client.get("/x")).rejects.toMatchObject({ status: 502, reason: "bad_response" });
  });

  it("lets an aborted request surface as AbortError, not ApiError", async () => {
    const abort = new DOMException("aborted", "AbortError");
    const { client } = setup({ response: abort });

    const err = await client.get("/x").catch((e: unknown) => e);

    expect(err).not.toBeInstanceOf(ApiError);
    expect((err as DOMException).name).toBe("AbortError");
  });
});
