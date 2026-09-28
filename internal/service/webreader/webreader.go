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

// Package webreader turns a web page into plain text a model can read, behind
// one interface, so the AI analysis can look inside a page whose search summary
// lacks the number it needs, and tests never reach the network.
package webreader

import (
	"context"
	"errors"
)

// ErrDisabled is what a Reader returns when reading is turned off. It is not a
// failure: the analysis carries on with the search summaries it has.
var ErrDisabled = errors.New("web page reading is turned off")

// Page is a page reduced to its text.
type Page struct {
	Title string
	// Text is the readable text with blank lines dropped and runs of spaces
	// collapsed. It can still be long; callers cut it to their own budget.
	Text string
}

// Reader fetches one page. Called as webreader.Reader.
// Read fetches whatever URL it is given; deciding which URLs may be read is
// the caller's job, and a URL that came from a model must be checked first.
// Implementations must be safe for concurrent use: the AI analysis reads the
// pages of one round at the same time.
type Reader interface {
	Read(ctx context.Context, url string) (*Page, error)
}
