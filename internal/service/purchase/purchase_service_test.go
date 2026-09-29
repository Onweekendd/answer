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

package purchase

import (
	"context"
	"errors"
	"testing"

	"github.com/apache/answer/internal/base/reason"
	"github.com/apache/answer/internal/entity"
	"github.com/apache/answer/internal/schema"
	"github.com/apache/answer/internal/service/mock"
	myErrors "github.com/segmentfault/pacman/errors"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

// requireReason asserts the error is a pacman error carrying the given reason key.
// The message itself is empty until the translator fills it in, so comparing
// err.Error() would compare nothing.
func requireReason(t *testing.T, err error, want string) {
	t.Helper()

	var pacmanErr *myErrors.Error
	require.True(t, errors.As(err, &pacmanErr), "expected a pacman error, got %v", err)
	require.Equal(t, want, pacmanErr.Reason)
}

const testSiteURL = "http://localhost:8081"

// newServiceWithSite builds a PurchaseService whose site URL is fixed, so the
// image check has something stable to compare against. purchaseRepo is left nil:
// the methods under test here never touch the database.
func newServiceWithSite(t *testing.T) *PurchaseService {
	t.Helper()

	ctrl := gomock.NewController(t)
	siteInfoService := mock.NewMockSiteInfoCommonService(ctrl)
	siteInfoService.EXPECT().GetSiteGeneral(gomock.Any()).
		Return(&schema.SiteGeneralResp{SiteUrl: testSiteURL}, nil).AnyTimes()

	return NewPurchaseService(nil, siteInfoService)
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		images  []string
		wantErr bool
	}{
		{
			name:    "本站上传目录下的图片",
			images:  []string{testSiteURL + "/uploads/post/a.png"},
			wantErr: false,
		},
		{
			name:    "多张都在本站上传目录下",
			images:  []string{testSiteURL + "/uploads/post/a.png", testSiteURL + "/uploads/post/b.png"},
			wantErr: false,
		},
		{
			name:    "外站图片",
			images:  []string{"https://evil.com/x.png"},
			wantErr: true,
		},
		{
			name:    "本站但不在上传目录下",
			images:  []string{testSiteURL + "/avatar/a.png"},
			wantErr: true,
		},
		{
			name:    "多张里混了一张外链",
			images:  []string{testSiteURL + "/uploads/post/a.png", "https://evil.com/x.png"},
			wantErr: true,
		},
		{
			// 前缀里带上了 "/uploads/"，所以 "…8081.evil.com/uploads/…" 也匹配不上。
			name:    "以本站地址开头的另一个域名",
			images:  []string{testSiteURL + ".evil.com/uploads/post/a.png"},
			wantErr: true,
		},
		{
			// 数量由 schema 的 validate:"min=1" 在 BindAndCheck 阶段拦截，
			// Validate 只管来源，空切片直接跳过循环。
			name:    "空数组由 schema 层拦截，这里放行",
			images:  nil,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			ps := newServiceWithSite(t)
			info := &schema.PurchaseInfo{Images: tt.images}

			// Act
			err := ps.Validate(context.Background(), info)

			// Assert
			if tt.wantErr {
				requireReason(t, err, reason.PurchaseImageNotFromSite)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestToEntity(t *testing.T) {
	t.Run("币种为空时填默认值 CNY 且不改写入参", func(t *testing.T) {
		// Arrange
		ps := newServiceWithSite(t)
		info := &schema.PurchaseInfo{
			Images:   []string{testSiteURL + "/uploads/post/a.png"},
			Price:    5499,
			Currency: "",
			Link:     "https://item.jd.com/1.html",
		}

		// Act
		detail := ps.ToEntity(info)

		// Assert
		require.Equal(t, "CNY", detail.Currency)
		require.Equal(t, "", info.Currency, "入参不应该被改写")
		require.Equal(t, 5499.0, detail.Price)
		require.Equal(t, "https://item.jd.com/1.html", detail.Link)
	})

	t.Run("传了币种就用传的", func(t *testing.T) {
		ps := newServiceWithSite(t)

		detail := ps.ToEntity(&schema.PurchaseInfo{Currency: "USD"})

		require.Equal(t, "USD", detail.Currency)
	})

	t.Run("images 序列化成 JSON 字符串", func(t *testing.T) {
		ps := newServiceWithSite(t)

		detail := ps.ToEntity(&schema.PurchaseInfo{Images: []string{"a", "b"}})

		require.Equal(t, `["a","b"]`, detail.Images)
	})

	t.Run("ai_status 由服务端掌握，不从请求里带过来", func(t *testing.T) {
		ps := newServiceWithSite(t)

		detail := ps.ToEntity(&schema.PurchaseInfo{AIStatus: entity.PurchaseAIStatusFinished})

		require.Equal(t, entity.PurchaseAIStatusNone, detail.AIStatus)
	})
}

func TestToSchema(t *testing.T) {
	t.Run("entity 转回 schema", func(t *testing.T) {
		// Arrange
		ps := newServiceWithSite(t)
		detail := &entity.PurchaseDetail{
			Images:   `["http://x/a.png","http://x/b.png"]`,
			Price:    5499,
			Currency: "CNY",
			Link:     "https://item.jd.com/1.html",
			AIStatus: entity.PurchaseAIStatusFinished,
		}

		// Act
		got := ps.ToSchema(detail)

		// Assert
		require.Equal(t, []string{"http://x/a.png", "http://x/b.png"}, got.Images)
		require.Equal(t, 5499.0, got.Price)
		require.Equal(t, "CNY", got.Currency)
		require.Equal(t, "https://item.jd.com/1.html", got.Link)
		require.Equal(t, entity.PurchaseAIStatusFinished, got.AIStatus)
	})

	t.Run("images 不是合法 JSON 时返回空切片而不 panic", func(t *testing.T) {
		ps := newServiceWithSite(t)

		got := ps.ToSchema(&entity.PurchaseDetail{Images: "not json"})

		require.NotNil(t, got)
		require.Empty(t, got.Images)
	})

	t.Run("product 为空字符串时是 nil，表示还没识别", func(t *testing.T) {
		ps := newServiceWithSite(t)

		got := ps.ToSchema(&entity.PurchaseDetail{Images: `[]`, Product: ""})

		require.Nil(t, got.Product)
	})

	t.Run("product 的 JSON 解析成结构体", func(t *testing.T) {
		ps := newServiceWithSite(t)

		got := ps.ToSchema(&entity.PurchaseDetail{
			Images:  `[]`,
			Product: `{"name":"任天堂 Switch OLED","price":2099,"list_price":null}`,
		})

		require.NotNil(t, got.Product)
		require.Equal(t, "任天堂 Switch OLED", got.Product.Name)
		require.Equal(t, 2099.0, *got.Product.Price)
		require.Nil(t, got.Product.ListPrice)
	})

	t.Run("product 不是合法 JSON 时是 nil 而不 panic", func(t *testing.T) {
		ps := newServiceWithSite(t)

		got := ps.ToSchema(&entity.PurchaseDetail{Images: `[]`, Product: "not json"})

		require.Nil(t, got.Product)
	})
}

// recordingRepo keeps the last product written. Embedding the interface
// satisfies the methods these tests never call.
type recordingRepo struct {
	PurchaseRepo
	questionID string
	product    string
}

func (r *recordingRepo) UpdateProduct(_ context.Context, questionID, product string) error {
	r.questionID = questionID
	r.product = product
	return nil
}

func TestSaveProduct(t *testing.T) {
	t.Run("存成 JSON，读回来和原来一样", func(t *testing.T) {
		// Arrange
		repo := &recordingRepo{}
		ps := NewPurchaseService(repo, nil)
		price := 2099.0
		product := &schema.PurchaseProduct{Name: "任天堂 Switch OLED", Price: &price, Specs: []string{"国行"}}

		// Act
		err := ps.SaveProduct(context.Background(), "q1", product)

		// Assert
		require.NoError(t, err)
		require.Equal(t, "q1", repo.questionID)
		got := ps.ToSchema(&entity.PurchaseDetail{Images: `[]`, Product: repo.product})
		require.Equal(t, product, got.Product)
	})

	t.Run("nil 存成空字符串，而不是 JSON 的 null", func(t *testing.T) {
		// Arrange
		repo := &recordingRepo{product: "old"}
		ps := NewPurchaseService(repo, nil)

		// Act
		err := ps.SaveProduct(context.Background(), "q1", nil)

		// Assert
		require.NoError(t, err)
		require.Equal(t, "", repo.product)
	})
}

func TestToEntityToSchemaRoundTrip(t *testing.T) {
	// Arrange
	ps := newServiceWithSite(t)
	info := &schema.PurchaseInfo{
		Images: []string{testSiteURL + "/uploads/post/a.png", testSiteURL + "/uploads/post/b.png"},
		Price:  5499,
		Link:   "https://item.jd.com/1.html",
	}

	// Act
	got := ps.ToSchema(ps.ToEntity(info))

	// Assert
	require.Equal(t, info.Images, got.Images)
	require.Equal(t, info.Price, got.Price)
	require.Equal(t, info.Link, got.Link)
	require.Equal(t, "CNY", got.Currency, "空币种在 ToEntity 时被补成默认值")
}
