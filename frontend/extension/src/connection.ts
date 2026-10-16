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

import type { OutgoingMessage } from "./protocol";
import { loadConfig, setStatus } from "./storage";

// 和 Go Hub 之间的那条 WebSocket。只管"连上、保持、断了重连、收发文本"，
// 不关心消息是什么意思，收到的内容交给 onMessage。

const KEEPALIVE_MS = 20_000; // 要小于 service worker 的 30 秒空闲上限
const RETRY_BASE_MS = 1_000;
const RETRY_MAX_MS = 30_000;

interface Handlers {
  onOpen(): void; // 连上以后要做的事（发 hello）
  onMessage(text: string): void;
}

export class HubConnection {
  private ws: WebSocket | undefined;
  private opening: Promise<void> | undefined;
  private attempt = 0; // 连续失败了几次，决定下次等多久
  private retryTimer: ReturnType<typeof setTimeout> | undefined;

  constructor(private readonly handlers: Handlers) {}

  // 可以随便多次调用：已经连着或正在连，就什么也不做（幂等）。
  // JS 虽然是单线程，但 open() 中间有 await：顶层和闹钟同时调用时，
  // 两次都可能在 await 期间通过检查，各建一条连接。所以正在打开时直接复用同一个 Promise。
  connect(): Promise<void> {
    this.opening ??= this.open().finally(() => {
      this.opening = undefined;
    });
    return this.opening;
  }

  private async open(): Promise<void> {
    if (this.ws && this.ws.readyState <= WebSocket.OPEN) {
      return; // CONNECTING(0) 或 OPEN(1)
    }
    clearTimeout(this.retryTimer);

    const { hubUrl, token } = await loadConfig();
    if (!token) {
      await setStatus("closed", "还没有设置 token，点插件图标填写");
      return; // 不重连：没有 token 连了也会被拒，等用户填好后由 storage 变化触发
    }

    await setStatus("connecting", hubUrl);
    const ws = new WebSocket(hubUrl);
    this.ws = ws;
    // 定时器跟着这一条连接走，旧连接关闭时不会误清新连接的定时器
    let keepalive: ReturnType<typeof setInterval> | undefined;

    ws.onopen = () => {
      this.attempt = 0;
      void setStatus("open", hubUrl);
      keepalive = setInterval(() => this.send({ type: "event", event: "ping" }), KEEPALIVE_MS);
      this.handlers.onOpen();
    };

    ws.onmessage = (e) => {
      if (typeof e.data === "string") {
        this.handlers.onMessage(e.data);
      }
    };

    // 连不上时浏览器会先触发 error 再触发 close，所以只在 close 里处理
    ws.onclose = (e) => {
      clearInterval(keepalive);
      if (this.ws !== ws) {
        return; // 是被 reconnect() 主动换掉的旧连接
      }
      this.ws = undefined;
      this.scheduleReconnect(`连接断开（code ${e.code}）`);
    };
  }

  // 配置变了以后用：关掉旧连接，立刻用新配置重连
  async reconnect(): Promise<void> {
    await this.opening; // 等正在进行的打开结束，不然它会用旧配置建出一条连接
    const old = this.ws;
    this.ws = undefined;
    old?.close();
    this.attempt = 0;
    await this.connect();
  }

  // 没连上时直接丢弃，返回 false。命令的结果丢了没关系：Go 那边会超时
  send(msg: OutgoingMessage): boolean {
    if (this.ws?.readyState !== WebSocket.OPEN) {
      return false;
    }
    this.ws.send(JSON.stringify(msg));
    return true;
  }

  // 指数退避：1s、2s、4s……最多 30s，再加一点随机，避免很多客户端同时重连
  private scheduleReconnect(reason: string): void {
    const delay = Math.min(RETRY_BASE_MS * 2 ** this.attempt, RETRY_MAX_MS) + Math.random() * 500;
    this.attempt++;
    void setStatus("closed", `${reason}，${Math.round(delay / 1000)} 秒后重连`);
    this.retryTimer = setTimeout(() => void this.connect(), delay);
  }
}
