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

package purchasechat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/apache/answer/internal/base/data"
	"github.com/apache/answer/internal/entity"
	purchaseai "github.com/apache/answer/internal/service/purchase_ai"
)

// chatLog keeps each conversation's log in purchase_chat_entry.
type chatLog struct {
	data *data.Data
}

// NewChatLog new chat log
func NewChatLog(data *data.Data) purchaseai.ChatLog {
	return &chatLog{data: data}
}

// Entries 取出一个对话的全部条目（读），见 ChatLog 接口。
//
// 上下文：
//   - 谁调用：每一轮对话开始时 PurchaseAIService.chatPath（chat.go），拿到以后交给 pathTo 走出一条分支、
//     再交给 project 拼成历史发给模型；打开旧对话时 ChatHistory（chat_page.go），交给 pageMessages 给页面重放
//   - 参数：chatID 是请求里的 chat_id，比如 7；userID 是登录用户，比如 "1"。chatID 是前端传上来的，
//     谁都可以随便填一个数字，所以要先确认这个对话是 userID 的
//   - 读的两张表：
//     purchase_chat：一个对话一行（entity.PurchaseChat），用户发第一条消息前前端调 POST /purchase/chat 建的
//     （purchaseChatRepo.AddChat），user_id 就是建它的人
//     purchase_chat_entry：一条日志一行（entity.PurchaseChatEntry），由 Append 写。id 是自增主键，
//     entry_id 是 "e_k3j9x0abcd12" 这样的条目 id，parent_id 是上一条的 entry_id，body 是 JSON
//   - 返回：entries 按写入的先后（也就是 id 从小到大）；对话不存在或者不是 userID 的，返回 nil, false, nil，
//     不算错误（调用方会回「找不到这段对话」）；数据库出错才返回 err
//
// 能用的：
//   - r.data.DB 是 *xorm.Engine。r.data.DB.Context(ctx) 开一个带 ctx 的会话，后面接链式条件：
//     .Where("a = ? AND b = ?", x, y) / .Asc("id") / .Exist(&entity.X{})（只问有没有，返回 bool, error）/
//     .Find(&rows)（rows 是 []*entity.X，查出所有行）。同目录 sandbox_store.go 的 SandboxOf 就是先查归属的例子
//   - fromRow(row)（本文件下面）：一行 → 一个 purchaseai.Entry，body 的 JSON 在里面解开
//   - dbError(err)（purchase_chat_repo.go）：把数据库错误包成 Answer 统一的 500，nil 进 nil 出
//
// TODO(精华，你来写)，按这个顺序：
//  1. 先确认 purchase_chat 里有 id = chatID 且 user_id = userID 的一行；没有就 return nil, false, nil
//  2. 查出 purchase_chat_entry 里 chat_id = chatID 的所有行，按 id 升序
//  3. 每一行用 fromRow 转成 Entry；某一行解不开就整个返回错误（日志坏了，不能悄悄少一条）
//
// 思考题：为什么按自增的 id 排，而不是按 created_at 排？一轮里的几十条可能在同一秒写进去
func (r *chatLog) Entries(ctx context.Context, chatID int64, userID string) ([]purchaseai.Entry, bool, error) {
	return nil, false, errors.New("TODO: chatLog.Entries")
}

