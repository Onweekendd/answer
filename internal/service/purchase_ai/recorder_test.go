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
	"errors"
	"testing"

	"github.com/apache/answer/internal/service/llm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// skipRecorder 写完 record 和 close 以后删掉
func skipRecorder(t *testing.T) {
	t.Helper()
	t.Skip("TODO(精华)：写完 chatRecorder 的 record 和 close 删掉这一行")
}

func textEntry(text string) Entry {
	return Entry{Kind: EntryMessage, Message: storeMessage(llm.Text(llm.RoleUser, text), messageMeta{})}
}

func TestChatRecorder_ChainsFromTheParent(t *testing.T) {
	// Arrange：接在上一轮的 v0 后面（只测串链，和写入时机无关）
	rec := newChatRecorder(&fakeChatLog{}, fakeChatID, "v0")

	// Act
	a := rec.record(context.Background(), textEntry("一"))
	b := rec.record(context.Background(), textEntry("二"))

	// Assert
	assert.NotEqual(t, a, b)
	assert.Equal(t, b, rec.leaf())
}

func TestChatRecorder_EverythingIsInTheLogAfterClose(t *testing.T) {
	skipRecorder(t)
	// Arrange
	log := &fakeChatLog{}
	rec := newChatRecorder(log, fakeChatID, "v0")

	// Act
	a := rec.record(context.Background(), textEntry("一"))
	b := rec.record(context.Background(), textEntry("二"))
	c := rec.record(context.Background(), textEntry("三"))
	err := rec.close(context.Background())

	// Assert：按顺序，一条接一条
	require.NoError(t, err)
	require.Len(t, log.entries, 3)
	assert.Equal(t, []string{a, b, c}, []string{log.entries[0].ID, log.entries[1].ID, log.entries[2].ID})
	assert.Equal(t, []string{"v0", a, b}, []string{log.entries[0].ParentID, log.entries[1].ParentID, log.entries[2].ParentID})
	assert.Equal(t, "三", log.entries[2].Message.Parts[0].Text)
}

func TestChatRecorder_CloseWritesEvenAfterThePageIsClosed(t *testing.T) {
	skipRecorder(t)
	// Arrange：用户关了页面，请求的 ctx 已经取消了
	log := &fakeChatLog{}
	ctx, cancel := context.WithCancel(context.Background())
	rec := newChatRecorder(log, fakeChatID, "")
	rec.record(ctx, textEntry("一"))
	cancel()

	// Act
	err := rec.close(ctx)

	// Assert：前面的都要记下来，写库时用的 ctx 不能是已经取消的那个
	require.NoError(t, err)
	assert.Len(t, log.entries, 1)
	assert.NoError(t, log.ctxErr)
}

func TestChatRecorder_FailedWriteIsReported(t *testing.T) {
	skipRecorder(t)
	// Arrange：数据库出错
	log := &fakeChatLog{appendErr: errors.New("database is down")}
	rec := newChatRecorder(log, fakeChatID, "")
	rec.record(context.Background(), textEntry("一"))

	// Act
	err := rec.close(context.Background())

	// Assert：Chat 要打日志，错误不能吞掉
	assert.Error(t, err)
}
