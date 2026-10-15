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

package repo_test

import (
	"time"

	"github.com/apache/answer/internal/entity"
)

// fillQuestionDefaults sets what the question service sets before it calls
// AddQuestion, for tests that call the repo directly. MySQL and SQLite let the
// gaps through, PostgreSQL does not: it rejects "" for a BIGINT id, and a zero
// time, written in a time zone west of UTC, lands in year 0, which it rejects too.
func fillQuestionDefaults(q *entity.Question) {
	for _, id := range []*string{&q.LastEditUserID, &q.AcceptedAnswerID, &q.LastAnswerID, &q.RevisionID} {
		if *id == "" {
			*id = "0"
		}
	}
	now := time.Now()
	// created_at has a database default, but xorm writes the zero value over it
	if q.CreatedAt.IsZero() {
		q.CreatedAt = now
	}
	if q.UpdatedAt.IsZero() {
		q.UpdatedAt = now
	}
	if q.PostUpdateTime.IsZero() {
		q.PostUpdateTime = now
	}
}

// fillTagDefaults is fillQuestionDefaults for tags.
func fillTagDefaults(tags []*entity.Tag) {
	for _, tag := range tags {
		for _, id := range []*string{&tag.RevisionID, &tag.UserID} {
			if *id == "" {
				*id = "0"
			}
		}
	}
}
