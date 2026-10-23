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

import { createHash, randomInt } from "node:crypto";
import {
  mkdir,
  open,
  readFile,
  rename,
  unlink,
  writeFile,
} from "node:fs/promises";
import { dirname, isAbsolute, resolve } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";
import { setTimeout as sleep } from "node:timers/promises";

import { makeCover } from "./cover.mjs";

const directory = dirname(fileURLToPath(import.meta.url));
const hash = (text) => createHash("sha256").update(text).digest("hex");
const terminal = new Set(["published", "publishing", "skipped"]);
const httpURL = (value) => {
  try {
    return (
      typeof value === "string" &&
      ["http:", "https:"].includes(new URL(value).protocol)
    );
  } catch {
    return false;
  }
};

export function localClock(timeZone, now = new Date()) {
  const parts = Object.fromEntries(
    new Intl.DateTimeFormat("en-CA", {
      timeZone,
      year: "numeric",
      month: "2-digit",
      day: "2-digit",
      hour: "2-digit",
      minute: "2-digit",
      hourCycle: "h23",
    })
      .formatToParts(now)
      .map(({ type, value }) => [type, value]),
  );
  return {
    day: `${parts.year}-${parts.month}-${parts.day}`,
    time: `${parts.hour}:${parts.minute}`,
  };
}

export function validateConfig(input, configDirectory = directory) {
  const config = {
    timeZone: "Asia/Shanghai",
    dailyAt: "10:00",
    postsPerDay: 3,
    minIntervalSeconds: 900,
    maxIntervalSeconds: 3600,
    topicCooldownDays: 7,
    requestTimeoutSeconds: 300,
    stateFile: resolve(directory, "out/state.json"),
    ...input,
  };
  if (!httpURL(config.baseUrl))
    throw new Error("baseUrl 必须是社区的 http(s) 地址");
  const base = new URL(config.baseUrl);
  if (base.username || base.password || base.search || base.hash)
    throw new Error("baseUrl 不能包含账号、查询参数或锚点");
  config.baseUrl = config.baseUrl.replace(/\/+$/, "");
  if (!/^([01]\d|2[0-3]):[0-5]\d$/.test(config.dailyAt))
    throw new Error("dailyAt 格式为 HH:mm");
  localClock(config.timeZone);
  for (const [key, min, max] of [
    ["postsPerDay", 1, 12],
    ["minIntervalSeconds", 1, 86400],
    ["maxIntervalSeconds", 1, 86400],
    ["topicCooldownDays", 1, 365],
    ["requestTimeoutSeconds", 10, 1800],
  ]) {
    if (
      !Number.isInteger(config[key]) ||
      config[key] < min ||
      config[key] > max
    )
      throw new Error(`${key} 必须为 ${min} 到 ${max} 的整数`);
  }
  if (config.maxIntervalSeconds < config.minIntervalSeconds)
    throw new Error("最大发帖间隔不能小于最小间隔");
  for (const kind of ["accounts", "topics"]) {
    if (!Array.isArray(config[kind]) || !config[kind].length)
      throw new Error(`${kind} 不能为空`);
    const ids = new Set();
    for (const item of config[kind]) {
      if (!/^[a-z0-9_-]+$/i.test(item.id) || ids.has(item.id))
        throw new Error(
          `${kind} 的 id 必须唯一，且只能有字母、数字、下划线、短横线`,
        );
      ids.add(item.id);
    }
  }
  for (const account of config.accounts) {
    if (
      typeof account.nickname !== "string" ||
      [...account.nickname].length < 2 ||
      [...account.nickname].length > 30 ||
      typeof account.persona !== "string" ||
      account.persona.length > 1000
    )
      throw new Error(
        `账号 ${account.id} 需要 2～30 字的昵称和不超过 1000 字的 persona`,
      );
    if (!account.tokenEnv && !(account.emailEnv && account.passwordEnv))
      throw new Error(
        `账号 ${account.id} 需要 tokenEnv 或 emailEnv/passwordEnv`,
      );
    if (
      account.categories &&
      (!Array.isArray(account.categories) ||
        !account.categories.every((v) => typeof v === "string"))
    )
      throw new Error(`账号 ${account.id} 的 categories 必须是字符串数组`);
  }
  for (const topic of config.topics) {
    if (
      typeof topic.request !== "string" ||
      !topic.request.trim() ||
      topic.request.length > 2000 ||
      typeof topic.category !== "string" ||
      !topic.category.trim() ||
      [...topic.category].length > 20
    )
      throw new Error(`话题 ${topic.id} 缺少有效的 request/category`);
    if (
      topic.images &&
      (!Array.isArray(topic.images) ||
        topic.images.length > 5 ||
        !topic.images.every(httpURL))
    )
      throw new Error(
        `话题 ${topic.id} 的 images 必须是最多 5 个 http(s) 图片地址`,
      );
    if (topic.coverFile)
      topic.coverFile = resolve(configDirectory, topic.coverFile);
  }
  config.stateFile = isAbsolute(config.stateFile)
    ? config.stateFile
    : resolve(configDirectory, config.stateFile);
  return config;
}

