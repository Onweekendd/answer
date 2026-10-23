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

// 核心在对话里追加或替换的话。都是固定的文字：追加以后不再改，前缀缓存才能命中。
// 属于某个插件的话（连续失败、重复动作、步数提醒……）跟着那个插件走，不在这里。

const (
	// SkippedText 回复因为页面变了而没有执行的调用
	SkippedText = "没有执行：前一个动作可能改变了页面，这个调用用的编号已经过期。先 snapshot 查看新页面，再决定要不要做。"

	// SkippedAfterFailText 回复排在失败的调用后面的调用：模型安排它们时，以为前一个会成功
	SkippedAfterFailText = "没有执行：前一个动作失败了，这个调用是按它成功来安排的。先看前一个动作失败的原因，再决定下一步。"

	// SkippedAfterDoneText 回复排在 done 后面的调用
	SkippedAfterDoneText = "没有执行：任务已经结束。"

	// DoneReminder 模型操作过工具以后只回了文字
	DoneReminder = "如果已经查完了，请调用 done，把完整的回答写在 text 里；如果还没查完，请接着操作浏览器。"

	// EmptyReply 代替一条空回复放回对话：服务商会拒绝既没有内容、也没有工具调用的 assistant 消息
	// （2026-10-19 实测：模型把输出全花在思考上，正文是空的，下一次请求被拒，整次任务报错）
	EmptyReply = "（这一步没有写出内容）"

	// EmptyReminder 模型回了空回复
	EmptyReminder = "你上一条回复是空的，什么都没写出来。如果已经查完了，请调用 done，把完整的回答写在 text 里；如果还没查完，请接着操作浏览器。"

	// LeakedCallText 是收尾时模型把工具调用写成了文字以后，再问一次时追加的话
	LeakedCallText = "你刚才写的是一个工具调用，不是回答。现在已经不能再调用任何工具了：直接用文字把回答写给用户，没查到的说明没查到。"

	// StepsPrompt、EmptyPrompt 是核心自己要求收尾时追加的话。收尾时不给工具，模型只能用文字回答
	StepsPrompt = "步数已经用完了，不能再操作浏览器。请根据已经看到的信息作答，没查到的信息直接说明未能核实。"
	EmptyPrompt = "你的回复又是空的。不能再操作浏览器了，请直接把回答写出来：根据已经看到的信息作答，没查到的直接说明未能核实。"
)
