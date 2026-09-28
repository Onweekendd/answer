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

package webreader

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	jinaEndpoint = "https://r.jina.ai/"
	// jinaTimeout is generous: Jina loads the page itself, and a slow site
	// took over ten seconds in testing.
	jinaTimeout = 30 * time.Second
	// jinaErrorBodyLimit caps how much of a failed response goes into the error
	jinaErrorBodyLimit = 512
)

// jinaReader reads pages through Jina Reader (r.jina.ai). It works without a
// key at a low rate limit (20 requests a minute when tested); a key raises it.
//
// The page is fetched from Jina's servers, not ours, so a URL pointing at our
// own network cannot reach it; Jina also refuses localhost and private IPs.
type jinaReader struct {
	apiKey     string
	httpClient *http.Client
	// endpoint is a field rather than the constant so a test can point it at
	// an httptest server.
	endpoint string
}

func newJina(apiKey string) *jinaReader {
	return &jinaReader{
		apiKey:     apiKey,
		httpClient: &http.Client{Timeout: jinaTimeout},
		endpoint:   jinaEndpoint,
	}
}

// wire format of the Jina Reader JSON response, only the fields we read
type jinaResponse struct {
	Code int `json:"code"`
	Data struct {
		Title string `json:"title"`
		Text  string `json:"text"`
		// HTTPStatus is what the page itself answered with; a 404 page still
		// comes back from Jina with code 200
		HTTPStatus int `json:"httpStatus"`
	} `json:"data"`
}

// Read fetches one page as plain text. Images are dropped and the page is
// asked for as text rather than Markdown, which cut a product page from 26KB
// to under 8KB in testing, most of the difference being link targets.
func (j *jinaReader) Read(ctx context.Context, url string) (*Page, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, j.endpoint+url, nil)
	if err != nil {
		return nil, fmt.Errorf("jina: build request for %s: %w", url, err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Return-Format", "text")
	req.Header.Set("X-Retain-Images", "none")
	if j.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+j.apiKey)
	}

	resp, err := j.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("jina: read %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		// Jina explains a refusal (rate limit, blocked address, bad key) in
		// the body, which the status alone does not tell apart
		body, _ := io.ReadAll(io.LimitReader(resp.Body, jinaErrorBodyLimit))
		return nil, fmt.Errorf("jina: read %s: status %d: %s", url, resp.StatusCode, body)
	}

	var out jinaResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("jina: read %s: decode response: %w", url, err)
	}
	if out.Data.HTTPStatus >= http.StatusBadRequest {
		return nil, fmt.Errorf("jina: read %s: page answered %d", url, out.Data.HTTPStatus)
	}

	return &Page{Title: strings.TrimSpace(out.Data.Title), Text: compactLines(out.Data.Text)}, nil
}

// compactLines collapses the spaces inside each line and drops empty lines.
// Jina's text output keeps the layout's whitespace, which was about a quarter
// of a page in testing and tells the model nothing.
func compactLines(s string) string {
	lines := strings.Split(s, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		if line = strings.Join(strings.Fields(line), " "); line != "" {
			out = append(out, line)
		}
	}
	return strings.Join(out, "\n")
}
