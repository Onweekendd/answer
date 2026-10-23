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

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDone(t *testing.T) {
	t.Run("带上回答和是否成功，结束任务", func(t *testing.T) {
		// Act
		res, err := runDone(context.Background(), `{"text":"**买**","success":false}`)

		// Assert
		require.NoError(t, err)
		assert.True(t, res.Terminate)
		assert.Equal(t, "**买**", res.Answer)
		assert.False(t, res.Success)
	})

	t.Run("回答为空时不结束，让模型补上", func(t *testing.T) {
		// Act
		res, err := runDone(context.Background(), `{"text":"  ","success":true}`)

		// Assert
		require.NoError(t, err)
		assert.False(t, res.Terminate)
		assert.True(t, res.Failed)
	})

	t.Run("参数不是合法的 JSON 时告诉模型格式", func(t *testing.T) {
		// Act
		res, err := runDone(context.Background(), `{text: oops}`)

		// Assert
		require.NoError(t, err)
		assert.True(t, res.Failed)
		assert.Contains(t, res.Text, `"text"`)
	})

	t.Run("多余的字段（老对话里的 followups 参数）不报错", func(t *testing.T) {
		// Act
		res, err := runDone(context.Background(), `{"text":"好","success":true,"followups":["a"]}`)

		// Assert
		require.NoError(t, err)
		assert.True(t, res.Terminate)
	})
}
