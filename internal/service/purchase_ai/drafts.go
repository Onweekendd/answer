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
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/apache/answer/internal/base/constant"
	"github.com/apache/answer/internal/base/reason"
	"github.com/apache/answer/internal/entity"
	"github.com/apache/answer/internal/schema"
	"github.com/apache/answer/internal/service/llm"
	"github.com/apache/answer/pkg/uid"
	"github.com/segmentfault/pacman/errors"
	"github.com/segmentfault/pacman/log"
)

// 帮用户发帖（人机协作）：用户说「帮我发到社区」，Agent 调用 draft_post 把对话整理成一篇帖子草稿。
// 这个工具不自己发帖，而是「挂起」：发一条 tool_suspend 事件，把草稿（tool_input）交给页面，
// 这次回答到此结束。用户在页面上改、发布或者放弃；发布走的是普通的发帖接口，所有发帖规则照旧。
//
// Agent 的那一票（买、再等等、不买）不交给页面决定：它记在服务端的草稿里，页面只拿到草稿 id。
// 帖子发出来以后，页面用草稿 id 请 AI 投票（VoteDraft），投的是服务端记下的那一票，改不了

// DraftStore 存草稿里不给页面改的部分。internal/repo/purchase_chat 用缓存实现，过一段时间自己过期
type DraftStore interface {
	SaveDraft(ctx context.Context, id string, draft *Draft) error
	GetDraft(ctx context.Context, id string) (draft *Draft, exist bool, err error)
	DeleteDraft(ctx context.Context, id string) error
}

// Draft 是服务端记下的草稿：谁的、AI 投哪一票
type Draft struct {
	UserID string `json:"user_id"`
	Vote   int    `json:"vote"` // entity.PurchaseVoteBuy / Wait / Skip
}

// DraftInput 是交给页面的草稿（tool_suspend 的 input）。字段和
// frontend/web/src/core/chat/mapper.ts 的 PostDraft 对应
type DraftInput struct {
	DraftID    string   `json:"draftId"`
	Title      string   `json:"title"`
	Content    string   `json:"content"`
	Images     []string `json:"images"`     // 选中的配图，第一张是封面
	Candidates []string `json:"candidates"` // 这段对话里所有能用的图，页面上可以换
	Price      float64  `json:"price"`
	Link       string   `json:"link,omitempty"`
	Category   string   `json:"category,omitempty"`
	Vote       int      `json:"vote"` // 只给页面显示「AI 会投：再等等」
}

type draftArgs struct {
	Title    string   `json:"title" description:"帖子标题，6 到 60 个字：要买什么、在纠结什么，比如「iPad Air M3 现在入手还是等双 11？」"`
	Content  string   `json:"content" description:"帖子正文，Markdown，写给社区里帮忙投票的人看：用户的需求和纠结点、查到的价格和出处、几个选择各自的好坏。不要写买不买的结论，留给大家投。用户的用途、预算这些只写对话里用户说过的，没说的不要编"`
	Images   []string `json:"images" description:"配图的网址，1 到 5 张，第一张是封面。只能用这段对话里出现过的图：用户上传的截图、回答里的图（[图N] 换成的那些）、商品卡片的图"`
	Price    float64  `json:"price,omitempty" description:"商品价格（元），用查到的到手价；不知道就填 0"`
	Link     string   `json:"link,omitempty" description:"商品链接，只用商品卡片里的链接；没有就不填"`
	Category string   `json:"category,omitempty" description:"品类，一个词，比如 数码、家电、家居、服饰、母婴"`
	Vote     string   `json:"vote" enum:"buy,wait,skip" description:"你的看法：帖子发出来以后你会以 AI 的身份投第一票。buy 买，wait 再等等，skip 不买"`
}

const (
	draftTitleMinRunes   = 6 // 发帖接口要求标题至少 6 个字
	draftTitleMaxRunes   = 60
	draftContentMaxRunes = 3000
	draftCategoryRunes   = 20
	draftMaxImages       = 5 // 发帖接口最多 5 张图
	draftMaxCandidates   = 12
)

var draftVotes = map[string]int{"buy": entity.PurchaseVoteBuy, "wait": entity.PurchaseVoteWait, "skip": entity.PurchaseVoteSkip}

// draftContext 是对话里整理草稿要用的东西；只有对话页的 Agent 有，帖子分析和 case 运行器没有
type draftContext struct {
	userID     string
	candidates []string // 对话里出现过的图，按出现的先后
	store      DraftStore
}

// uploadedImage 认出回答里本站存的图：Agent 存的（uploads/agent）和用户上传的（uploads/post）
var uploadedImage = regexp.MustCompile(`https?://[^\s()"'<>]+/uploads/(?:agent|post)/[0-9A-Za-z._-]+`)

// draftCandidates 是对话里能当配图的图：用户上传的截图，和回答里出现过的本站的图，去重，最多 draftMaxCandidates 张
func draftCandidates(path []Entry, latest *schema.PurchaseAIChatMessage) []string {
	out := []string{} // 不是 nil：nil 会编成 JSON 的 null，页面会把整份草稿当成坏的
	add := func(url string) {
		if len(out) < draftMaxCandidates && !slices.Contains(out, url) {
			out = append(out, url)
		}
	}
	for _, e := range path {
		switch {
		case e.Message != nil && e.Message.Role == llm.RoleUser && !e.Message.Synthetic:
			// 用户上传的截图
			for _, p := range e.Message.Parts {
				if p.Image != "" {
					add(p.Image)
				}
			}
		case e.View != nil:
			// 回答里的图：页面上的回答已经把 [图N] 换成了存好的图片网址
			for _, ev := range e.View.Events {
				if ev.Type == EventText {
					for _, url := range uploadedImage.FindAllString(ev.Delta, -1) {
						add(url)
					}
				}
			}
		}
	}
	if latest != nil {
		for _, url := range latest.Images {
			add(url)
		}
	}
	return out
}

