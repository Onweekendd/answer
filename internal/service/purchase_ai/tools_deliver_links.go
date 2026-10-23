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
	"strings"

	"github.com/apache/answer/internal/service/llm"
	"github.com/apache/answer/internal/service/purchase_ai/agent"
	"github.com/apache/answer/internal/service/purchase_ai/links"
	"github.com/apache/answer/internal/service/purchase_ai/tool"
)

// 交付的网页里的链接。网页是模型写的，链接可能：
//   - 带着追踪参数：spm、pvid 这类可能带着共享浏览器那个账号的信息，和商品卡片一样要洗掉（cleanProductURL）
//   - 点不动：iframe 里的普通链接想在框里打开，小红书、淘宝都不许被嵌进框里，结果是白屏；要 target="_blank"
//   - 是编的，或者是网页内容里注入的：用户在我们的产物里点到它，会以为是我们核实过的
//
// 所以交付时每个 <a href> 都洗一遍、补上 target 和 rel；再核对这次是不是真的见过它
// （打开过的页面、商品卡片、收割清单），没见过的列给模型，由它去核实或者删掉，不替它删

// linkBook 记着这次任务真的见过哪些网址
type linkBook struct {
	seen     map[string]bool   // links.Key
	opened   map[string]string // links.Key → 打开时的完整网址（小红书的 xsec_token 在里面）
	products *productBook      // 商品卡片的链接也算见过
}

// plugin 装「每个工具执行完，它打开的页面记下来」：不用每个工具自己想着记；
// 最终回答里的网址缺了小红书的 xsec_token，用打开时的补上（回答是模型手抄的，常常省掉）
func (l *linkBook) plugin() agent.Plugin {
	return func(r *agent.Registry) {
		r.AfterTool(func(_ context.Context, _ llm.ToolCall, res *tool.Result) {
			if res.URL != "" && !res.Failed {
				l.see(res.URL)
			}
		})
		r.OnAnswer(func(_ context.Context, a *agent.Answer) {
			a.Text = links.CompleteText(a.Text, l.full)
		})
	}
}

// see 记下这次任务真的见过的一个网址
func (l *linkBook) see(raw string) {
	key := links.Key(raw)
	if key == "" {
		return
	}
	if l.seen == nil {
		l.seen = map[string]bool{}
	}
	l.seen[key] = true
	// 同一页的网址只留带 token 的：读页面的结果里网址可能被截短
	if clean, ok := links.Clean(raw); ok && (l.opened[key] == "" || strings.Contains(clean, "xsec_token=")) {
		if l.opened == nil {
			l.opened = map[string]string{}
		}
		l.opened[key] = clean
	}
}

// full 是这次打开 key 这一页时的完整网址（洗过的），没打开过就是空的
func (l *linkBook) full(key string) string {
	return l.opened[key]
}

// saw 说这次任务见没见过这个网址：打开过的页面、收割清单（see 记下的），还有商品卡片
func (l *linkBook) saw(key string) bool {
	if l.seen[key] {
		return true
	}
	for _, p := range l.products.cards {
		if links.Key(p.URL) == key {
			return true
		}
	}
	return false
}

// text 是交付结果里关于链接的几句话：没见过的、去掉了的、哪些商品没链上。都没有就是空的
func (l *linkBook) text(report links.Report) string {
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
	for _, p := range l.products.cards {
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
