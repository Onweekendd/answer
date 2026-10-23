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
	"encoding/json"
	"github.com/apache/answer/internal/service/purchase_ai/tool/browsertool"
	"testing"

	"github.com/apache/answer/internal/service/llm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// callMsg 是一条只调用工具的 assistant 消息
func callMsg(reasoning string, calls ...llm.ToolCall) llm.Message {
	return llm.Message{Role: llm.RoleAssistant, Reasoning: reasoning, ToolCalls: calls}
}

// twoTurns 是两轮对话的日志：每轮都 snapshot 过一次，第一轮的回答引用了图 1
func twoTurns() []Entry {
	return []Entry{
		msgEntry("u1", "", llm.Text(llm.RoleUser, "XM5 值不值"), messageMeta{}),
		msgEntry("a1", "u1", callMsg("先看页面", llm.ToolCall{ID: "c1", Name: toolSnapshot}), messageMeta{}),
		msgEntry("t1", "a1", toolMessage("c1", "第一份快照"), messageMeta{ToolName: toolSnapshot}),
		msgEntry("a2", "t1", llm.Text(llm.RoleAssistant, "券后 2299\n[图1：券后价]"), messageMeta{Details: &Details{Final: true, Shots: []Shot{{ID: 1, Image: "https://x/uploads/agent/1.jpg"}}}}),
		msgEntry("u2", "a2", llm.Text(llm.RoleUser, "那 bose 的呢"), messageMeta{}),
		msgEntry("a3", "u2", callMsg("再看", llm.ToolCall{ID: "c2", Name: toolSnapshot}), messageMeta{}),
		msgEntry("t2", "a3", toolMessage("c2", "第二份快照"), messageMeta{ToolName: toolSnapshot}),
	}
}

func TestProject_MessagesComeBackInOrder(t *testing.T) {
	// Act
	got := project(twoTurns())

	// Assert：一条不少，回答上只给页面看的图模型看不到
	require.Len(t, got, 7)
	assert.Equal(t, llm.RoleUser, got[4].Role, "第二轮的用户消息紧接着第一轮的回答")
	assert.Equal(t, "那 bose 的呢", got[4].Parts[0].Text)
	assert.Len(t, got[3].Parts, 1, "Details 里的图不会变成消息的一段")
}

func TestProject_SnapshotsAreOmitted(t *testing.T) {
	// Act
	got := project(twoTurns())

	// Assert：历史里的页面都已经关了，最后一份也不留全文
	assert.Equal(t, browsertool.SnapshotOmitted, got[2].Parts[0].Text)
	assert.Equal(t, browsertool.SnapshotOmitted, got[6].Parts[0].Text)
	assert.Equal(t, "c2", got[6].ToolCallID, "结果还在，只是换了内容：调用要有回答")
}

func TestProject_OldPictureRefsBecomeText(t *testing.T) {
	// Arrange：第二轮用 done 回答，回答在参数里
	done, _ := json.Marshal(map[string]any{"text": "bose 便宜\n[图2：价格]", "success": true})
	path := append(twoTurns(),
		msgEntry("a4", "t2", callMsg("", llm.ToolCall{ID: "c3", Name: toolDone, Arguments: string(done)}), messageMeta{}),
		msgEntry("t3", "a4", toolMessage("c3", "已结束。"), messageMeta{ToolName: toolDone}))

	// Act
	got := project(path)

	// Assert
	assert.Equal(t, "券后 2299\n（配图：券后价）", got[3].Parts[0].Text)
	var args struct {
		Text string `json:"text"`
	}
	require.NoError(t, json.Unmarshal([]byte(got[7].ToolCalls[0].Arguments), &args), "改完还得是合法的 JSON")
	assert.Equal(t, "bose 便宜\n（配图：价格）", args.Text)
}

func TestProject_ReasoningAndCallsAreKept(t *testing.T) {
	// Act
	got := project(twoTurns())

	// Assert：deepseek 要求原样带回思考
	assert.Equal(t, "先看页面", got[1].Reasoning)
	assert.Equal(t, "c1", got[1].ToolCalls[0].ID)
	assert.Equal(t, "c1", got[2].ToolCallID)
}

func TestProject_UnfinishedCallsGetAResult(t *testing.T) {
	// Arrange：一次调了两个工具，只记下了第一个的结果就断了；之后用户又发了一条
	path := []Entry{
		msgEntry("u1", "", llm.Text(llm.RoleUser, "XM5 值不值"), messageMeta{}),
		msgEntry("a1", "u1", callMsg("", llm.ToolCall{ID: "c1", Name: toolOpen}, llm.ToolCall{ID: "c2", Name: toolSnapshot}), messageMeta{}),
		msgEntry("t1", "a1", toolMessage("c1", "已打开"), messageMeta{ToolName: toolOpen}),
		msgEntry("u2", "t1", llm.Text(llm.RoleUser, "怎么停了"), messageMeta{}),
	}

	// Act
	got := project(path)

	// Assert：c2 的结果补在 c1 的结果后面、用户的新消息前面
	require.Len(t, got, 5)
	assert.Equal(t, "c1", got[2].ToolCallID)
	assert.Equal(t, llm.RoleTool, got[3].Role)
	assert.Equal(t, "c2", got[3].ToolCallID)
	assert.Equal(t, unfinishedCallText, got[3].Parts[0].Text)
	assert.Equal(t, llm.RoleUser, got[4].Role)
}

func TestProject_UnfinishedCallsAtTheEnd(t *testing.T) {
	// Arrange：最后一条就是带调用的 assistant，一个结果都没有
	path := []Entry{
		msgEntry("u1", "", llm.Text(llm.RoleUser, "XM5 值不值"), messageMeta{}),
		msgEntry("a1", "u1", callMsg("", llm.ToolCall{ID: "c1", Name: toolOpen}), messageMeta{}),
	}

	// Act
	got := project(path)

	// Assert
	require.Len(t, got, 3)
	assert.Equal(t, "c1", got[2].ToolCallID)
}
