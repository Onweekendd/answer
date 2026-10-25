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
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/apache/answer/internal/base/reason"
	"github.com/apache/answer/internal/schema"
	"github.com/apache/answer/internal/service/llm"
	"github.com/apache/answer/internal/service/uploader"
	pacmanerrors "github.com/segmentfault/pacman/errors"
	"github.com/stretchr/testify/require"
)

// fakeUploader answers PostImagePath from a map. Embedding the interface
// satisfies all of its other methods, which these tests never call; calling
// one would panic on the nil interface, which is the right outcome.
type fakeUploader struct {
	uploader.UploaderService
	paths map[string]string
}

func (f fakeUploader) PostImagePath(_ context.Context, imageURL string) (string, error) {
	p, ok := f.paths[imageURL]
	if !ok {
		return "", uploader.ErrNotPostImage
	}
	return p, nil
}

// writePNG saves a small image and returns its path.
func writePNG(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "shot.png")
	f, err := os.Create(p)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()
	require.NoError(t, png.Encode(f, image.NewRGBA(image.Rect(0, 0, 20, 10))))
	return p
}

const switchJSON = `{"name":"任天堂 Switch OLED 国行 白色","brand":"任天堂","model":"Switch OLED",
	"specs":["国行","白色"],"platform":"京东","shop":"任天堂京东自营旗舰店","official":true,
	"price":2099,"list_price":2599,"promotions":["满2000减100"],"confidence":0.9,"notes":""}`

func Test_parseProduct(t *testing.T) {
	t.Run("正常的 JSON", func(t *testing.T) {
		// Act
		p, err := parseProduct(switchJSON)

		// Assert
		require.NoError(t, err)
		require.Equal(t, "任天堂 Switch OLED 国行 白色", p.Name)
		require.Equal(t, []string{"国行", "白色"}, p.Specs)
		require.True(t, *p.Official)
		require.InDelta(t, 2099.0, *p.Price, 0)
		require.InDelta(t, 2599.0, *p.ListPrice, 0)
	})

	t.Run("包在代码块里也能解析", func(t *testing.T) {
		for _, content := range []string{
			"```json\n" + switchJSON + "\n```",
			"```\n" + switchJSON + "\n```",
			"  \n" + switchJSON + "\n  ",
		} {
			// Act
			p, err := parseProduct(content)

			// Assert
			require.NoError(t, err, content)
			require.Equal(t, "Switch OLED", p.Model)
		}
	})

	t.Run("看不到的价格和官方店是 nil，0 元仍然是 0 元", func(t *testing.T) {
		// Act
		p, err := parseProduct(`{"name":"x","price":0,"list_price":null,"official":null,"confidence":0.5}`)

		// Assert
		require.NoError(t, err)
		require.NotNil(t, p.Price, "a price of 0 was shown, so it is not missing")
		require.InDelta(t, 0.0, *p.Price, 0)
		require.Nil(t, p.ListPrice)
		require.Nil(t, p.Official)
	})

	t.Run("不是 JSON 时报错", func(t *testing.T) {
		for _, content := range []string{"", "这是一个 Switch", `{"name":"x",`} {
			// Act
			p, err := parseProduct(content)

			// Assert
			require.Nil(t, p, content)
			require.ErrorContains(t, err, "not a valid JSON", content)
		}
	})

	t.Run("confidence 超出 0 到 1 时报错，不擅自换算", func(t *testing.T) {
		// Act: 90 probably means 90%, but that is a guess
		p, err := parseProduct(`{"name":"x","confidence":90}`)

		// Assert
		require.Nil(t, p)
		require.ErrorContains(t, err, "confidence")
	})

	t.Run("价格为负数时报错", func(t *testing.T) {
		for _, content := range []string{
			`{"name":"x","price":-1,"confidence":0.5}`,
			`{"name":"x","list_price":-1,"confidence":0.5}`,
		} {
			// Act
			p, err := parseProduct(content)

			// Assert
			require.Nil(t, p, content)
			require.ErrorContains(t, err, "negative", content)
		}
	})

	t.Run("不认识的平台改成其他，空的保持为空", func(t *testing.T) {
		// Act
		known, err1 := parseProduct(`{"name":"x","platform":" 京东 ","confidence":0.5}`)
		unknown, err2 := parseProduct(`{"name":"x","platform":"京东商城","confidence":0.5}`)
		empty, err3 := parseProduct(`{"name":"x","platform":"","confidence":0.5}`)

		// Assert
		require.NoError(t, err1)
		require.NoError(t, err2)
		require.NoError(t, err3)
		require.Equal(t, "京东", known.Platform)
		require.Equal(t, "其他", unknown.Platform)
		require.Empty(t, empty.Platform)
	})

	t.Run("过长的文字截断，列表去掉空项并限制条数", func(t *testing.T) {
		// Arrange
		long := strings.Repeat("长", maxProductNameRunes+50)
		// distinct items, so the cap is what cuts them rather than dedup
		var specs strings.Builder
		specs.WriteString(`" 国行 ",""`)
		for i := range maxProductListItems + 5 {
			_, _ = fmt.Fprintf(&specs, `,"规格%d"`, i)
		}

		// Act
		p, err := parseProduct(`{"name":"` + long + `","specs":[` + specs.String() + `],"confidence":0.5}`)

		// Assert
		require.NoError(t, err)
		require.Len(t, []rune(p.Name), maxProductNameRunes)
		require.Len(t, p.Specs, maxProductListItems)
		require.Equal(t, "国行", p.Specs[0])
	})

	t.Run("标签最多 3 个，每个不超过 35 字，去掉空的和重复的", func(t *testing.T) {
		// Arrange
		long := strings.Repeat("长", maxProductTagRunes+5)

		// Act
		p, err := parseProduct(`{"name":"x","confidence":0.5,"tags":[" 数码 ","","数码","` + long + `","手机","多余"]}`)

		// Assert
		require.NoError(t, err)
		require.Len(t, p.Tags, maxProductTags)
		require.Equal(t, "数码", p.Tags[0])
		require.Len(t, []rune(p.Tags[1]), maxProductTagRunes)
		require.Equal(t, "手机", p.Tags[2])
	})

	t.Run("缺少列表时给空列表，存进去是 [] 而不是 null", func(t *testing.T) {
		// Act
		p, err := parseProduct(`{"name":"x","confidence":0.5}`)

		// Assert
		require.NoError(t, err)
		require.NotNil(t, p.Specs)
		require.NotNil(t, p.Promotions)
		require.NotNil(t, p.Tags)
	})
}