function ranked(items, seed) {
  return [...items].sort((a, b) =>
    hash(`${seed}/${a.id}`).localeCompare(hash(`${seed}/${b.id}`)),
  );
}

export function chooseTopic(config, account, day, jobs) {
  const recent = Object.values(jobs).filter(
    (job) =>
      ["published", "publishing"].includes(job.status) &&
      (Date.parse(day) - Date.parse(job.day)) / 86400000 <
        config.topicCooldownDays,
  );
  return ranked(
    config.topics.filter(
      (topic) =>
        (!account.categories?.length ||
          account.categories.includes(topic.category)) &&
        !recent.some((job) => job.topicId === topic.id),
    ),
    `${day}/${account.id}`,
  )[0];
}

function grams(text) {
  const chars = [...text.toLowerCase().replace(/[\p{P}\p{S}\s]/gu, "")];
  return new Set(chars.slice(1).map((char, i) => chars[i] + char));
}

export function similarTitle(title, previous) {
  const a = grams(title);
  const b = grams(previous);
  if (!a.size || !b.size) return title.trim() === previous.trim();
  const overlap = [...a].filter((value) => b.has(value)).length;
  return overlap / (a.size + b.size - overlap) >= 0.65;
}

export function buildPrompt(account, topic, day, recentTitles) {
  return `今天是 ${day}。帮我把下面这个购物纠结整理成一篇社区帖子，直接调用 draft_post 交出草稿。
发帖昵称：${account.nickname}。表达习惯：${account.persona}
讨论方向：${topic.request}
写作要求：
- 标题 6～45 字，抓住一个具体的取舍，有意思但不夸张，让人想讲自己的选择。可以有一点幽默，不要写成广告或科普文章。
- 正文 80～220 字，两三小段即可，不要固定套“背景、优点、缺点、总结”。开头和句式要变化。收尾只问一个容易回答的问题。
- 可以用口语和第一人称表达待购买的犹豫，但不要编造已经购买、使用、亲测的经历、职业、家人或成交记录。没有给出的个人经历用假设来问。
- 不要捏造现价、折扣、参数或引用。题目中的金额是考虑的预算，不能当成查证过的价格；不知道实际价格时草稿 price 填 0，link 留空。
- 不要写买不买的结论，把选择留给投票。category 填「${topic.category}」。可以没有配图，脚本会补讨论封面。请直接出草稿，不要追问，不需要打开浏览器、生成网页或做额外调研。
最近已经讨论过的标题（不要改几个词重复发，要换一个细节或矛盾切入）：
${
  recentTitles
    .slice(-20)
    .map((title) => `- ${title}`)
    .join("\n") || "暂无"
}`;
}

class APIError extends Error {
  constructor(path, status, code, reason) {
    super(
      `${path} 请求失败：HTTP ${status} / ${code ?? "?"} (${reason || "unknown"})`,
    );
    this.status = status;
    this.code = code;
  }
}

