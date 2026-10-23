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

package agent

// 这个文件是整个重构的骨架，由你来写。下面每个 TODO 都写了：
// 钩子在循环的哪个位置被调用、现在这件事在哪段代码里、插件会拿到什么、要返回什么、有什么要注意。
// 照 pi 的 ExtensionAPI 来（pi/packages/coding-agent/src/core/extensions/types.ts 第 1592 行起），
// 但只取下面这 6 个钩子，不要 pi 那 40 个。
//
// 写完这个文件和 run.go，告诉我：我来把现有功能一个个改写成插件，并把 agent_test.go 里的 Test_run 搬过来当安全网。

// Plugin 是一个插件：一个函数，核心把注册器交给它，它想要什么就自己注册。
// 状态放在函数的闭包里（见 doc.go）
type Plugin func(*Registry)

// Registry 是核心交给插件的注册器。核心保存所有插件注册的东西，循环在固定的时机取出来调用。
//
// TODO(你) 把下面 6 个方法写出来。每个方法的签名就是一个钩子：参数是核心在那个时机给插件的，
// 返回值是插件能反馈给核心的。
type Registry struct {
	// TODO(你) 这里放注册进来的东西：工具、各个钩子的函数列表。
	// 顺序要紧：同一个钩子上，先注册的先被调用；工具的顺序就是发给模型的顺序。
}

// ───────────────────────────────────────────────────────────────────────────
// 钩子 1：Tool —— 收集工具
//
// 时机：任务开始时，核心遍历所有插件，把它们注册的工具按注册顺序收集成「名字 → 工具」和「发给模型的定义列表」。
// 现在的代码：tools.go 的 tools()，toolEntry、toolNeed、fromSpec、needBrowser 这些。
// 参数：tool.Spec（tool/tool.go，Name、Description、Args、Run 已经有了，不用改）。
// 要注意：
//   - 不给模型的工具怎么办？现在是「插件没连上就不给浏览器工具」。新的做法是入口不装浏览器插件，
//     所以不需要 toolNeed 枚举了。pi 还有 defaultActive: false，我们暂时不要。
//   - 工具重名要不要报错？建议报错：两个插件注册了同一个名字肯定是装配错了。
//   - done 不是插件的工具，是核心自己的（见 run.go 的 TODO）。
//
// func (r *Registry) Tool(spec tool.Spec)
// ───────────────────────────────────────────────────────────────────────────

// ───────────────────────────────────────────────────────────────────────────
// 钩子 2：AfterTool —— 一个工具执行完
//
// 时机：runCalls 里每个工具执行完、结果已经拿到、还没交给模型之前，每个调用一次（跳过的不调）。
// 现在的代码：tools.go 的 runTool 里顺手调的 seeLink（链接核对要的），和循环里 `if results[i].Snapshot {
// snapshotAt = append(...) }`（记下哪条是快照）。
// 参数：这次调用（id、name、arguments）和结果 *tool.Result。
// 返回值：没有。插件可以改 *tool.Result（pi 的 afterToolCall 是这样），比如链接核对不改，记录类的只读。
// 谁用：浏览器插件（记下快照所在的 call id）；链接插件（有 URL 的成功结果记下来）。
// 要注意：Result 现在是 tool.Result（通用部分）。根包的 toolResult 还有 Done、Answer、Skipped、
// PageHash、Product、Artifact……核心要认识哪些，是你要定的事：
//   - 核心必须认识的：Failed、PageChanged、Image、URL（跳过规则和截图要用）；
//   - 建议让 tool.Result 多一个 Terminate bool（pi 的 terminate）代替 Done，done 就不是特例；
//   - 商品卡片、产物这类只有个别工具有的，建议放进 Result 的一个 Extras map[string]any，由对应插件写、对应插件读，
//     核心原样记进日志的 Details。
//
// func (r *Registry) AfterTool(fn func(...))
// ───────────────────────────────────────────────────────────────────────────

// ───────────────────────────────────────────────────────────────────────────
// 钩子 3：BeforeModel —— 每次请求模型前，改这一次要发的消息
//
// 时机：循环每一步，调 r.chat 之前。
// 现在的代码：agent.go 循环的 ①：compactSnapshots(messages, snapshotAt)、compactScreenshots(...)。
// 参数：完整的消息列表。
// 返回值：这一次要发出去的消息列表。
// 谁用：浏览器插件——只留最新一份快照、最新一张截图，旧的换成占位语。
// 要注意（这是这个钩子比现在的写法好的地方）：
//   - 核心把它的返回值只用于这一次请求，不写回核心保存的历史，也不进日志。
//     现在的 compactSnapshots 直接改 messages，所以才有那段「Parts 和历史共用，只能整个换掉」的小心。
//     插件拿到的是消息的浅拷贝，换一条消息的 Parts 就换一条，不会伤到原件。
//   - 多个插件：上一个的返回值是下一个的输入。
//   - 怎么找到「哪条是旧快照」：用 ToolCallID（AfterTool 里记下带快照的调用 id），不用数组下标。
//     截图是核心在整批工具回复后面追加的用户消息，没有 ToolCallID：
//     你要定怎么认它——建议给 llm.Message 加一个内部用的 Tag string（不发给服务商），
//     核心追加截图消息时打上 Tag，插件按 Tag 找。
//
// func (r *Registry) BeforeModel(fn func([]llm.Message) []llm.Message)
// ───────────────────────────────────────────────────────────────────────────

