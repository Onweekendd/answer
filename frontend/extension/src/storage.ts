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

// background 和 popup 是两个进程，不共享变量。它们通过 chrome.storage 交换数据：
//   local：配置（Hub 地址、token），浏览器重启后还在
//   session：连接状态，浏览器关掉就清空

export interface Config {
  hubUrl: string;
  token: string;
}

const defaultConfig: Config = {
  // 设计文档 5.4：Hub 只监听本机；连服务器上的 Hub 时用 ssh -L 隧道，这里的地址不用改
  hubUrl: "ws://127.0.0.1:8765/agent",
  token: ""
};

export async function loadConfig(): Promise<Config> {
  // 传入默认值对象：没存过的键会用默认值补上
  return (await chrome.storage.local.get(defaultConfig)) as Config;
}

export async function saveConfig(patch: Partial<Config>): Promise<void> {
  await chrome.storage.local.set(patch);
}

export type ConnState = "connecting" | "open" | "closed";

export interface Status {
  state: ConnState;
  detail: string;
  at: number; // Date.now()
}

export async function setStatus(state: ConnState, detail = ""): Promise<void> {
  const status: Status = { state, detail, at: Date.now() };
  await chrome.storage.session.set({ status });
}

export async function getStatus(): Promise<Status | undefined> {
  const { status } = await chrome.storage.session.get("status");
  return status as Status | undefined;
}