export class CommunityAPI {
  constructor(config, token = "", doFetch = fetch) {
    this.url = `${config.baseUrl}/answer/api/v1`;
    this.timeout = config.requestTimeoutSeconds * 1000;
    this.token = token;
    this.fetch = doFetch;
  }

  async request(path, body, { form = false, stream = false } = {}) {
    const headers = { ...(form ? {} : { "Content-Type": "application/json" }) };
    if (this.token) headers.Authorization = this.token;
    const response = await this.fetch(`${this.url}${path}`, {
      method: "POST",
      headers,
      body: form ? body : JSON.stringify(body),
      signal: AbortSignal.timeout(this.timeout),
      redirect: "error",
    });
    if (
      stream &&
      response.ok &&
      response.headers.get("content-type")?.startsWith("text/event-stream")
    )
      return response;
    const envelope = await response.json().catch(() => null);
    if (!response.ok || envelope?.code !== 200)
      throw new APIError(
        path,
        response.status,
        envelope?.code,
        envelope?.reason,
      );
    if (stream) throw new Error("AI 接口没有返回事件流");
    return envelope.data;
  }

  async login(account, env = process.env) {
    if (account.tokenEnv && env[account.tokenEnv]) {
      this.token = env[account.tokenEnv];
      return;
    }
    if (!env[account.emailEnv] || !env[account.passwordEnv])
      throw new Error(`缺少账号 ${account.id} 的凭据环境变量`);
    const login = await this.request("/user/login/email", {
      e_mail: env[account.emailEnv],
      pass: env[account.passwordEnv],
    });
    if (!login?.access_token || login.mail_status === 2)
      throw new Error(`账号 ${account.id} 需要先完成邮箱验证`);
    this.token = login.access_token;
  }

  async upload(data, filename, type) {
    const form = new FormData();
    form.append("source", "post");
    form.append("file", new Blob([data], { type }), filename);
    const url = await this.request("/file", form, { form: true });
    if (typeof url !== "string") throw new Error("上传接口没有返回图片地址");
    const absolute = new URL(url, this.url).href;
    if (!httpURL(absolute)) throw new Error("上传接口返回了无效图片地址");
    return absolute;
  }

  async draft(chatId, prompt, images = []) {
    const response = await this.request(
      "/purchase/ai/chat",
      {
        chat_id: chatId,
        parent_id: "",
        message: { text: prompt, images },
      },
      { stream: true },
    );
    return readDraft(response.body);
  }
}

/** Accept LF/CRLF, multi-line data, split UTF-8, heartbeats; require a clean end. */
export async function readDraft(body) {
  if (!body) throw new Error("AI 事件流为空");
  const decoder = new TextDecoder();
  let buffer = "";
  let draft;
  let done = false;
  const consume = (frame) => {
    const data = frame
      .split("\n")
      .filter((line) => line.startsWith("data:"))
      .map((line) => line.slice(5).replace(/^ /, ""))
      .join("\n");
    if (!data) return;
    if (data === "[DONE]") {
      done = true;
      return;
    }
    const event = JSON.parse(data);
    if (event.type === "error")
      throw new Error("AI 对话失败，请在对应会话中查看原因");
    if (event.type === "tool_suspend" && event.name === "draft_post")
      draft = event.input;
    if (event.type === "done") done = true;
  };
  for await (const bytes of body) {
    buffer += decoder.decode(bytes, { stream: true });
    // Keep a trailing CR until the next chunk in case CRLF straddles chunks.
    buffer = buffer.replace(/\r\n/g, "\n");
    let end;
    while ((end = buffer.indexOf("\n\n")) >= 0) {
      consume(buffer.slice(0, end));
      buffer = buffer.slice(end + 2);
    }
    if (buffer.length > 2_000_000) throw new Error("AI 单个事件过大");
  }
  buffer += decoder.decode();
  if (buffer.trim()) consume(buffer);
  if (!done) throw new Error("AI 连接提前中断，没有发布");
  if (!draft) throw new Error("AI 没有给出发帖草稿，没有发布");
  return draft;
}