func Test_identifyProduct(t *testing.T) {
	t.Run("把能读的截图和说明一起发给模型，按 JSON 模式要结果", func(t *testing.T) {
		// Arrange: two URLs, one of them not a file on this server
		model := &fakeLLM{replies: []*llm.ChatResp{{Content: switchJSON}}}
		pas := &PurchaseAIService{
			llmClient:       model,
			uploaderService: fakeUploader{paths: map[string]string{"http://site/uploads/post/a.png": writePNG(t)}},
		}

		// Act
		p, err := pas.identifyProduct(context.Background(),
			[]string{"http://site/uploads/post/a.png", "https://cdn.example.com/b.png"})

		// Assert
		require.NoError(t, err)
		require.Equal(t, "Switch OLED", p.Model)

		require.Len(t, model.reqs, 1)
		req := model.reqs[0]
		require.True(t, req.JSONOutput)
		require.Empty(t, req.Tools)
		require.Equal(t, llm.RoleSystem, req.Messages[0].Role)
		user := req.Messages[1]
		require.Equal(t, llm.RoleUser, user.Role)
		require.Len(t, user.Parts, 2, "the text and the one readable image")
		require.NotEmpty(t, user.Parts[0].Text)
		require.True(t, strings.HasPrefix(user.Parts[1].ImageURL, "data:image/jpeg;base64,"))
	})

	t.Run("一张都读不到时不调用模型", func(t *testing.T) {
		// Arrange
		model := &fakeLLM{}
		pas := &PurchaseAIService{llmClient: model, uploaderService: fakeUploader{}}

		// Act
		p, err := pas.identifyProduct(context.Background(), []string{"https://cdn.example.com/b.png"})

		// Assert
		require.NoError(t, err)
		require.Nil(t, p)
		require.Empty(t, model.reqs)
	})

	t.Run("模型说截图里没有商品时返回 nil，不算错误", func(t *testing.T) {
		// Arrange
		model := &fakeLLM{replies: []*llm.ChatResp{{Content: `{"name":"","confidence":0,"notes":"这是一张监控大屏截图"}`}}}
		pas := &PurchaseAIService{
			llmClient:       model,
			uploaderService: fakeUploader{paths: map[string]string{"u": writePNG(t)}},
		}

		// Act
		p, err := pas.identifyProduct(context.Background(), []string{"u"})

		// Assert
		require.NoError(t, err)
		require.Nil(t, p)
	})

	t.Run("模型输出不合法时重试，重试后合格就用重试的结果", func(t *testing.T) {
		// Arrange
		model := &fakeLLM{replies: []*llm.ChatResp{{Content: "这是一台 Switch"}, {Content: switchJSON}}}
		pas := &PurchaseAIService{
			llmClient:       model,
			uploaderService: fakeUploader{paths: map[string]string{"u": writePNG(t)}},
		}

		// Act
		p, err := pas.identifyProduct(context.Background(), []string{"u"})

		// Assert
		require.NoError(t, err)
		require.Equal(t, "Switch OLED", p.Model)
		require.Len(t, model.reqs, 2)
	})

	t.Run("每次都不合法时，重试到上限后返回解析错误", func(t *testing.T) {
		// Arrange: one invalid reply per attempt, so every attempt reaches
		// parseProduct rather than running out of script
		replies := make([]*llm.ChatResp, 0, maxIdentifyAttempts)
		for i := range maxIdentifyAttempts {
			replies = append(replies, &llm.ChatResp{Content: fmt.Sprintf("这是一台 Switch（第 %d 次）", i+1)})
		}
		model := &fakeLLM{replies: replies}
		pas := &PurchaseAIService{
			llmClient:       model,
			uploaderService: fakeUploader{paths: map[string]string{"u": writePNG(t)}},
		}

		// Act
		p, err := pas.identifyProduct(context.Background(), []string{"u"})

		// Assert
		require.Nil(t, p)
		require.ErrorContains(t, err, "not a valid JSON", "a parse error, not the fake running out of replies")
		require.Len(t, model.reqs, maxIdentifyAttempts)
	})
}

