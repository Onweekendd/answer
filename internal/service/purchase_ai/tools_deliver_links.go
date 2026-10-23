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
	"fmt"
	"strings"

	"github.com/apache/answer/internal/service/purchase_ai/links"
)

// 交付的网页里的链接。网页是模型写的，链接可能：
//   - 带着追踪参数：spm、pvid 这类可能带着共享浏览器那个账号的信息，和商品卡片一样要洗掉（cleanProductURL）
//   - 点不动：iframe 里的普通链接想在框里打开，小红书、淘宝都不许被嵌进框里，结果是白屏；要 target="_blank"
//   - 是编的，或者是网页内容里注入的：用户在我们的产物里点到它，会以为是我们核实过的
//
// 所以交付时每个 <a href> 都洗一遍、补上 target 和 rel；再核对这次是不是真的见过它
// （打开过的页面、商品卡片、收割清单），没见过的列给模型，由它去核实或者删掉，不替它删

// seeLink 记下这次任务真的见过的一个网址
func (r *agentRun) seeLink(raw string) {
	key := links.Key(raw)
	if key == "" {
		return
	}
	if r.seenLinks == nil {
		r.seenLinks = map[string]bool{}
	}
	r.seenLinks[key] = true
}

// sawLink 说这次任务见没见过这个网址：打开过的页面、收割清单（seeLink 记下的），还有商品卡片
func (r *agentRun) sawLink(key string) bool {
	if r.seenLinks[key] {
		return true
	}
	for _, p := range r.products {
		if links.Key(p.URL) == key {
			return true
		}
	}
	return false
}

// linkText 是交付结果里关于链接的几句话：没见过的、去掉了的、哪些商品没链上。都没有就是空的
func (r *agentRun) linkText(report links.Report) string {
	var b strings.Builder
	if len(report.Unseen) > 0 {
		fmt.Fprintf(&b, "\n这些链接这次没打开过：%s。网页里的链接要是你真的打开过的页面、商品卡片或者收割清单里的："+
			"去打开核实一下，打不开或者不是那回事就删掉，改完再交付。", strings.Join(firstN(report.Unseen, 5), "、"))
	}
	if len(report.Dropped) > 0 {
		fmt.Fprintf(&b, "\n去掉了 %d 个点不了的链接（不是 http(s) 的，或者是广告跳转）：%s。", len(report.Dropped),
			strings.Join(firstN(report.Dropped, 3), "、"))
	}
	var missing []string
	for _, p := range r.products {
		if !report.Keys[links.Key(p.URL)] {
			missing = append(missing, fmt.Sprintf("%d. %s %s", p.ID, p.Title, p.URL))
		}
	}
	if len(missing) > 0 {
		fmt.Fprintf(&b, "\n网页里没链上这些商品卡片：%s。网页里提到它们的地方链到这些商品页，用户才能点过去核实；没提到的不用管。",
			strings.Join(missing, "；"))
	}
	return b.String()
}
