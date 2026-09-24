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

// Package llm wraps the chat models behind one interface, so the rest of the
// code never imports a vendor SDK and tests never reach the network.
package llm

import "context"

// Message roles, same names every OpenAI-compatible API uses.
const (
	RoleSystem    = "system"
	RoleUser      = "user"
	RoleAssistant = "assistant"
)

// Part is one piece of a message. Exactly one field is set: either the message
// carries text, or it carries an image. Keeping content as a list of parts is
// what lets a vision request be built without changing this interface.
type Part struct {
	Text string
	// ImageURL is a data: URI (data:image/jpeg;base64,...) or a public URL.
	// The mime type must match the real bytes, not the file extension.
	ImageURL string
}

// Message is one turn of the conversation.
type Message struct {
	Role  string
	Parts []Part
}

// Text builds a text-only message, which is most of them.
func Text(role, text string) Message {
	return Message{Role: role, Parts: []Part{{Text: text}}}
}

// ChatReq is a request to the model. Fields left empty fall back to whatever
// the provider is configured with in the admin panel.
type ChatReq struct {
	Messages  []Message
	Model     string
	MaxTokens int
	// Temperature is a pointer so "not set" is distinguishable from 0,
	// which is a meaningful value (deterministic output).
	Temperature *float32
	// JSONOutput asks the provider for a JSON object. Support varies, so the
	// caller must still validate what comes back.
	JSONOutput bool
}

// Usage is what the call cost, for the metering added later.
type Usage struct {
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
}

// ChatResp is the model's answer.
type ChatResp struct {
	// Content is the answer meant for the user. It can be empty while the
	// model is still reasoning, so never assume it is filled.
	Content string
	// Reasoning holds the chain of thought for models that return it
	// separately (deepseek does). Useful to show progress, never to parse.
	Reasoning string
	Model     string
	Usage     Usage
}

// Client is the only thing the rest of the code knows about a chat model.
// Implementations live next to this file; tests use a fake.
type Client interface {
	Chat(ctx context.Context, req *ChatReq) (*ChatResp, error)
}
