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
//     ③ Agent 跑完，Chat 调 close
//   - 一轮会记多少：典型的一轮二三十条，快照那几条各有 8000 字
//   - 会出什么事：用户关了页面（请求的 ctx 被取消，Agent 停下）；进程在跑到一半时重启；写库失败（数据库抖了一下）
//   - 记下来的谁用：下一轮 Chat 用 ChatLog.Entries 读出来，pathTo 从 leaf 走回根，project 变成模型的历史。
//     所以链必须是连着的：中间断一节，下一轮就走不回根了
//
// closeTimeout 是 close 写库最多等多久
const closeTimeout = 10 * time.Second

type chatRecorder struct {
	log    ChatLog
	chatID int64
	parent string // 下一条接在谁后面：一开始是请求里的 parent_id，每记一条就挪到那一条

	// pending 是已经串好链、还没写进库的条目。写入时机选的是「每条立刻写，失败的留着下次一起写」：
	// 进程中途重启时，写进去的那些还在（用户的问题、做到一半的步骤），半截的调用由 project 第 4 步补；
	// 一条写失败也不会断链，它留在这里，下一次 record 或者 close 时和新的一起重写。
	// Append 是一个事务，一批要么全进要么全不进，所以整批重写不会写出重复的
	pending []Entry
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

// record 记下一条，返回它的 ID（页面和下一轮靠这个 ID 接上）。
//
// 上下文：
//   - 谁调用：Chat 记用户消息和最后的 view；Agent 循环里的 agentRun.keep 记每一条模型消息。
//     都在同一个 goroutine 里一条接一条地调，不会并发，不用加锁
//   - e：Kind 和 Message（或 View）已经填好，ID、ParentID、CreatedAt 还是空的，由 c.chain 填
//   - ctx：请求的 ctx。用户关了页面它就被取消了，这时写库会失败，没关系：条目留在 pending 里，close 会换个 ctx 写
//   - 返回值不带 error：写不进去不该打断这一轮（用户正在等回答），失败的留给 close 兜底并报告
//
// TODO(精华，你来写)，按这个顺序：
//  1. e = c.chain(e)，拿到带 ID 的条目
//  2. 追加到 c.pending
//  3. c.flush(ctx) 试着写掉，错误先忽略（写 `_ = c.flush(ctx)`，让读代码的人知道是故意不管的）
//  4. 返回 e.ID
func (c *chatRecorder) record(ctx context.Context, e Entry) string {
	e = c.chain(e)

	c.pending = append(c.pending, e)

	_ = c.flush(ctx)

	return e.ID
}

// flush 把 pending 里的全部条目一次写进库，成功就清空，失败就原样留着、返回错误。
//
// 能用的：c.log.Append(ctx, c.chatID, entries) error。它就是你写的 chatLog.Append，在一个事务里按顺序插入；
// 测试里是 fakeChatLog（chatlog_test.go），它可以模拟写库出错（appendErr）、记下写库时 ctx 有没有被取消（ctxErr）
//
// TODO(精华，你来写)：
//  1. pending 是空的就直接返回 nil（不要白开一个事务）
//  2. Append 整个 pending；出错就返回这个错误，pending 不动
//  3. 成功就清空 pending（c.pending = nil），返回 nil
func (c *chatRecorder) flush(ctx context.Context) error {
	if len(c.pending) == 0 {
		return nil
	}

	err := c.log.Append(ctx, c.chatID, c.pending)
	if err != nil {
		return err
	}

	c.pending = nil
	return nil
}

// close 在这一轮结束时调一次：正常结束、出错、用户关了页面都要调（chat.go 里 Agent 跑完以后）。
//
// TODO(精华，你来写)：
//  1. 换一个写库用的 ctx：调用方给的是请求的 ctx，用户关了页面它就已经被取消了，拿它写库会立刻失败，
//     偏偏这正是最需要把前面的记下来的时候。context.WithoutCancel(ctx) 摘掉取消（ctx 里带的值还在），
//     再套一层 context.WithTimeout(…, closeTimeout) 给个期限，别让一次卡住的写库拖住 goroutine；
//     WithTimeout 返回的 cancel 要 defer 调掉
//  2. 用这个 ctx flush，把它的错误返回（Chat 会打日志）
func (c *chatRecorder) close(ctx context.Context) error {
	dbCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), closeTimeout)
	defer cancel()

	return c.flush(dbCtx)
}

// keep 把一条消息接到这一轮的对话后面，有 recorder 时同时记进日志。
// 循环里所有往 messages 里加消息的地方都走这里，日志才和模型实际看到的一致
func (r *agentRun) keep(ctx context.Context, messages []llm.Message, m llm.Message, meta messageMeta) []llm.Message {
	if r.rec != nil {
		r.rec.record(ctx, Entry{Kind: EntryMessage, Message: storeMessage(m, meta)})
	}
	return append(messages, m)
}
