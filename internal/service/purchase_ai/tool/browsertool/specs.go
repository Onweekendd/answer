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

package browsertool

import "github.com/apache/answer/internal/service/purchase_ai/tool"

// 浏览器工具的名字：告诉模型的，也是日志、事件里记的
const (
	NameOpen     = "open"
	NameNavigate = "navigate"
	NameSnapshot = "snapshot"
	NameRead     = "read"
	NameShot     = "screenshot"
	NameClick    = "click"
	NameHover    = "hover"
	NameType     = "type"
	NamePress    = "press"
	NameScroll   = "scroll"
)

// Specs 是浏览器工具，按名字查。顺序由用它的人定：工具定义在请求的最前面，顺序一变前缀缓存就全部失效
func (s *TaskState) Specs() map[string]tool.Spec {
	list := []tool.Spec{
		{Name: NameOpen, Description: "在后台新开一个标签页打开网址，之后的操作都在这个标签页里进行。任务开始时用它打开第一个网页。",
			Args: urlArgs{}, Run: s.Open},
		{Name: NameNavigate, Description: "在当前标签页里打开网址。想回到之前看过的页面时，navigate 到之前记下的网址。",
			Args: urlArgs{}, Run: s.Navigate},
		{Name: NameSnapshot, Description: "查看当前页面上可以操作的元素（链接、按钮、输入框、选项卡、菜单项），每行一个，开头的 [数字] 是编号，商品卡片的标题和价格也在里面。" +
			"打开网页、悬停、点击、输入并回车、滚动以后页面都会变，要先 snapshot 再决定下一步：编号只在最新一份快照里有效。",
			Args: struct{}{}, Run: s.Snapshot},
		{Name: NameRead, Description: "读页面上的文字：文章和笔记的正文、评论、商品详情里的说明。快照只列能操作的元素，这些段落文字不在快照里，要用 read 读。" +
			"页面上开着浮层（比如小红书的笔记）时只读浮层里的；填 index 读那个元素的全部文字（快照里每个元素最多显示 120 个字）。一次最多读 3000 字。",
			Args: readArgs{}, Run: s.Read},
		// 什么时候该截图；整屏和只截元素各适合看什么，见 screenshotMaxWidth 上面的实测
		{Name: NameShot, Description: "截图，看页面实际长什么样，用来确认快照里看不到的东西：哪个选项被选中了、有没有弹窗挡住、页面是不是白屏或者还在加载、动作做完有没有生效。" +
			"不填 index 截当前这一屏，适合看布局和状态，小字看不清；要读价格、优惠券、月销这类小字，填 index 只截那张商品卡片。先用快照，快照说不清楚再截图。" +
			"每张图都编成「图N」，能给结论作证的（价格和优惠的条件、参数表、差评）可以在回答里引用给用户看，不用再截一次。",
			Args: screenshotArgs{}, Run: s.Screenshot},
		{Name: NameClick, Description: "点击最新一份快照里编号为 index 的元素。", Args: indexArgs{}, Run: s.Click},
		{Name: NameHover, Description: "将鼠标悬停在最新快照里编号为 index 的元素上，不点击。用于展开下拉菜单；悬停后先 snapshot 查看新出现的选项，再 click 选项。",
			Args: indexArgs{}, Run: s.Hover},
		{Name: NameType, Description: "往编号为 index 的输入框里填文字。submit 为 true 时填完按回车，比如在搜索框里搜索。", Args: typeArgs{}, Run: s.Type},
		{Name: NamePress, Description: "按键盘上的一个键。Escape：关闭弹窗、浮层、下拉菜单（比如小红书的笔记浮层），比重新打开网页快，按完先 snapshot 再继续。" +
			"ArrowRight、ArrowLeft：把轮播图翻到下一张、上一张（比如小红书笔记的图片），页面别的地方不变，编号照样能用，翻完直接截图。",
			Args: pressArgs{}, Run: s.Press},
		{Name: NameScroll, Description: "上下滚动页面，看到更多内容，也会触发页面加载更多商品。快照最后会写着上面和下面还有几屏。",
			Args: scrollArgs{}, Run: s.Scroll},
	}
	specs := make(map[string]tool.Spec, len(list))
	for _, spec := range list {
		specs[spec.Name] = spec
	}
	return specs
}
