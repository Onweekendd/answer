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

import { getStatus, loadConfig, saveConfig, type Status } from "./storage";

// 点插件图标弹出的小窗口。它是另一个进程，每次打开都重新加载，关掉就销毁。
// 它不直接碰 WebSocket：只读状态、写配置，background 监听到配置变化会自己重连。

const $ = <T extends HTMLElement>(id: string) => document.getElementById(id) as T;

const stateText: Record<Status["state"], string> = {
  connecting: "正在连接",
  open: "已连接",
  closed: "未连接"
};

function render(status: Status | undefined): void {
  const state = status?.state ?? "closed";
  $("dot").dataset.state = state;
  $("state").textContent = stateText[state];
  $("detail").textContent = status?.detail ?? "";
}

async function init(): Promise<void> {
  const config = await loadConfig();
  $<HTMLInputElement>("hubUrl").value = config.hubUrl;
  $<HTMLInputElement>("token").value = config.token;
  render(await getStatus());

  // background 写状态时，这里跟着刷新
  chrome.storage.onChanged.addListener((changes, area) => {
    if (area === "session" && changes.status) {
      render(changes.status.newValue as Status | undefined);
    }
  });

  $<HTMLFormElement>("form").addEventListener("submit", (e) => {
    e.preventDefault();
    void saveConfig({
      hubUrl: $<HTMLInputElement>("hubUrl").value.trim(),
      token: $<HTMLInputElement>("token").value.trim()
    });
  });
}

void init();
