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
	"sync"

	"github.com/apache/answer/internal/service/websearch"
)

// sourceSet is the set of URLs one analysis has seen in its search results.
// fetch_page only reads URLs from this set: a page the model reads could tell
// it to fetch an address carrying the user's post in its query string, and
// the set means it can only ever fetch pages a search already returned.
//
// The searches of one round run at the same time, so they add to the set
// concurrently, which is why it holds a lock.
type sourceSet struct {
	mu sync.Mutex
	// urls is a set: only the keys matter, and struct{} takes no memory
	urls map[string]struct{}
}

func newSourceSet() *sourceSet {
	return &sourceSet{urls: make(map[string]struct{})}
}

// add records every result's URL. A result without one is skipped, so an
// empty URL never counts as a source.
func (s *sourceSet) add(results []websearch.Result) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, result := range results {
		if result.URL == "" {
			continue
		}
		s.urls[result.URL] = struct{}{}
	}
}

// has reports whether url came back from a search in this analysis. Reads
// lock too: a search of the same round may be adding while this reads, and a
// map read during a write is a fatal error, not just a stale answer.
func (s *sourceSet) has(url string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, ok := s.urls[url]
	return ok
}
