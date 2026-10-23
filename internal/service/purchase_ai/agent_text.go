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

package purchaseai

import (
	"fmt"

	"github.com/apache/answer/internal/service/llm"
	"github.com/apache/answer/internal/service/purchase_ai/tool/browsertool"
)

// 循环在对话里追加或替换的话。都是固定的文字：追加以后不再改，前缀缓存才能命中（概念 2）。

const (
	// snapshotOmitted 替换旧快照的内容
	snapshotOmitted = "（这是之前的页面，内容已省略，以最新一份快照为准。）"

	// screenshotOmitted 替换 Agent 自己截的旧截图（用户上传的截图不换）
	screenshotOmitted = "（这是之前的截图，已省略，以最新一张截图为准。）"

	// skippedText 回复因为页面变了而没有执行的调用（概念 1）
	skippedText = "没有执行：前一个动作可能改变了页面，这个调用用的编号已经过期。先 snapshot 查看新页面，再决定要不要做。"

	// skippedAfterFailText 回复排在失败的调用后面的调用：模型安排它们时，以为前一个会成功
	skippedAfterFailText = "没有执行：前一个动作失败了，这个调用是按它成功来安排的。先看前一个动作失败的原因，再决定下一步。"

	// skippedAfterDoneText 回复排在 done 后面的调用
	skippedAfterDoneText = "没有执行：任务已经结束。"

	// browserGoneText 回复插件断开时正在执行的调用
	browserGoneText = "浏览器断开了，这个动作没有做成。"

	// toolTimeoutText 回复单次调用插件超时（概念 6）
	toolTimeoutText = browsertool.TimeoutText

	// doneReminder 模型操作过浏览器以后只回了文字（概念 7）
	doneReminder = "如果已经查完了，请调用 done，把完整的回答写在 text 里；如果还没查完，请接着操作浏览器。"

	// emptyReply 代替一条空回复放回对话：服务商会拒绝既没有内容、也没有工具调用的 assistant 消息
	// （2026-10-19 实测：模型把输出全花在思考上，正文是空的，下一次请求被拒，整次任务报错）
	emptyReply = "（这一步没有写出内容）"
	// emptyReminder 模型回了空回复（概念 7）
	emptyReminder = "你上一条回复是空的，什么都没写出来。如果已经查完了，请调用 done，把完整的回答写在 text 里；如果还没查完，请接着操作浏览器。"
)

// stepWarningText 在还剩 remaining 步时追加一次（概念 4）
func stepWarningText(remaining int) string {
	return fmt.Sprintf("只剩 %d 步了。信息够了就调用 done 作答；不够，就用剩下的步数补上最关键的一项。", remaining)
}

// deliverWarningText 在有工作台、剩 deliverAt 步时追加一次：用户要的图、表、网页留够步数做
func deliverWarningText(remaining int) string {
	return fmt.Sprintf("还剩 %d 步。用户要你做图、表格、网页这类东西的话，现在就开始在工作台里做（写网页、shot 截图、看一眼），"+
		"做完还有步数再补查；用户没要就不用管这条。", remaining)
}

// loopNudgeText 在同一个动作重复出现时追加（概念 5）
func loopNudgeText(call llm.ToolCall, times int) string {
	return fmt.Sprintf("你已经在同一个页面状态下第 %d 次执行 %s(%s)，页面没有出现新的内容。"+
		"换个做法，比如换一个元素、换一个搜索词、换一个网站，或者根据已有的信息调用 done 作答。",
		times, call.Name, canonicalArgs(call.Arguments))
}

// leakedCallText 是收尾时模型把工具调用写成了文字以后，再问一次时追加的话
const leakedCallText = "你刚才写的是一个工具调用，不是回答。现在已经不能再调用任何工具了：直接用文字把回答写给用户，没查到的说明没查到。"

// finishPrompts 是收尾时追加的话，按原因（概念 4）。收尾时不给工具，模型只能用文字回答
var finishPrompts = map[finishReason]string{
	finishSteps:       "步数已经用完了，不能再操作浏览器。请根据已经看到的信息作答，没查到的信息直接说明未能核实。",
	finishFailures:    "浏览器操作连续失败，不再继续。请根据已经看到的信息作答，并说明哪些信息没能查到。",
	finishLoop:        "你一直在重复同样的操作，不再继续。请根据已经看到的信息作答，并说明哪些信息没能查到。",
	finishBrowserGone: "浏览器断开了，不能再操作。请根据已经看到的信息作答，并说明价格等信息没能实时核实。",
	finishEmpty:       "你的回复又是空的。不能再操作浏览器了，请直接把回答写出来：根据已经看到的信息作答，没查到的直接说明未能核实。",
}
