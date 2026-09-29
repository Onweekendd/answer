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
	"encoding/json"
	"fmt"
	"strings"

	"github.com/apache/answer/internal/base/reason"
	"github.com/apache/answer/internal/schema"
	"github.com/apache/answer/internal/service/llm"
	"github.com/apache/answer/pkg/imgprep"
	"github.com/segmentfault/pacman/errors"
	"github.com/segmentfault/pacman/log"
)

// limits on what a reading may hold: it is stored with the post and quoted in
// later prompts, and a model now and then runs on and on in one field
const (
	maxProductNameRunes  = 200
	maxProductFieldRunes = 100
	maxProductNotesRunes = 300
	maxProductListItems  = 10
	maxIdentifyAttempts  = 2
	maxProductTags       = 3
	// TagItem.DisplayName is lte=35, so a longer tag would be rejected when
	// the post is submitted
	maxProductTagRunes = 35
)

// imageParts reads the post's screenshots from disk into image parts for the
// vision model. An image that cannot be read is logged and skipped, so one
// broken upload does not cost the others; with none readable the result is
// empty and the caller decides what that means.
func (pas *PurchaseAIService) imageParts(ctx context.Context, imageURLs []string) []llm.Part {
	parts := make([]llm.Part, 0, len(imageURLs))
	for _, imageURL := range imageURLs {
		localPath, err := pas.uploaderService.PostImagePath(ctx, imageURL)
		if err != nil {
			log.Warnf("purchase ai: skip image %s: %v", imageURL, err)
			continue
		}

		dataURI, err := imgprep.DataURI(localPath, imgprep.DefaultMaxSide)
		if err != nil {
			log.Warnf("purchase ai: skip image %s: %v", imageURL, err)
			continue
		}

		parts = append(parts, llm.Part{ImageURL: dataURI})
	}
	return parts
}

// productFor returns the product a post is about: the stored one when there is
// one, otherwise one recognized from the screenshots now and saved for next
// time. It returns nil rather than an error when there is none, because the
// analysis works from the title and description without it, only less exactly;
// a failed recognition should cost precision, not the whole analysis.
func (pas *PurchaseAIService) productFor(ctx context.Context, questionID string, purchase *schema.PurchaseInfo) *schema.PurchaseProduct {
	if purchase == nil {
		return nil
	}
	if purchase.Product != nil {
		return purchase.Product
	}

	product, err := pas.identifyProduct(ctx, purchase.Images)
	if err != nil {
		log.Warnf("purchase ai: identify product failed, questionID: %s, err: %v", questionID, err)
		return nil
	}
	if product == nil {
		return nil
	}

	// a failed save only means the next analysis recognizes it again, so this
	// one still uses what it has
	if err := pas.purchaseService.SaveProduct(ctx, questionID, product); err != nil {
		log.Warnf("purchase ai: save product failed, questionID: %s, err: %v", questionID, err)
	}
	return product
}

// Identify reads the product off screenshots the user has just uploaded, so
// the post form can offer to fill in the price and tags. Nothing is saved: the
// post does not exist yet, and the form submits the reading along with it.
func (pas *PurchaseAIService) Identify(ctx context.Context, req *schema.PurchaseAIIdentifyReq) (*schema.PurchaseAIIdentifyResp, error) {
	product, err := pas.identifyProduct(ctx, req.Images)

	if err != nil {
		return nil, errors.InternalServer(reason.AIRequestFailed).WithError(err).WithStack()
	}

	return &schema.PurchaseAIIdentifyResp{
		Product: product,
	}, nil
}