// Append 把一批条目追加进日志（写），见 ChatLog 接口。
//
// 上下文：
//   - 谁调用：chatRecorder（recorder.go）。一轮里先记用户消息，Agent 循环每加一条消息记一条，最后记一条 view；
//     record 是每条立刻 Append，还是攒着在 close 里一次 Append，是你在 recorder 那个 TODO 里决定的。
//     所以这里的 entries 可能是 1 条，也可能是一轮的二三十条
//   - entries 长什么样（一轮）：
//     {ID: "e_u1", ParentID: "e_v0", Kind: "message", Message: &StoredMessage{Role: "user", Parts: [{Text: "那 bose 呢"}]}}
//     {ID: "e_a1", ParentID: "e_u1", Kind: "message", Message: &StoredMessage{Role: "assistant", ToolCalls: [...]}}
//     {ID: "e_t1", ParentID: "e_a1", Kind: "message", Message: &StoredMessage{Role: "tool", ToolName: "open", ...}}
//     ……
//     {ID: "e_v1", ParentID: "e_a9", Kind: "view", View: &StoredView{Events: [...]}}
//     ID 是 recorder 用 newEntryID 生成的，ParentID 已经串好了，这里只管按顺序存
//   - 写的两张表：每个条目在 purchase_chat_entry 插一行（toRow 转好）；再把 purchase_chat 这一行的
//     updated_at 改成现在，侧边栏的对话列表按 updated_at 倒序（purchaseChatRepo.ListChats），刚聊过的排最前
//   - 表上有 UNIQUE(chat_id, entry_id)：同一个对话里同一个条目 id 再插一次会报错，这是有意的
//
// 能用的：
//   - toRow(chatID, e)（本文件下面）：一个 Entry → 一行，body 已经是 JSON
//   - 事务：r.data.DB.Transaction(func(session *xorm.Session) (any, error) { ... })，函数返回 error 就整体回滚。
//     里面先 session = session.Context(ctx)，再用 session.Insert(row)、session.ID(id).Cols("col").Update(&entity.X{})。
//     PurchaseChat.UpdatedAt 带 xorm 的 `updated` 标签，Update 时 xorm 自动填当前时间，不用自己给值。
//     同目录 purchase_chat_repo.go 的 SaveMessage 就是「写消息 + 改 updated_at」放一个事务的例子
//   - 需要 import "xorm.io/xorm"（Transaction 的参数类型）
//
// TODO(精华，你来写)，按这个顺序：
//  1. entries 为空直接返回 nil
//  2. 先在事务外把每个条目 toRow 成行，转换出错就返回（不要开了事务才发现）
//  3. 开事务：按顺序一行一行 Insert，再更新 purchase_chat 的 updated_at
//  4. 出错用 dbError 包一下返回
//
// 思考题：
//   - 为什么要放在一个事务里？想想插到第 15 条时数据库断了，下一轮 pathTo 会看到什么
//   - xorm 支持 session.Insert(&rows) 一次插一批，为什么这里一条一条插？提示：看 Entries 靠什么排序
func (r *chatLog) Append(ctx context.Context, chatID int64, entries []purchaseai.Entry) error {
	return errors.New("TODO: chatLog.Append")
}

func toRow(chatID int64, e purchaseai.Entry) (*entity.PurchaseChatEntry, error) {
	var body any
	switch e.Kind {
	case purchaseai.EntryMessage:
		body = e.Message
	case purchaseai.EntryView:
		body = e.View
	default:
		return nil, fmt.Errorf("purchase chat: unknown entry kind %q", e.Kind)
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	return &entity.PurchaseChatEntry{ChatID: chatID, EntryID: e.ID, ParentID: e.ParentID, Kind: e.Kind, Body: string(raw)}, nil
}

func fromRow(row *entity.PurchaseChatEntry) (purchaseai.Entry, error) {
	e := purchaseai.Entry{ID: row.EntryID, ParentID: row.ParentID, Kind: row.Kind, CreatedAt: row.CreatedAt}
	var err error
	switch row.Kind {
	case purchaseai.EntryMessage:
		e.Message = &purchaseai.StoredMessage{}
		err = json.Unmarshal([]byte(row.Body), e.Message)
	case purchaseai.EntryView:
		e.View = &purchaseai.StoredView{}
		err = json.Unmarshal([]byte(row.Body), e.View)
	default:
		err = fmt.Errorf("unknown entry kind %q", row.Kind)
	}
	if err != nil {
		return purchaseai.Entry{}, fmt.Errorf("purchase chat: entry %s: %w", row.EntryID, err)
	}
	return e, nil
}
