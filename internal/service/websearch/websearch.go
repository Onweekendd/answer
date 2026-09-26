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

// Package websearch puts web search engines behind one interface, so the AI
// analysis can cite live prices and reviews without knowing which engine
// answered, and tests never reach the network.
package websearch

import (
	"context"
	"errors"
	"time"
)

// ErrDisabled is what a Provider returns when no engine is configured. It is
// not a failure: callers carry on without sources and do not log it as one.
var ErrDisabled = errors.New("web search is not configured")

// Freshness limits results by publish date. The values are this package's own;
// each provider maps them onto whatever its API expects.
type Freshness string

const (
	FreshnessAny   Freshness = ""
	FreshnessWeek  Freshness = "week"
	FreshnessMonth Freshness = "month"
	FreshnessYear  Freshness = "year"
)

// Options tunes one search. Zero values mean "provider default".
type Options struct {
	Count     int
	Freshness Freshness
}

// Result is one page, reduced to what the model reads and what the reader
// needs to check the claim.
type Result struct {
	Title    string
	URL      string
	SiteName string
	// Summary is the page text handed to the model. Providers fill it with the
	// longest plain-text excerpt they have.
	Summary string
	// PublishedAt is zero when the page carries no date. It is a time.Time and
	// not a string so the prompt can print it in one format for every provider.
	PublishedAt time.Time
}

// Provider is the only thing the rest of the code knows about a search engine.
// Called as websearch.Provider, which is why the name does not repeat "Search".
type Provider interface {
	Search(ctx context.Context, query string, opt Options) ([]Result, error)
}
