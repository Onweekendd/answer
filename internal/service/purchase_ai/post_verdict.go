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
	"strings"

	"github.com/apache/answer/internal/entity"
)

// parseVerdict reads the verdict the system prompt asks for on the first line,
// such as "**再等等** —— 东西不错但时机不对", and returns its vote choice, or 0
// when the first line holds none. 不买 is checked before 买, which it contains.
func parseVerdict(advice string) int {
	for line := range strings.SplitSeq(advice, "\n") {
		line = strings.TrimLeft(strings.TrimSpace(line), "#*> ")
		if line == "" {
			continue
		}
		switch {
		case strings.HasPrefix(line, "不买"):
			return entity.PurchaseVoteSkip
		case strings.HasPrefix(line, "再等等"):
			return entity.PurchaseVoteWait
		case strings.HasPrefix(line, "买"):
			return entity.PurchaseVoteBuy
		}
		return 0
	}
	return 0
}
