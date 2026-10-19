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
	toolTimeoutText = "页面太久没有响应，这个动作可能没有做成。可以先 snapshot 看看现在的页面，或者换一个网址。"

	// doneReminder 模型操作过浏览器以后只回了文字（概念 7）
	doneReminder = "如果已经查完了，请调用 done，把完整的回答写在 text 里；如果还没查完，请接着操作浏览器。"
)

// stepWarningText 在还剩 remaining 步时追加一次（概念 4）
func stepWarningText(remaining int) string {
	return fmt.Sprintf("只剩 %d 步了。信息够了就调用 done 作答；不够，就用剩下的步数补上最关键的一项。", remaining)
}

// loopNudgeText 在同一个动作重复出现时追加（概念 5）
func loopNudgeText(call llm.ToolCall, times int) string {
	return fmt.Sprintf("你已经在同一个页面状态下第 %d 次执行 %s(%s)，页面没有出现新的内容。"+
		"换个做法，比如换一个元素、换一个搜索词、换一个网站，或者根据已有的信息调用 done 作答。",
		times, call.Name, canonicalArgs(call.Arguments))
}

// finishPrompts 是收尾时追加的话，按原因（概念 4）。收尾时不给工具，模型只能用文字回答
var finishPrompts = map[finishReason]string{
	finishSteps:       "步数已经用完了，不能再操作浏览器。请根据已经看到的信息作答，没查到的信息直接说明未能核实。",
	finishFailures:    "浏览器操作连续失败，不再继续。请根据已经看到的信息作答，并说明哪些信息没能查到。",
	finishLoop:        "你一直在重复同样的操作，不再继续。请根据已经看到的信息作答，并说明哪些信息没能查到。",
	finishBrowserGone: "浏览器断开了，不能再操作。请根据已经看到的信息作答，并说明价格等信息没能实时核实。",
}
