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
	"encoding/json"
	"strings"

	"github.com/apache/answer/internal/base/reason"
	"github.com/apache/answer/internal/entity"
	"github.com/apache/answer/internal/schema"
	"github.com/apache/answer/internal/service/siteinfo_common"
	"github.com/segmentfault/pacman/errors"
	"github.com/segmentfault/pacman/log"
)

// PurchaseService purchase detail business logic: validation and schema <-> entity mapping.
// It is called by QuestionService / QuestionCommon, not by a controller.
type PurchaseService struct {
	purchaseRepo    PurchaseRepo
	siteInfoService siteinfo_common.SiteInfoCommonService
}

// NewPurchaseService new purchase service
func NewPurchaseService(
	purchaseRepo PurchaseRepo,
	siteInfoService siteinfo_common.SiteInfoCommonService,
) *PurchaseService {
	return &PurchaseService{
		purchaseRepo:    purchaseRepo,
		siteInfoService: siteInfoService,
	}
}

// Validate business rules that validate tags cannot express:
// every image URL must point at this site's own upload directory.
func (ps *PurchaseService) Validate(ctx context.Context, info *schema.PurchaseInfo) error {
	general, err := ps.siteInfoService.GetSiteGeneral(ctx)
	if err != nil {
		return err
	}
	for _, img := range info.Images {
		if !strings.HasPrefix(img, general.SiteUrl+"/uploads/") {
			return errors.BadRequest(reason.PurchaseImageNotFromSite)
		}
	}
	return nil
}

// ToEntity maps request schema -> entity. AIStatus is server-owned and never copied from the request.
func (ps *PurchaseService) ToEntity(info *schema.PurchaseInfo) *entity.PurchaseDetail {
	imagesJSON, _ := json.Marshal(info.Images)

	currency := info.Currency
	if currency == "" {
		currency = "CNY"
	}

	return &entity.PurchaseDetail{
		Price:    info.Price,
		Currency: currency,
		Link:     info.Link,
		Images:   string(imagesJSON),
	}
}

// ToSchema maps entity -> response schema.
func (ps *PurchaseService) ToSchema(detail *entity.PurchaseDetail) *schema.PurchaseInfo {
	images := make([]string, 0)
	if err := json.Unmarshal([]byte(detail.Images), &images); err != nil {
		log.Warnf("unmarshal purchase images failed: %v", err)
	}

	return &schema.PurchaseInfo{
		Images:   images,
		Price:    detail.Price,
		Currency: detail.Currency,
		Link:     detail.Link,
		AIStatus: detail.AIStatus,
	}
}

// IsChanged reports whether the incoming purchase info differs from what is stored.
// Compared after ToEntity so defaults (currency) and encoding (images JSON) match on both sides.
func (ps *PurchaseService) IsChanged(ctx context.Context, questionID string, info *schema.PurchaseInfo) (bool, error) {
	old, exist, err := ps.purchaseRepo.GetByQuestionID(ctx, questionID)
	if err != nil {
		return false, err
	}
	if !exist {
		return true, nil
	}
	incoming := ps.ToEntity(info)
	return incoming.Price != old.Price ||
		incoming.Currency != old.Currency ||
		incoming.Link != old.Link ||
		incoming.Images != old.Images, nil
}

// GetByQuestionID returns the purchase info for one question, or nil when absent.
func (ps *PurchaseService) GetByQuestionID(ctx context.Context, questionID string) (*schema.PurchaseInfo, error) {

	detail, exist, err := ps.purchaseRepo.GetByQuestionID(ctx, questionID)

	if err != nil {
		return nil, err
	}
	if !exist {
		return nil, nil
	}

	return ps.ToSchema(detail), nil
}

// BatchGetByQuestionIDs returns purchase info keyed by question ID, one query for the whole page.
func (ps *PurchaseService) BatchGetByQuestionIDs(ctx context.Context, questionIDs []string) (map[string]*schema.PurchaseInfo, error) {
	details, err := ps.purchaseRepo.BatchGetByQuestionIDs(ctx, questionIDs)
	if err != nil {
		return nil, err
	}

	result := make(map[string]*schema.PurchaseInfo, len(details))
	for qid, detail := range details {
		result[qid] = ps.ToSchema(detail)
	}

	return result, nil
}
