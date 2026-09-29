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

package schema

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/apache/answer/internal/base/validator"
	"github.com/segmentfault/pacman/i18n"
	"github.com/stretchr/testify/require"
)

// TestPurchaseInfoProductValidation checks a product submitted with a post, the
// way BindAndCheck does: JSON in, then the site's validator.
func TestPurchaseInfoProductValidation(t *testing.T) {
	const post = `{"images":["http://localhost:8081/uploads/post/a.jpg"],"price":0%s}`
	long := func(n int) string { return strings.Repeat("长", n) }

	tests := []struct {
		name    string
		product string // the "product" member, or "" for none
		wantErr bool
	}{
		{name: "不带商品", product: "", wantErr: false},
		{name: "商品为 null", product: `null`, wantErr: false},
		{
			name: "识别接口返回的完整商品",
			product: `{"name":"Apple iPhone 17 512GB","brand":"苹果","model":"iPhone 17","tags":["数码","手机"],
				"specs":["512GB"],"platform":"天猫","shop":"","official":null,"price":6520,"list_price":7999,
				"promotions":["领政府补贴省 500"],"confidence":0.72,"notes":""}`,
			wantErr: false,
		},
		{name: "只有名称", product: `{"name":"x"}`, wantErr: false},
		{name: "价格为 0", product: `{"name":"x","price":0}`, wantErr: false},
		{name: "没有名称", product: `{"name":""}`, wantErr: true},
		{name: "名称只有空格", product: `{"name":"   "}`, wantErr: true},
		{name: "名称过长", product: `{"name":"` + long(201) + `"}`, wantErr: true},
		{name: "标签超过 3 个", product: `{"name":"x","tags":["a","b","c","d"]}`, wantErr: true},
		{name: "标签过长", product: `{"name":"x","tags":["` + long(36) + `"]}`, wantErr: true},
		{name: "标签有空字符串", product: `{"name":"x","tags":[""]}`, wantErr: true},
		{name: "规格超过 10 个", product: `{"name":"x","specs":["1","2","3","4","5","6","7","8","9","10","11"]}`, wantErr: true},
		{name: "不认识的平台", product: `{"name":"x","platform":"京东商城"}`, wantErr: true},
		{name: "价格为负数", product: `{"name":"x","price":-1}`, wantErr: true},
		{name: "原价为负数", product: `{"name":"x","list_price":-1}`, wantErr: true},
		{name: "置信度大于 1", product: `{"name":"x","confidence":90}`, wantErr: true},
		{name: "备注过长", product: `{"name":"x","notes":"` + long(301) + `"}`, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			member := ""
			if tt.product != "" {
				member = `,"product":` + tt.product
			}
			info := &PurchaseInfo{}
			require.NoError(t, json.Unmarshal([]byte(strings.Replace(post, "%s", member, 1)), info))

			// Act
			_, err := validator.GetValidatorByLang(i18n.DefaultLanguage).Check(info)

			// Assert
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