export function toPost(draft, topic, fallbackImage) {
  if (
    !draft ||
    typeof draft.draftId !== "string" ||
    !draft.draftId ||
    typeof draft.title !== "string" ||
    typeof draft.content !== "string" ||
    [...draft.title.trim()].length < 6 ||
    [...draft.title.trim()].length > 150 ||
    [...draft.content.trim()].length < 20 ||
    [...draft.content].length > 3000
  )
    throw new Error("草稿的标题或正文无效，没有发布");
  const images = draft.images ?? [];
  if (!Array.isArray(images) || images.length > 5 || !images.every(httpURL))
    throw new Error("草稿图片无效，没有发布");
  if (
    draft.price !== undefined &&
    (typeof draft.price !== "number" ||
      !Number.isFinite(draft.price) ||
      draft.price < 0)
  )
    throw new Error("草稿价格无效，没有发布");
  const result = {
    title: draft.title.trim(),
    content: draft.content.trim(),
    tags: [
      {
        slug_name: topic.category,
        display_name: topic.category,
        original_text: "",
      },
    ],
    purchase: {
      images: images.length ? images : fallbackImage ? [fallbackImage] : [],
      price: draft.price ?? 0,
      currency: "CNY",
      link: "",
    },
  };
  // The bot does not add unverified product links, initial votes, or discussion counts.
  return result;
}

async function saveState(file, state) {
  const temp = `${file}.${process.pid}.tmp`;
  await writeFile(temp, JSON.stringify(state, null, 2) + "\n", { mode: 0o600 });
  await rename(temp, file);
}

async function loadState(file, baseUrl) {
  try {
    const state = JSON.parse(await readFile(file, "utf8"));
    if (
      state.version !== 1 ||
      state.baseUrl !== baseUrl ||
      !state.jobs ||
      typeof state.jobs !== "object" ||
      Array.isArray(state.jobs)
    )
      throw new Error("状态文件格式或 baseUrl 不匹配；请使用单独的 stateFile");
    return state;
  } catch (error) {
    if (error.code !== "ENOENT") throw error;
    return { version: 1, baseUrl, jobs: {} };
  }
}

