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
	"fmt"
	"sync"
	"testing"

	"github.com/apache/answer/internal/service/websearch"
	"github.com/stretchr/testify/require"
)

func Test_sourceSet(t *testing.T) {
	t.Run("搜到过的 URL 才算数", func(t *testing.T) {
		// Arrange
		sources := newSourceSet()

		// Act
		sources.add([]websearch.Result{{URL: "https://a.example/1"}, {URL: ""}})

		// Assert
		require.True(t, sources.has("https://a.example/1"))
		require.False(t, sources.has("https://evil.example/log?data=secret"))
		require.False(t, sources.has(""), "an empty URL is never a source")
	})

	t.Run("多个搜索同时写入，一条都不丢", func(t *testing.T) {
		// Arrange
		sources := newSourceSet()
		var wg sync.WaitGroup

		// Act: 20 searches finish at once, each adding its own five results
		for i := 0; i < 20; i++ {
			wg.Go(func() {
				results := make([]websearch.Result, 0, 5)
				for j := 0; j < 5; j++ {
					results = append(results, websearch.Result{URL: fmt.Sprintf("https://a.example/%d/%d", i, j)})
				}
				sources.add(results)
				sources.has("https://a.example/0/0") // reads race with the writes too
			})
		}
		wg.Wait()

		// Assert
		for i := 0; i < 20; i++ {
			for j := 0; j < 5; j++ {
				require.True(t, sources.has(fmt.Sprintf("https://a.example/%d/%d", i, j)))
			}
		}
	})
}
