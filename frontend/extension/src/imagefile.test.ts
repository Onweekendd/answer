/*
 * Licensed to the Apache Software Foundation (ASF) under one
 * or more contributor license agreements.  See the NOTICE file
 * distributed with this work for additional information
 * regarding copyright ownership.  The ASF licenses this file
 * to you under the Apache License, Version 2.0 (the
 * "License"); you may not use this file except in compliance
 * with the License.  You may obtain a copy of the License at
 *
 *   http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing,
 * software distributed under the License is distributed on an
 * "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
 * KIND, either express or implied.  See the License for the
 * specific language governing permissions and limitations
 * under the License.
 */

import { describe, expect, it } from "vitest";

import { downloadImage, isRaster } from "./imagefile";

const respond = (body: BodyInit, init: ResponseInit) => (async () => new Response(body, init)) as typeof fetch;

describe("isRaster", () => {
  it.each([
    ["jpeg", [0xff, 0xd8, 0xff, 0xe0]],
    ["png", [0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a]],
    ["webp", [...new TextEncoder().encode("RIFF\0\0\0\0WEBP")]]
  ])("认得 %s", (_, bytes) => {
    expect(isRaster(new Uint8Array(bytes))).toBe(true);
  });

  it("svg 和 html 不算", () => {
    expect(isRaster(new TextEncoder().encode("<svg/>"))).toBe(false);
    expect(isRaster(new TextEncoder().encode("<!doctype html>"))).toBe(false);
  });
});

describe("downloadImage", () => {
  it("HTTP 失败时报错，提示改用截图", async () => {
    // Act
    const got = downloadImage("https://img.example.com/a.jpg", 1600, 85, { fetch: respond("no", { status: 403 }) });

    // Assert
    await expect(got).rejects.toMatchObject({ code: "EXEC_FAILED", message: expect.stringContaining("403") });
  });

  it("下载到的是 html 错误页时报错，哪怕标着 image/jpeg", async () => {
    // Act
    const got = downloadImage("https://img.example.com/a.jpg", 1600, 85, {
      fetch: respond("<html>", { status: 200, headers: { "Content-Type": "image/jpeg" } })
    });

    // Assert
    await expect(got).rejects.toMatchObject({ code: "EXEC_FAILED" });
  });

  it("页面里已经有的图不再下载", async () => {
    // Arrange：页面那份是 svg，所以被拒；没有去下载
    const fetched: string[] = [];
    const doFetch = (async (url: RequestInfo | URL) => {
      fetched.push(String(url));
      return new Response(new Uint8Array([0xff, 0xd8, 0xff]), { status: 200 });
    }) as typeof fetch;

    // Act
    const got = downloadImage("https://img.example.com/a.jpg", 1600, 85, {
      fromPage: async () => new TextEncoder().encode("<svg/>"),
      fetch: doFetch
    });

    // Assert
    await expect(got).rejects.toMatchObject({ code: "EXEC_FAILED" });
    expect(fetched).toEqual([]);
  });

  it("页面里拿不到时才下载", async () => {
    // Arrange
    const fetched: string[] = [];
    const doFetch = (async (url: RequestInfo | URL) => {
      fetched.push(String(url));
      return new Response("<svg/>", { status: 200 });
    }) as typeof fetch;

    // Act
    const got = downloadImage("https://img.example.com/a.jpg", 1600, 85, {
      fromPage: async () => null,
      fetch: doFetch
    });

    // Assert
    await expect(got).rejects.toMatchObject({ code: "EXEC_FAILED" });
    expect(fetched).toEqual(["https://img.example.com/a.jpg"]);
  });

  it("参数不对时不下载", async () => {
    // Act
    const got = downloadImage("https://img.example.com/a.jpg", 10, 85, { fetch: respond("", { status: 200 }) });

    // Assert
    await expect(got).rejects.toMatchObject({ code: "BAD_MESSAGE" });
  });
});