export async function runBatch(
  config,
  {
    publish = false,
    env = process.env,
    now = () => new Date(),
    log = console.log,
    apiFactory = (c) => new CommunityAPI(c),
    wait = sleep,
  } = {},
) {
  const { day } = localClock(config.timeZone, now());
  await mkdir(dirname(config.stateFile), { recursive: true });
  const lockPath = `${config.stateFile}.lock`;
  const lock = await open(lockPath, "wx", 0o600).catch((error) => {
    if (error.code === "EEXIST")
      throw new Error(
        "另一个发帖任务正在运行；若上次异常退出，请确认进程已停止后删除 .lock 文件",
      );
    throw error;
  });
  let failures = 0;
  try {
    await lock.writeFile(String(process.pid));
    const state = await loadState(config.stateFile, config.baseUrl);
    const accounts = ranked(config.accounts, day).slice(0, config.postsPerDay);
    const usedTopics = new Set();
    const previewTitles = [];
    for (const [index, account] of accounts.entries()) {
      if (localClock(config.timeZone, now()).day !== day) break;
      const key = `${day}/${account.id}`;
      const existing = state.jobs[key];
      if (
        publish &&
        existing &&
        (terminal.has(existing.status) || existing.attempts >= 2)
      ) {
        log(
          `[跳过] ${account.nickname}: ${existing.status}（${existing.questionId ?? key}）`,
        );
        continue;
      }
      const committed = Object.values(state.jobs).filter(
        (job) =>
          job.day === day && ["publishing", "published"].includes(job.status),
      ).length;
      if (publish && committed >= config.postsPerDay) break;
      const topic = chooseTopic(
        {
          ...config,
          topics: config.topics.filter((t) => !usedTopics.has(t.id)),
        },
        account,
        day,
        state.jobs,
      );
      if (!topic) {
        log(`[跳过] ${account.nickname}: 暂无未冷却的话题`);
        continue;
      }
      usedTopics.add(topic.id);
      if (index > 0) {
        const seconds = randomInt(
          config.minIntervalSeconds,
          config.maxIntervalSeconds + 1,
        );
        log(`[等待] ${seconds} 秒后处理 ${account.nickname}`);
        await wait(seconds * 1000);
      }
      // Runtime clock is checked after the delay too, so a restart or midnight does not over-post.
      if (localClock(config.timeZone, now()).day !== day) break;
      const job = {
        day,
        accountId: account.id,
        topicId: topic.id,
        status: "generating",
        attempts: (existing?.attempts ?? 0) + 1,
      };
      if (publish) {
        state.jobs[key] = job;
        await saveState(config.stateFile, state);
      }
      const api = apiFactory(config);
      try {
        await api.login(account, env);
        const chat = await api.request("/purchase/chat", {
          title: topic.request.slice(0, 60),
        });
        if (!chat?.id) throw new Error("创建会话失败");
        job.chatId = chat.id;
        if (publish) await saveState(config.stateFile, state);
        const titles = [
          ...Object.values(state.jobs)
            .filter((j) => terminal.has(j.status) && j.title)
            .map((j) => j.title),
          ...previewTitles,
        ];
        const draft = await api.draft(
          chat.id,
          buildPrompt(account, topic, day, titles),
          topic.images ?? [],
        );
        const post = toPost(draft, topic);
        job.title = post.title;
        if (titles.some((title) => similarTitle(post.title, title))) {
          job.status = "skipped";
          log(`[跳过重复] ${account.nickname}: ${post.title}`);
          if (publish) await saveState(config.stateFile, state);
          continue;
        }
        if (!publish) {
          previewTitles.push(post.title);
          const preview = resolve(
            dirname(config.stateFile),
            `preview-${day}-${account.id}.json`,
          );
          await writeFile(
            preview,
            JSON.stringify(
              {
                account: account.nickname,
                topic: topic.id,
                chatId: chat.id,
                draft,
                post,
              },
              null,
              2,
            ) + "\n",
            { mode: 0o600 },
          );
          log(
            `[预览] ${account.nickname}: ${post.title}\n${post.content}\n草稿保存：${preview}`,
          );
          continue;
        }
        if (!post.purchase.images.length) {
          const data = topic.coverFile
            ? await readFile(topic.coverFile)
            : makeCover(`${topic.id}/${day}`);
          const ext = topic.coverFile?.split(".").pop()?.toLowerCase() ?? "png";
          const type = {
            png: "image/png",
            jpg: "image/jpeg",
            jpeg: "image/jpeg",
            webp: "image/webp",
          }[ext];
          if (!type) throw new Error("coverFile 只支持 png/jpg/jpeg/webp");
          post.purchase.images = [await api.upload(data, `cover.${ext}`, type)];
        }
        if (localClock(config.timeZone, now()).day !== day) {
          job.status = "skipped";
          await saveState(config.stateFile, state);
          log(
            `[跳过] ${account.nickname}: 生成草稿时跨过午夜，留待新一天的任务`,
          );
          break;
        }
        // Journal before POST: if the response is lost we never blindly publish again.
        job.status = "publishing";
        await saveState(config.stateFile, state);
        const result = await api.request("/question", post);
        if (!result?.id)
          throw new Error("发帖结果缺少 ID，请人工核对，任务不会自动重发");
        job.questionId = result.id;
        job.status = "published";
        await saveState(config.stateFile, state);
        log(
          `[已发布] ${account.nickname}: ${post.title} → ${config.baseUrl}/questions/${result.id}`,
        );
        await api
          .request("/purchase/ai/draft/result", {
            draft_id: draft.draftId,
            status: "published",
            question_id: result.id,
            title: post.title,
          })
          .catch(() =>
            log(`[提示] 帖子 ${result.id} 已发布，会话草稿状态回写失败`),
          );
      } catch (error) {
        failures++;
        const rejected =
          error instanceof APIError &&
          error.status < 500 &&
          error.code >= 400 &&
          error.code < 500 &&
          error.code !== 408;
        if (
          job.status !== "published" &&
          (job.status !== "publishing" || rejected)
        )
          job.status = "failed";
        if (publish) await saveState(config.stateFile, state);
        log(
          `[失败] ${account.nickname}: ${error.message}${job.status === "publishing" ? "；发帖结果待核对，不会自动重发" : ""}`,
        );
      }
    }
  } finally {
    await lock.close();
    await unlink(lockPath);
  }
  return { failures };
}

