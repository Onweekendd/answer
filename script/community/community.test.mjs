/* Licensed to the Apache Software Foundation (ASF) under one or more
 * contributor license agreements. See the NOTICE file distributed with this
 * work for additional information regarding copyright ownership.
 * The ASF licenses this file to You under the Apache License, Version 2.0
 * (the "License"); you may not use this file except in compliance with
 * the License. You may obtain a copy of the License at
 * http://www.apache.org/licenses/LICENSE-2.0
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

import assert from "node:assert/strict";
import { mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { createServer } from "node:http";
import { tmpdir } from "node:os";
import { resolve } from "node:path";
import test from "node:test";
import { inflateSync } from "node:zlib";

import {
  buildPrompt,
  chooseTopic,
  CommunityAPI,
  localClock,
  readDraft,
  runBatch,
  similarTitle,
  toPost,
  validateConfig,
} from "./community.mjs";
import { makeCover } from "./cover.mjs";

const draft = {
  draftId: "draft-1",
  title: "每天少洗十分钟碗，值得花两千吗？",
  content:
    "想把每天洗碗的十分钟省下来，又怕小厨房多一台机器更挤。假设预算两千，大家会优先考虑省时间还是省台面？",
  images: [],
  price: 0,
  category: "家电",
  vote: 2,
};
const fixedNow = () => new Date("2026-10-23T04:00:00Z");

function sampleConfig(extra = {}) {
  return validateConfig({
    baseUrl: "http://127.0.0.1:8081",
    postsPerDay: 1,
    minIntervalSeconds: 1,
    maxIntervalSeconds: 1,
    accounts: [
      {
        id: "rice",
        nickname: "米饭要趁热",
        persona: "句子短，有点幽默",
        tokenEnv: "TEST_TOKEN",
        categories: ["家电"],
      },
    ],
    topics: [
      {
        id: "dishwasher",
        category: "家电",
        request: "为了少洗碗买洗碗机，值不值？",
      },
    ],
    ...extra,
  });
}

function stream(text) {
  const bytes = new TextEncoder().encode(text);
  // Deliberately split Chinese characters and CRLF across individual chunks.
  return new ReadableStream({
    start(controller) {
      for (const byte of bytes) controller.enqueue(new Uint8Array([byte]));
      controller.close();
    },
  });
}

test("SSE preserves split UTF-8/CRLF, ignores heartbeat, and reads the server draft_post contract", async () => {
  const value = await readDraft(
    stream(
      `: ping\r\n\r\ndata: ${JSON.stringify({ type: "tool_suspend", name: "draft_post", input: draft })}\r\n\r\ndata: [DONE]\r\n\r\n`,
    ),
  );
  assert.deepEqual(value, draft);
});

test("SSE never publishes an interrupted draft or a stream that later errors", async () => {
  const first = `data: ${JSON.stringify({ type: "tool_suspend", name: "draft_post", input: draft })}\n\n`;
  await assert.rejects(readDraft(stream(first)), /提前中断/);
  await assert.rejects(
    readDraft(
      stream(first + 'data: {"type":"error","message":"private detail"}\n\n'),
    ),
    /AI 对话失败/,
  );
  await assert.rejects(
    readDraft(stream('data: {"type":"done"}\n\n')),
    /没有给出/,
  );
});

test("configuration validates dates, quotas, unique IDs, URLs and interval ranges", () => {
  assert.throws(() => sampleConfig({ postsPerDay: 100 }), /postsPerDay/);
  assert.throws(() => sampleConfig({ dailyAt: "25:00" }), /HH:mm/);
  assert.throws(() => sampleConfig({ timeZone: "invalid" }), RangeError);
  assert.throws(
    () => sampleConfig({ baseUrl: "https://user:password@site.invalid" }),
    /baseUrl/,
  );
  assert.throws(
    () =>
      sampleConfig({
        topics: [
          {
            id: "x",
            category: "a",
            request: "b",
            images: ["javascript:alert(1)"],
          },
        ],
      }),
    /images/,
  );
  assert.throws(
    () => sampleConfig({ minIntervalSeconds: 60, maxIntervalSeconds: 20 }),
    /间隔/,
  );
  const config = sampleConfig();
  assert.throws(
    () => sampleConfig({ accounts: [...config.accounts, ...config.accounts] }),
    /唯一/,
  );
  assert.equal(
    localClock("America/Tijuana", new Date("2026-10-23T02:00:00Z")).day,
    "2026-10-22",
  );
});

test("topic rotation respects interests and cooldown; title deduplication ignores punctuation", () => {
  const config = sampleConfig();
  const account = config.accounts[0];
  assert.equal(chooseTopic(config, account, "2026-10-23", {}).id, "dishwasher");
  assert.equal(
    chooseTopic(config, account, "2026-10-23", {
      x: { day: "2026-10-22", topicId: "dishwasher", status: "published" },
    }),
    undefined,
  );
  assert.equal(
    chooseTopic(config, account, "2026-10-23", {
      x: { day: "2026-10-01", topicId: "dishwasher", status: "published" },
    }).id,
    "dishwasher",
  );
  assert(similarTitle(draft.title, draft.title.replace("？", "！")));
  assert(!similarTitle(draft.title, "想买掌机是为了有空的自己吗"));
  assert.match(
    buildPrompt(account, config.topics[0], "2026-10-23", [draft.title]),
    /米饭要趁热/,
  );
});

test("post validation rejects invalid prices/images and retains ordinary account content", () => {
  const topic = sampleConfig().topics[0];
  const post = toPost(draft, topic, "https://site.invalid/cover.png");
  assert.equal(post.title, draft.title);
  assert.equal(post.content, draft.content);
  assert.deepEqual(post.purchase.images, ["https://site.invalid/cover.png"]);
  assert.deepEqual(post.tags[0].slug_name, "家电");
  assert.throws(
    () => toPost({ ...draft, images: ["data:bad"] }, topic),
    /图片/,
  );
  assert.throws(() => toPost({ ...draft, price: -1 }, topic), /价格/);
  assert.throws(() => toPost({ ...draft, title: "短" }, topic), /标题/);
});

test("fallback cover is a deterministic, decodable RGB PNG with an unfiltered row for each line", () => {
  const png = makeCover("dishwasher");
  assert.equal(png.subarray(1, 4).toString(), "PNG");
  assert.deepEqual(png, makeCover("dishwasher"));
  const width = png.readUInt32BE(16);
  const height = png.readUInt32BE(20);
  const chunks = [];
  for (let i = 8; i < png.length;) {
    const size = png.readUInt32BE(i);
    if (png.subarray(i + 4, i + 8).toString() === "IDAT")
      chunks.push(png.subarray(i + 8, i + 8 + size));
    i += 12 + size;
  }
  assert.equal(
    inflateSync(Buffer.concat(chunks)).length,
    height * (width * 3 + 1),
  );
});

async function fixture(t, { mode = "success", customDraft = draft } = {}) {
  const root = await mkdtemp(resolve(tmpdir(), "answer-community-"));
  t.after(async () => {
    if (!resolve(root).startsWith(resolve(tmpdir(), "answer-community-")))
      throw new Error("Unexpected test cleanup path");
    await rm(root, { recursive: true, force: true });
  });
  const calls = [];
  const server = createServer(async (request, response) => {
    const parts = [];
    for await (const part of request) parts.push(part);
    const raw = Buffer.concat(parts).toString();
    const path = request.url.replace("/answer/api/v1", "");
    calls.push({ path, raw, authorization: request.headers.authorization });
    const json = (value, code = 200) => {
      response.writeHead(code, { "Content-Type": "application/json" });
      response.end(
        JSON.stringify({
          code,
          data: value,
          reason: code === 400 ? "captcha_needed" : "",
        }),
      );
    };
    if (path === "/purchase/chat") return json({ id: "123" });
    if (path === "/purchase/ai/chat") {
      response.writeHead(200, { "Content-Type": "text/event-stream" });
      response.end(
        `data: ${JSON.stringify({ type: "tool_suspend", name: "draft_post", input: customDraft })}\n\ndata: [DONE]\n\n`,
      );
      return;
    }
    if (path === "/file") return json("/uploads/post/cover.png");
    if (path === "/question") {
      if (mode === "lost-response") {
        request.socket.destroy();
        return;
      }
      if (mode === "captcha") return json(null, 400);
      return json({ id: "42" });
    }
    if (path === "/purchase/ai/draft/result") return json(null);
    response.writeHead(404);
    response.end();
  });
  await new Promise((accept) => server.listen(0, "127.0.0.1", accept));
  t.after(() => new Promise((accept) => server.close(accept)));
  const config = sampleConfig({
    baseUrl: `http://127.0.0.1:${server.address().port}`,
    stateFile: resolve(root, "state.json"),
  });
  const logs = [];
  const options = {
    publish: true,
    env: { TEST_TOKEN: "test-secret-token" },
    now: fixedNow,
    log: (text) => logs.push(text),
    wait: async () => {},
  };
  return { root, config, calls, logs, options };
}

test("published job uploads fallback, posts once, records ID, and is not repeated on restart", async (t) => {
  const f = await fixture(t);
  assert.equal((await runBatch(f.config, f.options)).failures, 0);
  assert.equal((await runBatch(f.config, f.options)).failures, 0);
  assert.equal(f.calls.filter((call) => call.path === "/question").length, 1);
  assert.equal(f.calls.filter((call) => call.path === "/file").length, 1);
  assert(!f.calls.some((call) => /vote/.test(call.path)));
  const published = JSON.parse(
    f.calls.find((call) => call.path === "/question").raw,
  );
  assert.equal(
    published.purchase.images[0],
    `${f.config.baseUrl}/uploads/post/cover.png`,
  );
  const state = JSON.parse(await readFile(f.config.stateFile, "utf8"));
  assert.equal(state.jobs["2026-10-23/rice"].questionId, "42");
  assert.equal(state.jobs["2026-10-23/rice"].status, "published");
  assert(!JSON.stringify(state).includes("test-secret-token"));
  assert(!f.logs.join("\n").includes("test-secret-token"));
});

test("dry-run makes a real AI draft but never uploads, posts, or consumes a daily quota", async (t) => {
  const f = await fixture(t);
  await runBatch(f.config, { ...f.options, publish: false });
  assert(
    !f.calls.some((call) =>
      ["/file", "/question", "/purchase/ai/draft/result"].includes(call.path),
    ),
  );
  await assert.rejects(readFile(f.config.stateFile), { code: "ENOENT" });
  const preview = JSON.parse(
    await readFile(resolve(f.root, "preview-2026-10-23-rice.json"), "utf8"),
  );
  assert.equal(preview.post.title, draft.title);
  assert.equal((await runBatch(f.config, f.options)).failures, 0);
});

test("lost publish response is journaled as uncertain and never retried blindly", async (t) => {
  const f = await fixture(t, { mode: "lost-response" });
  assert.equal((await runBatch(f.config, f.options)).failures, 1);
  await runBatch(f.config, f.options);
  assert.equal(f.calls.filter((call) => call.path === "/question").length, 1);
  const state = JSON.parse(await readFile(f.config.stateFile, "utf8"));
  assert.equal(state.jobs["2026-10-23/rice"].status, "publishing");
});

test("a draft completed after midnight is not published under the previous day's quota", async (t) => {
  const f = await fixture(t);
  let now = fixedNow();
  const result = await runBatch(f.config, {
    ...f.options,
    now: () => now,
    apiFactory: (config) => {
      const api = new CommunityAPI(config);
      const generate = api.draft.bind(api);
      api.draft = async (...args) => {
        const value = await generate(...args);
        now = new Date("2026-10-24T04:00:00Z");
        return value;
      };
      return api;
    },
  });
  assert.equal(result.failures, 0);
  assert(!f.calls.some((call) => call.path === "/question"));
  const state = JSON.parse(await readFile(f.config.stateFile, "utf8"));
  assert.equal(state.jobs["2026-10-23/rice"].status, "skipped");
});

test("explicit rejection allows a bounded retry, including when the regenerated title is identical", async (t) => {
  const f = await fixture(t, { mode: "captcha" });
  assert.equal((await runBatch(f.config, f.options)).failures, 1);
  assert.equal((await runBatch(f.config, f.options)).failures, 1);
  await runBatch(f.config, f.options);
  assert.equal(f.calls.filter((call) => call.path === "/question").length, 2);
  const state = JSON.parse(await readFile(f.config.stateFile, "utf8"));
  assert.equal(state.jobs["2026-10-23/rice"].status, "failed");
  assert.equal(state.jobs["2026-10-23/rice"].attempts, 2);
});

test("preexisting lock or corrupt state stops before calling any service", async (t) => {
  const f = await fixture(t);
  await writeFile(`${f.config.stateFile}.lock`, "999999");
  await assert.rejects(runBatch(f.config, f.options), /另一个发帖任务/);
  await rm(`${f.config.stateFile}.lock`);
  await writeFile(f.config.stateFile, "broken");
  await assert.rejects(runBatch(f.config, f.options), SyntaxError);
  assert.equal(f.calls.length, 0);
});

test("account login uses raw Authorization token and stops on unverified mail", async () => {
  const api = new CommunityAPI(sampleConfig(), "", async (_url, init) => {
    assert.equal(JSON.parse(init.body).e_mail, "user@example.invalid");
    return Response.json({
      code: 200,
      data: { access_token: "", mail_status: 2 },
    });
  });
  await assert.rejects(
    api.login(
      { id: "x", emailEnv: "EMAIL", passwordEnv: "PASS" },
      { EMAIL: "user@example.invalid", PASS: "private-password" },
    ),
    /邮箱验证/,
  );
});
