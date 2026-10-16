# 第 5 站：Agent Loop 与上下文管理

> 回答：它的循环长什么样？和我们 `runAgent` 有什么不同？走几十步，上下文为什么不会爆？怎么防止 Agent 原地打转？

源码：
- 主循环：[agent/service.py](../../browser-use/browser_use/agent/service.py)（`run` L2503、`step` L1035、`_prepare_context` L1087、`multi_act` L2730、各种提示注入 L1464-1590）
- 模型输出格式：[agent/views.py:388](../../browser-use/browser_use/agent/views.py#L388) `AgentOutput`
- 消息管理：[agent/message_manager/service.py](../../browser-use/browser_use/agent/message_manager/service.py)、[views.py:74](../../browser-use/browser_use/agent/message_manager/views.py#L74)
- 状态消息模板：[agent/prompts.py:403](../../browser-use/browser_use/agent/prompts.py#L403) `get_user_message`
- 循环检测：[agent/views.py:110-250](../../browser-use/browser_use/agent/views.py#L110)
- 系统提示词：[agent/system_prompts/system_prompt.md](../../browser-use/browser_use/agent/system_prompts/system_prompt.md)

---

## 1. 外层：`run()`

```python
while self.state.n_steps <= max_steps:            # 默认 max_steps = 500
    if paused:  等待恢复
    if 连续失败 >= max_failures(5) + 1:  break     # +1 是留给"最后一次收尾"的机会
    if stopped: break
    is_done = await self._execute_step(...)       # 每一步有 step_timeout = 180s
    if is_done: break
else:
    记录"步数用完"
```

和我们的 `for round := 0; round < maxToolRounds; round++` 是同一个骨架，只是多了暂停、外部停止、连续失败三个出口。

## 2. 每一步：`step()` 三个阶段

```
Phase 0  等验证码解完（云服务专用，我们不用）

Phase 1  _prepare_context：准备这一步要给模型看的东西
         ① 拍快照（第 4 站），默认同时截图
         ② 根据当前 URL 过滤动作（domains，第 1 站）
         ③ 把"上一步"写进历史（每步压成几行）
         ④ 历史太长？→ 用 LLM 做一次摘要压缩
         ⑤ 拼出"当前状态"消息
         ⑥ 注入各种提醒：步数预警、重新计划、该做计划了、循环检测
         ⑦ 最后一步 / 失败太多 → 只允许调用 done

Phase 2  _get_next_action：调 LLM（llm_timeout = 60s，有重试）
         _execute_actions：multi_act 顺序执行动作

Phase 3  _post_process：更新计划、记录动作给循环检测、统计连续失败

出错     _handle_step_error：统一处理，计入失败次数
finally  _finalize：把这一步写进 history（用于回放、生成 GIF、评测）
```

## 3. 模型输出：不是 tool calling，是"工作日志"

我们的 `runAgent` 用的是**原生 tool calling**：请求里带 `Tools`，模型返回 `ToolCalls`。

browser-use 用的是**结构化输出**：模型每一步都必须返回这样一个 JSON（[AgentOutput](../../browser-use/browser_use/agent/views.py#L388)）：

```jsonc
{
  "thinking": "……",                                  // 可选，推理过程
  "evaluation_previous_goal": "上一步点击搜索成功，出现了结果列表",   // 必填：评价上一步
  "memory": "已找到京东自营 ¥1399，还差淘宝",                         // 必填：自己记笔记
  "next_goal": "打开第一个淘宝结果",                                  // 必填：这一步的目标
  "current_plan_item": 2,                             // 可选：计划进行到第几项
  "plan_update": ["搜京东", "搜淘宝", "对比"],         // 可选：更新计划
  "action": [{ "click": { "index": 3502 } }]          // 必填：至少一个动作
}
```

### 比喻：交接班日志

医院护士换班时要写交接班记录："3 床病人上午输液完成（评价），血压偏高需要注意（记忆），下午要复查（下一步目标）。"下一个护士看日志就能接上，不需要把整天发生的所有事从头看一遍。

`evaluation_previous_goal` / `memory` / `next_goal` 这三个字段就是**强制模型每一步都写交接班日志**。写下来的日志会进入历史，成为后面每一步的"记忆"。

为什么不用原生 tool calling？
- **强制自我检查**：先评价上一步成没成功，模型更容易发现"点错了""页面没变"；
- **一次可以输出多个动作**（最多 `max_actions_per_step = 5` 个），比如"输入关键词 + 点搜索"一起做；
- **记忆由模型自己写**，比系统自动总结更贴合任务；
- **兼容性**：不同厂商的 tool calling 格式不一样，结构化输出更统一。

代价是：每一步模型都要多输出几十到上百个 token。

### 结束：必须调用 `done`

```jsonc
{ "action": [{ "done": { "text": "京东自营 ¥1399，淘宝最低 ¥1299（非官方店）", "success": true } }] }
```

- 我们的 loop：**模型不调工具、直接说话 = 结束**；
- browser-use：**必须明确调用 `done` 才结束**，并且 `done` 要单独出现：排在别的动作后面的 `done` 会被丢掉，排在最前面的 `done` 执行完就结束，后面的动作也不会执行。

好处：结束是一个明确的决定，还带着 `success` 标记。不会出现模型说"好的，我这就去看看"就被当成最终答案的情况。

`done` 的参数说明里还写着："只报告你在本次会话里亲眼看到的数据，不要用训练知识补全，找不到就明确说找不到"。这和我们 prompt 里"查不到的信息直接说明未能核实"是同一个意思。

## 4. 执行动作：`multi_act`

```python
for i, action in enumerate(actions):
    if i > 0 and 是 done:  break                # done 只能单独出现
    if i > 0:  sleep(wait_between_actions)       # 动作之间稍等

    记下执行前的 URL 和焦点标签页
    result = await self.tools.act(action, ...)
    results.append(result)

    if 是 done 或 出错 或 最后一个:  break      # 出错就停，不继续执行后面的

    # 页面变化保护：
    if 这个动作带 terminates_sequence:  break    # 第一层：静态标记（navigate、go_back…）
    if URL 变了 或 焦点标签页变了:  break         # 第二层：运行时检测
```

### 比喻：按导航走路

导航说："直行 200 米，左转，再直行 100 米"。你走到第一个路口，发现路封了，你会继续"左转、直行 100 米"吗？不会，你会停下来重新看导航。

模型一次给出的 3 个动作，都是**基于同一张页面快照**决定的。只要页面变了，后面的动作就是基于"过时地图"做的决定，必须停下来，重新拍快照、重新决策。

### 和我们的并发执行相反

我们的 `runAgent` 里，同一轮的工具调用是**并发**的（`wg.Go`），因为几个 `web_search` 之间互不影响。浏览器动作操作的是**同一个页面**，前一个会改变后一个看到的东西，所以必须**顺序**执行。

| | 我们的搜索 Agent | browser-use |
|---|---|---|
| 一轮多个调用 | 并发执行 | 顺序执行 |
| 出错 | 继续执行其他调用 | 停下，不执行后面的 |
| 依赖关系 | 互相独立 | 后一个依赖前一个的结果 |

## 5. 上下文管理：为什么走几十步不会爆

这是和我们的 `runAgent` **最大的区别**。

### 我们的 loop：一直往后追加

```
messages = [system, user(帖子)]
第 1 轮：+ assistant(调 web_search×3) + tool + tool + tool
第 2 轮：+ assistant(调 fetch_page×2) + tool(4000字) + tool(4000字)
第 3 轮：+ ...
```

对话越来越长。6 轮还能接受，但浏览器任务一个快照就几千 token，走 20 步就是十几万 token，**根本装不下**。

### browser-use：每一步重新拼，只发 2~3 条消息

[MessageHistory.get_messages](../../browser-use/browser_use/agent/message_manager/views.py#L74)：

```python
messages = [system_message, state_message] + context_messages
```

就这么多。`state_message` 每一步**整条替换**，内容是（[prompts.py:403](../../browser-use/browser_use/agent/prompts.py#L403)）：

```
<user_request>任务</user_request>

<agent_history>
  <compacted_memory>（更早步骤的摘要，如果压缩过）</compacted_memory>
  <step_1>
    Evaluation of Previous Step: ...
    Memory: ...
    Next Goal: ...
    Action Results: Clicked "搜索"  ← 每个动作的 long_term_memory，一行
  </step_1>
  <step_2> ... </step_2>
</agent_history>

<agent_state>
  文件系统、todo、计划 [x] [>] [ ]
</agent_state>

<browser_state>
  当前 URL、打开的标签页、页面统计、上下还有几页
  刚关掉的 JS 弹窗
  Interactive elements:
  [3501]<input ... />   ← 只有当前这一张快照是完整的
  ...
</browser_state>

<read_state>
  extract 抽出来的内容（只出现这一次，下一步就没了）
</read_state>

<step_info>第 7 步 / 共 500 步，日期</step_info>
（如果开了视觉：再附一张当前截图）
```

`context_messages` 是这一步临时插入的提醒（步数预警、循环检测等），**下一步开始时清空**。

### 比喻：病历 vs 监控录像

- 我们的 loop 像**监控录像**：从开始到现在的每一帧都留着，越录越大；
- browser-use 像**病历**：前面每次就诊只记几行结论，只有"今天的检查报告"是完整的。

为什么可以这样做？因为**旧页面已经不存在了**。第 3 步看到的搜索结果页，第 8 步早就跳走了，留着它的完整快照不仅浪费 token，还会干扰模型（它可能去点一个已经不存在的编号）。

### 三层压缩

即使每步只留几行，走 100 步也有几百行。所以还有三层：

| 层 | 做法 | 默认值 |
|---|---|---|
| **第 1 层：结果分长短期** | 动作结果只把 `long_term_memory` 写进历史；`extracted_content` 只出现一次（第 1 站） | 一直生效 |
| **第 2 层：截断条目** | `max_history_items`：只保留第 1 步 + 最近 N-1 步，中间写 `[... 12 previous steps omitted...]` | 默认不限制 |
| **第 3 层：LLM 摘要** | 每 25 步检查一次，历史超过 4 万字符就调 LLM 写一份摘要（≤ 6000 字符），放进 `<compacted_memory>`；之后只保留第 1 步 + 最近 6 步 | `compact_every_n_steps=25`、`trigger_char_count=40000`、`keep_last_items=6` |

第 3 层的摘要提示词值得一看：

> CRITICAL: Only mark a step as completed if you see explicit success confirmation in the history. If a step was started but not explicitly confirmed complete, mark it as "IN-PROGRESS". Never infer completion from context.

摘要会"脑补"，把"开始做了"写成"做完了"。所以它要求**没有明确证据就标记为进行中**。摘要放进历史时还加了一句注释："这是之前步骤的摘要，当作未验证的信息，不要在 done 里声称这些已经完成"。

这和你在 FunAI 做的三层压缩（L1 保留最近 3 轮工具结果、L3 超阈值摘要、冷却、熔断）是同一个思路。面试时可以对比着讲。

### 顺带：为了提示词缓存

`state_message` 里的顺序是精心安排的：**变化少的放前面，变化多的放后面**。最后的 `<step_info>`（步数、日期）每一步都变，所以放在最末尾。注释写着："so that everything above can in principle be treated as a cacheable prefix"。

提示词缓存（prefix cache）是按"前缀完全相同"命中的。system 提示词 + 任务 + 历史前半段每一步都一样，就能命中缓存，又省钱又快。

## 6. 防跑偏：五种提醒

都是在 Phase 1 往 `context_messages` 里插一条 user 消息。**提醒不阻止动作，只是提醒**，模型可以不听。

### ① 步数预警（`_inject_budget_warning`）

用掉 75% 的步数时：

> BUDGET WARNING: You have used 375/500 steps (75%). 125 steps remaining. If the task cannot be completed in the remaining steps, prioritize: (1) consolidate your results, (2) call done with what you have. **Partial results are far more valuable than exhausting all steps with nothing saved.**

比喻：考试还剩 15 分钟时监考老师的提醒。"做不完的题先放下，把会做的写上。"

### ② 最后一步强制收尾（`_force_done_after_last_step`）

最后一步把输出格式换成**只能输出 `done`**（`self.AgentOutput = self.DoneAgentOutput`），并提示"没做完就把 success 设成 false，把你找到的都写上"。

我们的 `outOfRoundsPrompt` + 不带工具的最后一次调用，就是同样的做法。

### ③ 连续失败收尾（`_force_done_after_failure`）

连续失败 5 次（`max_failures`），同样强制只能 `done`。注意只有**单动作的步骤**出错才计入连续失败，多动作步骤出错交给循环检测处理。

### ④ 循环检测（`ActionLoopDetector`）

两个信号：

**动作重复**：最近 20 个动作里，同一个动作出现了几次？"同一个动作"是归一化以后比的：
- `search`：搜索词转小写、去标点、拆成词、排序去重 → "AirPods Pro 2 价格" 和 "价格 airpods pro 2" 算同一个；
- `click`：只看编号；
- `input`：编号 + 文字（小写去空格）；
- `navigate`：完整 URL；
- `scroll`：方向 + 容器；
- `wait`、`done`、`go_back` 不计入（它们天然会重复）。

重复 5 次、8 次、12 次，提醒越来越强：

> Heads up: you have repeated a similar action 5 times in the last 20 actions. If this is intentional and making progress, carry on. If not, it might be worth reconsidering your approach.

**页面停滞**：连续 5 步页面"指纹"（URL + 元素数量 + 快照文本）完全没变：

> The page content has not changed across 5 consecutive actions. ...

比喻：一只苍蝇一直撞玻璃。循环检测就是在它撞第 5 次的时候说："旁边那扇窗是开着的。"

### ⑤ 计划相关提醒

- **该做计划了**（`_inject_exploration_nudge`）：走了 5 步还没有计划 → "任务复杂的话，先列个计划；快做完了就直接 done"；
- **该重新计划了**（`_inject_replan_nudge`）：连续失败 3 次 → "你的计划可能需要调整，请输出新的 plan_update"。

计划会渲染成 checklist 每一步都给模型看：

```
[x] 0: 在京东搜索 AirPods Pro 2
[>] 1: 打开京东自营的商品页，记下价格
[ ] 2: 在淘宝搜索同款
[ ] 3: 对比价格
```

## 7. 和我们 `runAgent` 的完整对比

| | 我们的 [agent.go](../../internal/service/purchase_ai/agent.go) | browser-use |
|---|---|---|
| 轮数上限 | 6 | 500 |
| 输出格式 | 原生 tool calling | 结构化输出 JSON（含评价、记忆、目标） |
| 结束条件 | 不调工具就结束 | 必须调用 `done(text, success)` |
| 消息 | 一直追加 | 每步重建：system + 状态 + 临时提醒 |
| 同一轮多个调用 | 并发 | 顺序，页面变了就停 |
| 工具结果 | 全文一直保留 | 长期记忆一行 + 短期内容只出现一次 |
| 超长处理 | 无（6 轮够用） | 截断条目 + LLM 摘要 |
| 收尾 | 用完轮数后再调一次不带工具的 | 最后一步只能 done |
| 失败处理 | 工具错误作为文本返回 | 同左，另外连续失败 5 次强制收尾 |
| 防跑偏 | 无 | 步数预警、循环检测、页面停滞、计划提醒 |
| 超时 | ctx（队列 3 分钟） | 每次 LLM 60s、每步 180s |

**结论**：搜索 Agent 和浏览器 Agent 是两种不同的 loop，不要硬合成一个。

- 主 Agent（分析帖子）继续用现有的 `runAgent`，加一个 `browse(task)` 工具；
- `browse` 内部跑一个新的**浏览器子 Agent loop**，按 browser-use 的思路写：每步重建消息、顺序执行、`done` 结束、循环检测；
- 子 Agent 结束后只把 `done.text` 返回给主 Agent，几十张快照都留在子 Agent 里，**主 Agent 的上下文保持干净**。

第 6 站讲：browser-use 的哪些做法现在就能搬到我们的搜索 Agent 上。