async function initialize(config) {
  let failures = 0;
  for (const account of config.accounts) {
    const email = process.env[account.emailEnv];
    const pass = process.env[account.passwordEnv];
    if (!email || !pass)
      throw new Error(
        `注册 ${account.id} 需要 emailEnv/passwordEnv 对应的环境变量`,
      );
    try {
      const api = new CommunityAPI(config);
      const result = await api.request("/user/register/email", {
        name: account.nickname,
        e_mail: email,
        pass,
      });
      console.log(
        `[已注册] ${account.nickname}${result?.mail_status === 2 ? "：需要完成邮箱验证" : ""}`,
      );
    } catch (error) {
      failures++;
      console.error(`[注册失败] ${account.nickname}: ${error.message}`);
    }
  }
  return { failures };
}

async function main(args) {
  if (args.includes("--help")) {
    console.log(`社区每日发帖（Node.js 22+，无需安装依赖）
node script/community/community.mjs --config <配置路径> [--publish] [--daemon]
默认：生成草稿预览，不发帖。--publish：发布；--daemon：按 timeZone/dailyAt 每天运行。
--init-accounts：仅用普通注册接口创建配置中的账号，不发帖；已有账号无需此步骤。
凭据来自环境变量；可使用 node --env-file=<私有环境文件> 加载。`);
    return;
  }
  const known = new Set([
    "--config",
    "--publish",
    "--daemon",
    "--init-accounts",
  ]);
  for (let i = 0; i < args.length; i++) {
    if (!known.has(args[i])) throw new Error(`未知参数 ${args[i]}`);
    if (args[i] === "--config" && (!args[++i] || args[i].startsWith("--")))
      throw new Error("--config 后需要配置文件路径");
  }
  if (
    args.includes("--init-accounts") &&
    (args.includes("--publish") || args.includes("--daemon"))
  )
    throw new Error("注册账号请单独执行");
  const configPath = resolve(
    args.includes("--config")
      ? args[args.indexOf("--config") + 1]
      : resolve(directory, "community.local.json"),
  );
  const config = validateConfig(
    JSON.parse((await readFile(configPath, "utf8")).replace(/^\uFEFF/, "")),
    dirname(configPath),
  );
  if (args.includes("--init-accounts")) {
    process.exitCode = (await initialize(config)).failures ? 1 : 0;
    return;
  }
  const publish = args.includes("--publish");
  const run = () => runBatch(config, { publish });
  if (!args.includes("--daemon")) {
    process.exitCode = (await run()).failures ? 1 : 0;
    return;
  }
  console.log(
    `[定时任务] ${config.timeZone} 每天 ${config.dailyAt}，最多 ${config.postsPerDay} 帖，${publish ? "发布模式" : "预览模式"}`,
  );
  let lastDay;
  while (true) {
    const { day, time } = localClock(config.timeZone);
    if (day !== lastDay && time >= config.dailyAt) {
      // Failed days are not repeatedly retried in a tight loop; a manual invocation can retry.
      lastDay = day;
      await run().catch((error) =>
        console.error(`[任务失败] ${error.message}`),
      );
    }
    await sleep(30_000);
  }
}

if (
  process.argv[1] &&
  pathToFileURL(resolve(process.argv[1])).href === import.meta.url
) {
  main(process.argv.slice(2)).catch((error) => {
    console.error(error.message);
    process.exitCode = 1;
  });
}
