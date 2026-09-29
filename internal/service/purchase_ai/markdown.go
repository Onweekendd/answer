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

import "regexp"

// numberRange matches a range written with an ASCII tilde, such as 68~74.
var numberRange = regexp.MustCompile(`(\d)[ \t]*~[ \t]*(\d)`)

// fixNumberRanges rewrites 68~74 as 68～74. The site renders Markdown with
// GFM, where a single ~ also marks strikethrough, so two ranges in one
// paragraph strike out everything between them and swallow both tildes. The
// full-width ～ is how ranges are written in Chinese anyway, and it is not
// Markdown.
//
// Only a tilde between two digits is touched, so ~~strikethrough~~ and a ~ in
// a URL path stay as they are.
func fixNumberRanges(s string) string {
	// a match consumes the digit after the tilde, so in 1~2~3 the second
	// range is only seen on the next pass
	for {
		fixed := numberRange.ReplaceAllString(s, "${1}～${2}")
		if fixed == s {
			return fixed
		}
		s = fixed
	}
}