// ───────────────────────────────────────────────────────────────────────────
// 钩子 4：OnAnswer —— 最终回答出来时，改文字、附带产出
//
// 时机：确定「这是最终回答」的那一刻，记进日志之前。有三个来源：模型直接回答；调用 done；被迫收尾后的回答。
// 现在的代码：agent.go 的 answerDetails（存回答引用的图，返回 Details{Final, Shots}），循环里调了两处，
// finish 里一处；chat.go 的 answerEvents 里切 <followups> 块。
// 参数：一个 *Answer {Text string; Extras map[string]any}（你来定义这个类型）。
// 返回值：没有，插件直接改它。
// 谁用：图片插件——存回答 [图N] 引用的图，写进 Extras["shots"]；followups 插件——切掉 <followups> 块，
// 清洗后写进 Extras["followups"]。
// 要注意：
//   - 为什么要在记日志之前：日志只追加，记了就改不了（agent.go 现在的注释写过）。
//     Extras 和回答一起记进日志，打开历史对话时直接读出来，chat_page.go 就不用再解析一遍。
//   - 需要 ctx：存图要上传。
//   - 不返回 error：现在 answerDetails 也不返回，存失败了回答照给。
//
// func (r *Registry) OnAnswer(fn func(ctx context.Context, a *Answer))
// ───────────────────────────────────────────────────────────────────────────

// ───────────────────────────────────────────────────────────────────────────
// 钩子 5：EndOfTurn —— 一轮结束，追加提醒或者走收尾
//
// 时机：一轮的工具都执行完、回复和截图都记进了消息之后。done 和插件断开已经在它之前处理了。
// 现在的代码：agent.go 循环的 ⑦⑧⑨：
//   ⑦ r.failures.record(results)       连续失败（agent_loop.go 的 failureCounter）
//   ⑧ r.loops.record(fingerprint(...)) 重复动作（agent_loop.go 的 loopDetector）
//   ⑨ 剩 warnAt 步 / deliverAt 步时追加提醒
//   以及 for 条件里的 maxSteps，和循环耗尽以后走 finishSteps。
// 参数：一个 Turn {Step 第几步（从 0 开始）; Left 还剩几步; Calls 这一轮的调用; Results 对应的结果（跳过的也在，带 Skipped）}。
// 返回值：一个 Verdict {Messages 要追加的提醒（会标成 Synthetic 记进日志）; Finish 非空就是走收尾，值是 finishReason}。
// 谁用：guard 包的 Steps、Failures、Loops，和交付插件的「剩 10 步提醒做成果」。
// 要注意：
//   - 顺序是规则：多个插件按注册顺序调用，第一个返回 Finish 的赢，但后面插件的 Messages 还要追加吗？
//     现在的行为是：连续失败收尾时，不会再发循环提醒和步数提醒（直接 return）。要保持这一点，
//     就是「第一个 Finish 之后，不再调后面的」。你决定，我的建议是保持现在的行为。
//   - 现在 ⑦ 在 ⑧ 之前：装配列表的顺序要写成 Steps、Failures、Loops，并且用测试固定住。
//   - 插件自己的状态（连续失败了几次、最近 10 个动作的指纹）在闭包里，不在核心里。
//   - maxSteps 谁管？Steps 插件只能在一轮结束时说「该收尾了」，循环的 for 条件还得知道上限。
//     建议循环用「for 一直转，直到有结论」，Steps 插件在最后一步返回 Finish: finishSteps。
//
// func (r *Registry) EndOfTurn(fn func(Turn) Verdict)
// ───────────────────────────────────────────────────────────────────────────

// ───────────────────────────────────────────────────────────────────────────
// 钩子 6：OnFinish —— 任务结束，往结果里放东西
//
// 时机：循环的所有出口之后（正常结束、done、收尾、出错），agent.go 的 run() 现在在这里统一带上
// res.Products、res.Shots。出错返回时也调（现在是 res != nil 才带）。
// 参数：*Result（核心的结果类型，你来定）。
// 谁用：商品插件（Products）、图片插件（Shots）、交付插件（Artifacts）。
// 要注意：Result 里这些东西放在哪里：建议同样用 Extras map[string]any，
// 入口（Chat、Analyze、RunTask）自己按 key 取出来。核心的 Result 就只有 Text、Success、Finish、Steps、Reasoning。
//
// func (r *Registry) OnFinish(fn func(*Result))
// ───────────────────────────────────────────────────────────────────────────

// 第二步再做（先不写）：Prompt —— 系统提示词里加一段。
// 现在系统提示词是入口自己拼的（chat.go 的 chatPrompt、service.go 的 buildPostPrompt），
// 拼之前就要知道装了哪些插件。先让入口继续拼，插件接管提示词放第二步。