// draftPost 把模型写的草稿整理好、存下 AI 的那一票，再挂起：交给页面，这次回答结束
func (r *agentRun) draftPost(ctx context.Context, arguments string) (toolResult, error) {
	var args draftArgs
	if res := decodeArgs(arguments, &args, `{"title": "...", "content": "...", "images": ["..."], "vote": "wait"}`); res != nil {
		return *res, nil
	}
	title := oneLine(args.Title)
	if n := utf8.RuneCountInString(title); n < draftTitleMinRunes || n > draftTitleMaxRunes {
		return toolResult{Failed: true, Text: fmt.Sprintf("参数无效：title 要 %d 到 %d 个字。", draftTitleMinRunes, draftTitleMaxRunes)}, nil
	}
	vote, ok := draftVotes[args.Vote]
	if !ok {
		return toolResult{Failed: true, Text: "参数无效：vote 只能是 buy、wait、skip。"}, nil
	}
	content := strings.TrimSpace(args.Content)
	if clipped := clipRunes(content, draftContentMaxRunes); clipped != content {
		content = clipped + "…"
	}
	candidates := r.draftCandidates()
	input := DraftInput{
		DraftID:    uid.IDStr12(),
		Title:      r.view(title),
		Content:    r.view(content),
		Images:     pickImages(args.Images, candidates),
		Candidates: candidates,
		Price:      max(args.Price, 0),
		Category:   r.view(clipRunes(oneLine(args.Category), draftCategoryRunes)),
		Vote:       vote,
	}
	if link, ok := cleanProductURL(args.Link); ok && isProductPage(link) {
		input.Link = link
	}

	if err := r.draft.store.SaveDraft(ctx, input.DraftID, &Draft{UserID: r.draft.userID, Vote: vote}); err != nil {
		log.Errorf("purchase ai: cannot keep the draft: %v", err)
		return toolResult{Failed: true, Text: "草稿没存成，告诉用户稍后再试。"}, nil
	}
	r.emit(ctx, Event{Type: EventToolSuspend, ID: r.callID, Name: toolDraft, Input: &input})
	return toolResult{
		Text:    "草稿已经交给用户确认。",
		Display: "草稿交给你确认",
		Done:    true,
		Answer:  draftAnswer,
		Success: true,
	}, nil
}

// draftAnswer 是挂起以后这次的回答：草稿就在它下面
const draftAnswer = "我把这次的讨论整理成了一篇帖子草稿，就在下面。改好以后点「发布」，帖子会发到社区请大家投票，我会先投一票。"

// draftCandidates 是对话里的图加上这一次的商品图。这一次截的图还没存（回答引用了才存），不算
func (r *agentRun) draftCandidates() []string {
	out := append([]string{}, r.draft.candidates...)
	add := func(url string) {
		if url != "" && len(out) < draftMaxCandidates && !slices.Contains(out, url) {
			out = append(out, url)
		}
	}
	for _, p := range r.products {
		add(p.Image)
	}
	return out
}

// pickImages 是模型挑的配图里确实在对话中出现过的那些（模型抄网址可能抄错）；一张都不对就用前几张
func pickImages(picked, candidates []string) []string {
	out := []string{}
	for _, url := range picked {
		url = strings.TrimSpace(url)
		if slices.Contains(candidates, url) && !slices.Contains(out, url) && len(out) < draftMaxImages {
			out = append(out, url)
		}
	}
	if len(out) == 0 {
		out = append(out, candidates[:min(len(candidates), 3)]...)
	}
	return out
}

// VoteDraft 在用户用草稿发出帖子以后，以 AI 的身份投草稿里记下的那一票。一份草稿只投一次。
// 草稿过期了、不是这个用户的、帖子不是他发的，都不投
func (pas *PurchaseAIService) VoteDraft(ctx context.Context, req *schema.PurchaseDraftVoteReq) error {
	draft, exist, err := pas.drafts.GetDraft(ctx, req.DraftID)
	if err != nil {
		return err
	}
	if !exist || draft.UserID != req.UserID {
		return errors.NotFound(reason.ObjectNotFound)
	}
	question, err := pas.questionCommon.Info(ctx, req.QuestionID, req.UserID)
	if err != nil {
		return err
	}
	if question.UserID != req.UserID {
		return errors.Forbidden(reason.ForbiddenError)
	}
	botID, err := pas.configService.GetStringValue(ctx, constant.AIBotUserIDKey)
	if err != nil {
		return err
	}
	if botID == "" {
		return errors.InternalServer(reason.AIRequestFailed).WithError(fmt.Errorf("no ai bot user configured"))
	}
	if _, err := pas.voteService.Vote(ctx, &schema.PurchaseVoteReq{
		QuestionID: req.QuestionID,
		Choice:     draft.Vote,
		UserID:     botID,
	}); err != nil {
		return err
	}
	if err := pas.drafts.DeleteDraft(ctx, req.DraftID); err != nil {
		log.Warnf("purchase ai: cannot delete a used draft: %v", err)
	}
	return nil
}
