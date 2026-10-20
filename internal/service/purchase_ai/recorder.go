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
	"context"
	"time"

	"github.com/apache/answer/internal/service/llm"
)

// chatRecorder 把一轮里产生的条目写进对话日志（上下文管理第一批的精华 ③）。
// 一轮一个，用完就扔。条目一条接一条串成链：第一条接在请求的 parent_id 后面，之后每条接在上一条后面。
//
// 上下文：
//   - 谁调用：
//     ① Chat 开始一轮时 newChatRecorder，先 record 这一轮的用户消息（重新生成时没有这一条）；
//     ② Agent 循环每往对话里加一条消息（assistant 的调用、工具结果、提醒、截图那条）都经过
//     agentRun.keep，它调 record；
//     ③ Chat 最后 record 一条 view（这一轮推给页面的事件），然后 close
//   - 一轮会记多少：典型的一轮二三十条，快照那几条各有 8000 字
//   - 会出什么事：用户关了页面（请求的 ctx 被取消，Agent 停下）；进程在跑到一半时重启；写库失败（数据库抖了一下）
//   - 记下来的谁用：下一轮 Chat 用 ChatLog.Entries 读出来，pathTo 从 leaf 走回根，project 变成模型的历史。
//     所以链必须是连着的：中间断一节，下一轮就走不回根了
type chatRecorder struct {
	log    ChatLog
	chatID int64
	parent string // 下一条接在谁后面：一开始是请求里的 parent_id，每记一条就挪到那一条

	// TODO(精华，你来写)：写入时机要用到的东西加在这里（比如攒着还没写的条目）
}

func newChatRecorder(log ChatLog, chatID int64, parent string) *chatRecorder {
	return &chatRecorder{log: log, chatID: chatID, parent: parent}
}

// chain 把一条接到链上：生成 ID、填上 ParentID，再把 parent 挪到它。只管串链，不写库
func (c *chatRecorder) chain(e Entry) Entry {
	e.ID = newEntryID()
	e.ParentID = c.parent
	e.CreatedAt = time.Now()
	c.parent = e.ID
	return e
}

// leaf 是最后记下的那一条的 ID：页面下一条消息的 parent_id 就是它
func (c *chatRecorder) leaf() string {
	return c.parent
}

// record 记下一条，返回它的 ID。
//
// TODO(精华，你来写)，按这个顺序：
//  1. 先决定写入时机，写一句注释说明理由：
//     A. 每一条都立刻写（Append 一条）：跑到一半崩了，前面的都在；代价是一轮二三十次 INSERT，
//     而且日志里会留下「只有调用、没有结果」的半截，靠 project 第 4 步补
//     B. 攒在内存里，close 时一次写完：一次事务，干净；代价是进程崩了这一轮全丢，用户刚才看到的回答下次打开就没了
//     对照上面「会出什么事」的三种情况，各自在 A、B 下是什么结果
//  2. 先 c.chain(e) 拿到带 ID 的条目，再按你的选择写库或者攒起来，返回 ID
//  3. 选 A 的话，写库失败怎么办：这一条丢了，后面的 ParentID 指向它，链就断了。
//     要么把没写成的留着、下次 record 或者 close 时再一起写，要么……你来定
func (c *chatRecorder) record(ctx context.Context, e Entry) string {
	return c.chain(e).ID
}

// close 在这一轮结束时调一次：正常结束、出错、用户关了页面都要调（Chat 里 defer）。
//
// TODO(精华，你来写)：
//  1. 把还没写进库的写掉
//  2. ctx 很关键：调用方给的是请求的 ctx，用户关了页面它就已经被取消了，拿它写库会立刻失败，
//     偏偏这正是最需要把前面的记下来的时候。用 context.WithoutCancel(ctx) 摘掉取消，
//     再套一个 context.WithTimeout 给个期限（比如 10 秒），别让一次卡住的写库拖住 goroutine
//  3. 返回写库的错误，Chat 会打日志
func (c *chatRecorder) close(ctx context.Context) error {
	return nil
}

// keep 把一条消息接到这一轮的对话后面，有 recorder 时同时记进日志。
// 循环里所有往 messages 里加消息的地方都走这里，日志才和模型实际看到的一致
func (r *agentRun) keep(ctx context.Context, messages []llm.Message, m llm.Message, meta messageMeta) []llm.Message {
	if r.rec != nil {
		r.rec.record(ctx, Entry{Kind: EntryMessage, Message: storeMessage(m, meta)})
	}
	return append(messages, m)
}
