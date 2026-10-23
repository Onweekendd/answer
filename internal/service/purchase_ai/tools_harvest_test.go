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

	"github.com/apache/answer/internal/service/sandbox"
	"github.com/stretchr/testify/assert"
)

func Test_harvestText(t *testing.T) {
	entries := []sandbox.Entry{{N: 1, File: "001.jpg", Title: "奶油风客厅"}}

	t.Run("缩略图只够挑选时，告诉模型原图地址在清单里", func(t *testing.T) {
		// Arrange：Pinterest，插件按默认 200 宽压的
		var data harvestData
		data.Stats.ThumbWidth = 200
		data.Stats.Ended = true

		// Act
		text := harvestText(entries, data)

		// Assert
		assert.False(t, data.thumbsAreMaterial())
		assert.Contains(t, text, "200 像素宽，只够挑选和预览")
		assert.Contains(t, text, "original（原图地址）")
	})

	t.Run("缩略图就是素材时，让模型直接用、别从工作台去这个网站下载", func(t *testing.T) {
		// Arrange：小红书列表，页面上最大的就是 640 宽的封面预览
		var data harvestData
		data.Stats.ThumbWidth = 640
		data.Stats.Ended = true

		// Act
		text := harvestText(entries, data)

		// Assert
		assert.True(t, data.thumbsAreMaterial())
		assert.Contains(t, text, "最宽 640 像素")
		assert.Contains(t, text, "不要用 bash 从这个网站另外下载图")
		assert.NotContains(t, text, "original")
	})

	t.Run("老版本插件不报宽度，按只够挑选说", func(t *testing.T) {
		var data harvestData
		assert.False(t, data.thumbsAreMaterial())
		assert.Contains(t, harvestText(entries, data), "200 像素宽")
	})
}
