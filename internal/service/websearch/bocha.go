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

package websearch

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/segmentfault/pacman/log"
)

const (
	bochaEndpoint     = "https://api.bochaai.com/v1/web-search"
	bochaTimeout      = 15 * time.Second
	bochaDefaultCount = 5
	// bochaErrorBodyLimit caps how much of a failed response goes into the error
	bochaErrorBodyLimit = 512
)

// bochaProvider calls the Bocha web search API (bochaai.com).
type bochaProvider struct {
	apiKey     string
	httpClient *http.Client
	// endpoint is a field rather than the constant so a test can point it at
	// an httptest server.
	endpoint string
}

func newBocha(apiKey string) *bochaProvider {
	return &bochaProvider{
		apiKey:     apiKey,
		httpClient: &http.Client{Timeout: bochaTimeout},
		endpoint:   bochaEndpoint,
	}
}

// wire format of the Bocha API, only the fields we read

type bochaRequest struct {
	Query     string `json:"query"`
	Freshness string `json:"freshness,omitempty"`
	Summary   bool   `json:"summary"`
	Count     int    `json:"count,omitempty"`
}

type bochaResponse struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data struct {
		WebPages struct {
			Value []bochaPage `json:"value"`
		} `json:"webPages"`
	} `json:"data"`
}

type bochaPage struct {
	Name          string `json:"name"`
	URL           string `json:"url"`
	SiteName      string `json:"siteName"`
	Snippet       string `json:"snippet"`
	Summary       string `json:"summary"`
	DatePublished string `json:"datePublished"` // RFC 3339, e.g. 2025-11-12T02:17:06+08:00
}

// bochaFreshness maps our Freshness onto the values Bocha accepts.
func bochaFreshness(f Freshness) string {
	switch f {
	case FreshnessWeek:
		return "oneWeek"
	case FreshnessMonth:
		return "oneMonth"
	case FreshnessYear:
		return "oneYear"
	default:
		return "noLimit"
	}
}

// Search runs one query against Bocha. A page whose date does not parse keeps
// a zero PublishedAt rather than failing the whole search.
func (b *bochaProvider) Search(ctx context.Context, query string, opt Options) ([]Result, error) {
	count := bochaDefaultCount
	if opt.Count > 0 {
		count = opt.Count
	}
	reqBody := bochaRequest{
		Query:     query,
		Freshness: bochaFreshness(opt.Freshness),
		Summary:   true,
		Count:     count,
	}

	payload, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("bocha: encode request for %q: %w", query, err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, b.endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("bocha: build request for %q: %w", query, err)
	}
	req.Header.Set("Authorization", "Bearer "+b.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := b.httpClient.Do(req)
	if err != nil {
		// the error names the endpoint and says whether it was a timeout or a
		// cancelled context; it never includes the headers, so the key stays out
		return nil, fmt.Errorf("bocha: search %q: %w", query, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		// Bocha explains a rejection (bad key, no balance, rate limit) in the
		// body, which the status alone does not tell apart
		body, _ := io.ReadAll(io.LimitReader(resp.Body, bochaErrorBodyLimit))
		return nil, fmt.Errorf("bocha: search %q: status %d: %s", query, resp.StatusCode, body)
	}

	var out bochaResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("bocha: search %q: decode response: %w", query, err)
	}
	// an HTTP 200 can still carry an error code in the body
	if out.Code != http.StatusOK {
		return nil, fmt.Errorf("bocha: search %q: api error %d: %s", query, out.Code, out.Msg)
	}

	pages := out.Data.WebPages.Value
	results := make([]Result, 0, len(pages))
	for _, page := range pages {
		summary := page.Summary
		if summary == "" {
			summary = page.Snippet
		}

		var publishedAt time.Time
		if page.DatePublished != "" {
			t, err := time.Parse(time.RFC3339, page.DatePublished)
			if err != nil {
				log.Debugf("bocha: unparsable date %q for %s: %v", page.DatePublished, page.URL, err)
			} else {
				publishedAt = t
			}
		}

		results = append(results, Result{
			Title:       page.Name,
			URL:         page.URL,
			SiteName:    page.SiteName,
			Summary:     summary,
			PublishedAt: publishedAt,
		})
	}
	return results, nil
}
