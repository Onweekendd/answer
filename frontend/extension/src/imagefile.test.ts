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

import { downloadImage } from "./imagefile";

const respond = (body: BodyInit, init: ResponseInit) => async () => new Response(body, init);

describe("downloadImage", () => {
  it("HTTP 失败时报错，提示改用截图", async () => {
    // Act
    const got = downloadImage("https://img.example.com/a.jpg", 1600, 85, respond("no", { status: 403 }));

    // Assert
    await expect(got).rejects.toMatchObject({ code: "EXEC_FAILED", message: expect.stringContaining("403") });
  });

  it("下载到的不是图片时报错", async () => {
    // Act
    const got = downloadImage(
      "https://img.example.com/a.jpg",
      1600,
      85,
      respond("<html>", { status: 200, headers: { "Content-Type": "text/html" } })
    );

    // Assert
    await expect(got).rejects.toMatchObject({ code: "EXEC_FAILED" });
  });

  it("svg 不下载：它可以带脚本，也转不成照片", async () => {
    // Act
    const got = downloadImage(
      "https://img.example.com/a.svg",
      1600,
      85,
      respond("<svg/>", { status: 200, headers: { "Content-Type": "image/svg+xml" } })
    );

    // Assert
    await expect(got).rejects.toMatchObject({ code: "EXEC_FAILED" });
  });

  it("参数不对时不下载", async () => {
    // Act
    const got = downloadImage("https://img.example.com/a.jpg", 10, 85, respond("", { status: 200 }));

    // Assert
    await expect(got).rejects.toMatchObject({ code: "BAD_MESSAGE" });
  });
});
