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
	"testing"

	"github.com/apache/answer/internal/schema"
	"github.com/apache/answer/internal/service/llm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPageMessages(t *testing.T) {
	// Arrange：第一轮带截图、中间截过一张图；第二轮问完以后又重新生成了一次（v2b 和 v2 是兄弟）
	entries := []Entry{
		msgEntry("u1", "", userMessage(&schema.PurchaseAIChatMessage{Text: "XM5 值不值", Images: []string{"https://x/uploads/post/a.jpg"}}), messageMeta{}),
		msgEntry("a1", "u1", callMsg("", llm.ToolCall{ID: "c1", Name: toolShot}), messageMeta{}),
		msgEntry("t1", "a1", toolMessage("c1", "已截图"), messageMeta{ToolName: toolShot}),
		msgEntry("s1", "t1", llm.Text(llm.RoleUser, "这是第 1 步截的图"), messageMeta{Synthetic: true}),
		msgEntry("a2", "s1", llm.Text(llm.RoleAssistant, "值"), messageMeta{}),
		viewEntry("v1", "a2", Event{Type: EventText, Delta: "值"}),
		msgEntry("u2", "v1", llm.Text(llm.RoleUser, "那 bose 呢"), messageMeta{}),
		msgEntry("a3", "u2", llm.Text(llm.RoleAssistant, "也行"), messageMeta{}),
		viewEntry("v2", "a3", Event{Type: EventText, Delta: "也行"}),
		msgEntry("a4", "u2", llm.Text(llm.RoleAssistant, "不如索尼"), messageMeta{}),
		viewEntry("v2b", "a4", Event{Type: EventText, Delta: "不如索尼"}),
	}

	// Act
	got := pageMessages(entries)

	// Assert：截图那条提醒、工具调用都不是页面上的消息
	require.Len(t, got, 5)
	assert.Equal(t, PageMessage{ID: "u1", Role: "user", CreatedAt: got[0].CreatedAt, Text: "XM5 值不值", Images: []string{"https://x/uploads/post/a.jpg"}}, got[0])
	assert.Equal(t, PageMessage{ID: "v1", ParentID: "u1", Role: "assistant", CreatedAt: got[1].CreatedAt, Events: []Event{{Type: EventText, Delta: "值"}}}, got[1])
	assert.Equal(t, "v1", got[2].ParentID, "第二个问题接在第一个回答后面")
	assert.Equal(t, "u2", got[3].ParentID)
	assert.Equal(t, "u2", got[4].ParentID, "重新生成的回答和原来那个是兄弟")
}
