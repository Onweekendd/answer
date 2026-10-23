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
	"testing"

	"github.com/apache/answer/internal/service/llm"
	"github.com/stretchr/testify/require"
)

// leakedDone 是 deepseek 把调用 done 写成文字时的样子（2026-10-18 小红书那次运行里的原样格式）
func leakedDone(text string) string {
	return `<｜｜DSML｜｜ calls>
<｜｜DSML｜｜ invoke name="done">
<｜｜DSML｜｜ parameter name="success" string="false">true</｜｜DSML｜｜ parameter>
<｜｜DSML｜｜ parameter name="text" string="true">` + text + `</｜｜DSML｜｜ parameter>
</｜｜DSML｜｜ invoke>
</｜｜DSML｜｜ calls>`
}

func TestAnswerText(t *testing.T) {
	multiLine := "**不买**\n- 理由一\n- 理由二"
	notDone := `<｜｜DSML｜｜ invoke name="click"><｜｜DSML｜｜ parameter name="text">x</｜｜DSML｜｜ parameter>`
	cases := []struct {
		name    string
		content string
		want    string
	}{
		{"正常的回答原样返回", "**买** 很好", "**买** 很好"},
		{"done 写成了文字：取出 text，多行也行", leakedDone(multiLine), multiLine},
		{"写成文字的不是 done：原样返回，不乱取", notDone, notDone},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Act
			got := AnswerText(&llm.ChatResp{Content: tc.content})

			// Assert
			require.Equal(t, tc.want, got)
		})
	}
}