func (pas *PurchaseAIService) identifyProduct(ctx context.Context, imageURLs []string) (*schema.PurchaseProduct, error) {
	if len(imageURLs) == 0 {
		return nil, nil
	}

	imageParts := pas.imageParts(ctx, imageURLs)
	if len(imageParts) == 0 {
		return nil, nil
	}

	messages := []llm.Message{
		llm.Text(llm.RoleSystem, buildIdentifyPrompt()),
		{
			Role:  llm.RoleUser,
			Parts: append([]llm.Part{{Text: "以下是用户上传的商品截图，按上传顺序排列。"}}, imageParts...),
		},
	}

	req := llm.ChatReq{
		Messages: messages,
	}

	product, err := llm.ChatJSON(ctx, pas.llmClient, &req, maxIdentifyAttempts, parseProduct)
	if err != nil {
		return nil, err
	}

	// valid JSON with no name is the model saying the screenshots show no
	// product, which is an answer, not a failure
	if product.Name == "" {
		return nil, nil
	}
	return product, nil
}

// productPlatforms are the platform values the identify prompt allows.
var productPlatforms = map[string]bool{"淘宝": true, "天猫": true, "京东": true, "拼多多": true, "其他": true}

// parseProduct turns the model's reply into a product and checks it. Small
// slips it can fix safely it fixes: an unknown platform becomes 其他, overlong
// text is cut. What it cannot fix without guessing, it returns as an error,
// worded so the model can be told what to correct.
func parseProduct(content string) (*schema.PurchaseProduct, error) {
	var p schema.PurchaseProduct
	if err := json.Unmarshal([]byte(stripCodeFence(content)), &p); err != nil {
		return nil, fmt.Errorf("the reply is not a valid JSON object: %w", err)
	}

	// a model asked for 0 to 1 sometimes answers 90 meaning 90%, or makes a
	// number up; dividing by 100 would guess which, so it is sent back
	if p.Confidence < 0 || p.Confidence > 1 {
		return nil, fmt.Errorf("confidence must be between 0 and 1, got %v", p.Confidence)
	}
	if p.Price != nil && *p.Price < 0 {
		return nil, fmt.Errorf("price must not be negative, got %v", *p.Price)
	}
	if p.ListPrice != nil && *p.ListPrice < 0 {
		return nil, fmt.Errorf("list_price must not be negative, got %v", *p.ListPrice)
	}

	p.Name = clipRunes(strings.TrimSpace(p.Name), maxProductNameRunes)
	p.Brand = clipRunes(strings.TrimSpace(p.Brand), maxProductFieldRunes)
	p.Model = clipRunes(strings.TrimSpace(p.Model), maxProductFieldRunes)
	p.Shop = clipRunes(strings.TrimSpace(p.Shop), maxProductFieldRunes)
	p.Notes = clipRunes(strings.TrimSpace(p.Notes), maxProductNotesRunes)
	p.Specs = cleanList(p.Specs, maxProductListItems, maxProductFieldRunes)
	p.Tags = cleanList(p.Tags, maxProductTags, maxProductTagRunes)
	p.Promotions = cleanList(p.Promotions, maxProductListItems, maxProductFieldRunes)

	p.Platform = strings.TrimSpace(p.Platform)
	if p.Platform != "" && !productPlatforms[p.Platform] {
		p.Platform = "其他"
	}
	return &p, nil
}

// stripCodeFence removes a ```json ... ``` wrapper. The prompt asks for bare
// JSON, but some models wrap it anyway.
func stripCodeFence(s string) string {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "```") {
		return s
	}
	// drop the opening line, which may name the language
	if i := strings.Index(s, "\n"); i >= 0 {
		s = s[i+1:]
	} else {
		s = strings.TrimPrefix(s, "```")
	}
	return strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(s), "```"))
}

// cleanList trims each item, cuts it to maxRunes, drops empty and repeated
// ones and keeps at most maxItems. It never returns nil, so the stored JSON
// says [] rather than null.
func cleanList(items []string, maxItems, maxRunes int) []string {
	out := make([]string, 0, min(len(items), maxItems))
	seen := make(map[string]bool, len(items))
	for _, item := range items {
		if len(out) == maxItems {
			break
		}
		item = clipRunes(strings.TrimSpace(item), maxRunes)
		if item == "" || seen[item] {
			continue
		}
		seen[item] = true
		out = append(out, item)
	}
	return out
}

// clipRunes cuts s to at most max runes. Unlike truncateRunes it adds no note,
// because the result is stored data rather than text for a model.
func clipRunes(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max])
}
