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

package llm

import (
	"context"
	"fmt"
)

// jsonRetryPrompt tells the model why its last reply was rejected. It is in
// English on purpose: the rejection is the parse function's own error text.
const jsonRetryPrompt = "Your previous reply was rejected: %v. " +
	"Reply again with the corrected JSON object only, and nothing else."

// withRejection returns a new conversation: messages, then the reply the model
// gave, then why it was rejected. The model sees what it wrote next to what is
// wrong with it, which fixes a reply better than asking again from scratch.
//
// It copies rather than appends, so the caller's slice is never written to.
func withRejection(messages []Message, reply string, err error) []Message {
	out := make([]Message, 0, len(messages)+2)
	out = append(out, messages...)
	return append(out,
		Text(RoleAssistant, reply),
		Text(RoleUser, fmt.Sprintf(jsonRetryPrompt, err)),
	)
}

// ChatJSON asks for a JSON reply and turns it into a T with parse, the way a
// zod schema's parse does in TypeScript. When parse rejects the reply, the
// model is shown its reply and parse's error and asked again, up to attempts
// times in all. A failed call to the provider is returned at once: resending
// fixes a malformed reply, not a network error.
//
// On failure it returns T's zero value, which is nil when T is a pointer.
func ChatJSON[T any](ctx context.Context, client Client, req *ChatReq, attempts int, parse func(content string) (T, error)) (T, error) {
	var zero T
	if attempts < 1 {
		attempts = 1
	}

	// a copy, so the caller's request keeps its own settings; withRejection
	// builds a new slice each round, so the caller's messages stay as they were
	r := *req
	r.JSONOutput = true

	var lastErr error
	for range attempts {
		resp, err := client.Chat(ctx, &r)
		if err != nil {
			return zero, err
		}

		v, err := parse(resp.Content)
		if err == nil {
			return v, nil
		}

		lastErr = err
		r.Messages = withRejection(r.Messages, resp.Content, err)
	}
	return zero, fmt.Errorf("llm: no valid JSON after %d attempts: %w", attempts, lastErr)
}