func TestPurchaseAIService_Identify(t *testing.T) {
	t.Run("AI 未配置时原样返回 400，不改写成笼统的 500", func(t *testing.T) {
		// Arrange: the client refuses before any request, as it does when AI is disabled
		model := &fakeLLM{err: pacmanerrors.BadRequest(reason.AINotConfigured)}
		pas := newProductForService(t, model)

		// Act
		resp, err := pas.Identify(context.Background(), &schema.PurchaseAIIdentifyReq{Images: []string{"u"}})

		// Assert
		require.Nil(t, resp)
		var got *pacmanerrors.Error
		require.ErrorAs(t, err, &got)
		require.Equal(t, 400, got.Code)
		require.Equal(t, reason.AINotConfigured, got.Reason)
	})

	t.Run("没有原因的错误归为 AI 请求失败", func(t *testing.T) {
		// Arrange: every reply fails to parse, which is a plain error with no reason
		replies := make([]*llm.ChatResp, 0, maxIdentifyAttempts)
		for range maxIdentifyAttempts {
			replies = append(replies, &llm.ChatResp{Content: "这是一台 Switch"})
		}
		pas := newProductForService(t, &fakeLLM{replies: replies})

		// Act
		resp, err := pas.Identify(context.Background(), &schema.PurchaseAIIdentifyReq{Images: []string{"u"}})

		// Assert
		require.Nil(t, resp)
		var got *pacmanerrors.Error
		require.ErrorAs(t, err, &got)
		require.Equal(t, 500, got.Code)
		require.Equal(t, reason.AIRequestFailed, got.Reason)
	})

	t.Run("识别成功时返回商品", func(t *testing.T) {
		// Arrange
		model := &fakeLLM{replies: []*llm.ChatResp{{Content: switchJSON}}}
		pas := newProductForService(t, model)

		// Act
		resp, err := pas.Identify(context.Background(), &schema.PurchaseAIIdentifyReq{Images: []string{"u"}})

		// Assert
		require.NoError(t, err)
		require.Equal(t, "Switch OLED", resp.Product.Model)
	})
}

// newProductForService builds a service whose screenshot "u" is readable, so a
// tests only have to script the model.
func newProductForService(t *testing.T, model *fakeLLM) *PurchaseAIService {
	t.Helper()
	return &PurchaseAIService{
		llmClient:       model,
		uploaderService: fakeUploader{paths: map[string]string{"u": writePNG(t)}},
	}
}
