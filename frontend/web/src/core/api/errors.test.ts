import { describe, expect, it } from "vitest";

import { ApiError } from "./client";
import { describeError, fieldErrors } from "./errors";

describe("fieldErrors", () => {
  it("maps the server field errors by field name", () => {
    const err = new ApiError(400, "x", "bad", [
      { error_field: "e_mail", error_msg: "邮箱格式不对" },
      { error_field: "pass", error_msg: "密码太短" }
    ]);

    expect(fieldErrors(err)).toEqual({ e_mail: "邮箱格式不对", pass: "密码太短" });
  });

  it("is empty for other errors, for non-array data and for malformed entries", () => {
    expect(fieldErrors(new Error("x"))).toEqual({});
    expect(fieldErrors(new ApiError(400, "x", "bad", null))).toEqual({});
    expect(fieldErrors(new ApiError(400, "x", "bad", [{ nope: 1 }, null, "str"]))).toEqual({});
  });
});

describe("describeError", () => {
  it("prefers the server message", () => {
    expect(describeError(new ApiError(400, "purchase.vote_closed", "投票已结束"))).toBe("投票已结束");
  });

  it("falls back to a generic line when the message is empty", () => {
    expect(describeError(new ApiError(500, "base.database_error", ""))).toBe("服务器出错了，请稍后再试");
  });

  it("has its own line for a network failure", () => {
    expect(describeError(new ApiError(0, "network_error", "网络连接失败，请稍后重试"))).toBe(
      "网络连接失败，请稍后重试"
    );
  });

  it("does not leak the text of an unexpected error", () => {
    expect(describeError(new TypeError("Cannot read properties of undefined (reading 'x')"))).toBe(
      "出了点问题，请稍后再试"
    );
  });
});
