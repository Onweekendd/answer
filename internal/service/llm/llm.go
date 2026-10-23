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

import (
	"context"
	"encoding/json"
)

// Message roles, same names every OpenAI-compatible API uses.
const (
	RoleSystem    = "system"
	RoleUser      = "user"
	RoleAssistant = "assistant"
	RoleTool      = "tool"
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

// ToolCall is the model asking for a tool to be run. The model never runs
// anything itself: the caller runs the tool and answers with a RoleTool
// message whose ToolCallID is this ID.
type ToolCall struct {
	// ID pairs the call with its result. A model can ask for several calls in
	// one turn, so the results must say which call each one answers.
	ID   string
	Name string
	// Arguments is a JSON object written by the model. It is not guaranteed to
	// be valid JSON or to match the schema, so the tool must check it.
	Arguments string
}

// Tool tells the model a tool exists. It is only the declaration sent to the
// provider; running the tool is up to the caller, which maps Name to its code.
type Tool struct {
	Name string
	// Description is what the model reads to decide when to call the tool,
	// so it is written for the model, not for people reading this code.
	Description string
	// Parameters is a JSON Schema object describing the arguments. It is sent
	// verbatim, which a plain string would not be: it would go out quoted.
	Parameters json.RawMessage
}

// Message is one turn of the conversation.
type Message struct {
	Role  string
	Parts []Part
	// ToolCalls is set on an assistant message that asked for tools. When the
	// conversation is sent again, that message must come back with its calls,
	// or the tool results that follow it answer nothing.
	ToolCalls []ToolCall
	// Reasoning is set on an assistant message from a reasoning model. In a
	// conversation that offers tools, deepseek requires it back unchanged on
	// every later request, and it is how the model picks up its earlier thinking.
	Reasoning string
	// ToolCallID is set on a RoleTool message only: the ToolCall.ID it answers.
	ToolCallID string
	// Tag is for the caller's own bookkeeping (which message is a screenshot the
	// agent took, say). It is never sent to a provider.
	Tag string
}

// Text builds a text-only message, which is most of them.
func Text(role, text string) Message {
	return Message{Role: role, Parts: []Part{{Text: text}}}
}

// ChatReq is a request to the model. Fields left empty fall back to whatever
// the provider is configured with in the admin panel.
type ChatReq struct {
	Messages []Message
	// Tools the model may call this turn. Leave it empty to make the model
	// answer from what it already has.
	Tools     []Tool
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
	// Content is the answer meant for the user. When the model asks for tools
	// it is empty or a short preamble ("let me look that up"), not an answer,
	// and it can be empty when the whole budget went on reasoning.
	Content string
	// Reasoning holds the chain of thought for models that return it
	// separately (deepseek does). Useful to show progress, never to parse.
	Reasoning string
	Model     string
	Usage     Usage
	// ToolCalls is non-empty when the model wants tools run before it answers.
	// It, not Content, decides whether the turn is finished.
	ToolCalls []ToolCall
}

// Client is the only thing the rest of the code knows about a chat model.
// Implementations live next to this file; tests use a fake.
type Client interface {
	Chat(ctx context.Context, req *ChatReq) (*ChatResp, error)
}
