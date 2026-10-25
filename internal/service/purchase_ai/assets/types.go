// Licensed to the Apache Software Foundation (ASF) under one
// or more contributor license agreements. See the NOTICE file
// distributed with this work for additional information regarding
// copyright ownership. The ASF licenses this file to You under the
// Apache License, Version 2.0 (the "License"); you may not use this
// file except in compliance with the License. You may obtain a copy
// at http://www.apache.org/licenses/LICENSE-2.0
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.

// Package assets runs media collection jobs using the purchase_ai agent and
// browser tools. HTTP and MCP are adapters over the same service.
package assets

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"
)

type Request struct {
	ProjectID          string   `json:"project_id" description:"Storage namespace: 1-80 letters, digits, underscores or hyphens"`
	RequestID          string   `json:"request_id,omitempty" description:"Optional idempotency key, unique within the project"`
	Query              string   `json:"query,omitempty" description:"Topic and selection requirements; required unless source_urls is provided"`
	Kind               string   `json:"kind" description:"video or image"`
	Count              int      `json:"count,omitempty" description:"Number of assets, 1-10; defaults to 1"`
	SourceURLs         []string `json:"source_urls,omitempty" description:"Optional known source pages; at most 20"`
	MinShortSide       int      `json:"min_short_side,omitempty" description:"Minimum actual short edge in pixels; defaults to 1080"`
	MaxDurationSeconds int      `json:"max_duration_seconds,omitempty" description:"Maximum video duration, 1-900 seconds; defaults to 120"`
}

var identifier = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,79}$`)

func (r *Request) normalize() error {
	r.Query = strings.TrimSpace(r.Query)
	if !identifier.MatchString(r.ProjectID) {
		return errors.New("project_id must contain 1-80 letters, digits, underscores or hyphens")
	}
	if r.RequestID != "" && !identifier.MatchString(r.RequestID) {
		return errors.New("invalid request_id")
	}
	if r.Kind != "video" && r.Kind != "image" {
		return errors.New("kind must be video or image")
	}
	if r.Count == 0 {
		r.Count = 1
	}
	if r.Count < 1 || r.Count > 10 {
		return errors.New("count must be between 1 and 10")
	}
	if len([]rune(r.Query)) > 2000 || len(r.SourceURLs) > 20 {
		return errors.New("query or source_urls is too large")
	}
	if r.Query == "" && len(r.SourceURLs) == 0 {
		return errors.New("query or source_urls is required")
	}
	for _, raw := range r.SourceURLs {
		u, err := url.Parse(raw)
		if err != nil || u.Hostname() == "" || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") {
			return fmt.Errorf("invalid source URL: %q", raw)
		}
	}
	if r.MinShortSide == 0 {
		r.MinShortSide = 1080
	}
	if r.MinShortSide < 1 || r.MinShortSide > 4096 {
		return errors.New("min_short_side must be between 1 and 4096")
	}
	if r.MaxDurationSeconds == 0 {
		r.MaxDurationSeconds = 120
	}
	if r.MaxDurationSeconds < 1 || r.MaxDurationSeconds > 900 {
		return errors.New("max_duration_seconds must be between 1 and 900")
	}
	return nil
}

type Asset struct {
	ID           string    `json:"asset_id"`
	Kind         string    `json:"type"`
	Bucket       string    `json:"bucket"`
	ObjectKey    string    `json:"object_key"`
	URL          string    `json:"url"`
	URLExpiresAt time.Time `json:"url_expires_at"`
	SourceURL    string    `json:"source_url"`
	OriginalURL  string    `json:"original_url,omitempty"`
	Title        string    `json:"title"`
	Author       string    `json:"author,omitempty"`
	ContentType  string    `json:"content_type"`
	Bytes        int       `json:"bytes"`
	SHA256       string    `json:"sha256"`
	Width        int       `json:"width"`
	Height       int       `json:"height"`
	DurationMS   int64     `json:"duration_ms,omitempty"`
	Variant      string    `json:"variant,omitempty"`
}

type Job struct {
	ID        string    `json:"task_id"`
	Request   Request   `json:"request"`
	Status    string    `json:"status"`
	Stage     string    `json:"stage"`
	Assets    []Asset   `json:"assets"`
	Error     string    `json:"error,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

const (
	Queued        = "queued"
	Running       = "running"
	Completed     = "completed"
	Partial       = "partial"
	Failed        = "failed"
	Canceled      = "canceled"
	InputRequired = "input_required"
)

// Store returns only confirmed, durable objects. Access URLs can be refreshed
// independently of the stable object key.
type Store interface {
	Put(context.Context, string, Asset, []byte) (Asset, error)
	AccessURL(context.Context, Asset) (Asset, error)
}

type Outcome struct {
	NeedsInput bool
	Message    string
}
type RunFunc func(context.Context, Job, func(Asset) error, func(string)) (Outcome, error)

var ErrNotFound = errors.New("task not found")
var ErrConflict = errors.New("request_id already used for a different request")
var ErrClosed = errors.New("asset service is closed")
var ErrCapacity = errors.New("task limit reached")
