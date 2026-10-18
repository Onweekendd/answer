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
	"net/http"
	"strings"
	"time"

	"github.com/apache/answer/internal/base/reason"
	"github.com/apache/answer/internal/service/siteinfo_common"
	"github.com/sashabaranov/go-openai"
	"github.com/segmentfault/pacman/errors"
)

const (
	defaultTimeout = 120 * time.Second
	// defaultMaxTokens is a budget for the whole completion. On a reasoning model
	// the chain of thought is billed as completion tokens too, so it competes with
	// the answer: too small a budget returns an empty Content and a long Reasoning.
	defaultMaxTokens = 4096
)

// openAICompatibleClient talks the OpenAI chat-completions protocol, which is
// what deepseek, qwen, moonshot and others expose. The provider is read from
// the admin settings on every call, so changing it takes effect without a
// restart, which also means the sdk client is built per call.
type openAICompatibleClient struct {
	siteInfoService siteinfo_common.SiteInfoCommonService
	httpClient      *http.Client
	// static, when set, is used instead of the admin settings
	static *StaticProvider
}

// StaticProvider is a fixed provider for code that runs outside the site,
// such as the case runner, where there are no admin settings to read.
type StaticProvider struct {
	APIHost string `json:"api_host"`
	APIKey  string `json:"api_key"`
	Model   string `json:"model"`
}

// NewStaticClient builds a client that always uses p.
func NewStaticClient(p StaticProvider) Client {
	return &openAICompatibleClient{
		httpClient: &http.Client{Timeout: defaultTimeout},
		static:     &p,
	}
}

// NewClient new llm client
func NewClient(siteInfoService siteinfo_common.SiteInfoCommonService) Client {
	return &openAICompatibleClient{
		siteInfoService: siteInfoService,
		httpClient:      &http.Client{Timeout: defaultTimeout},
	}
}

// provider holds what one call needs from the admin settings.
type provider struct {
	client *openai.Client
	model  string
}

// resolveProvider reads the admin settings and builds a client for them.
func (c *openAICompatibleClient) resolveProvider(ctx context.Context) (*provider, error) {
	if s := c.static; s != nil {
		if s.APIKey == "" || s.APIHost == "" {
			return nil, errors.BadRequest(reason.AINotConfigured)
		}
		return c.newProvider(s.APIHost, s.APIKey, s.Model), nil
	}

	aiConfig, err := c.siteInfoService.GetSiteAI(ctx)
	if err != nil {
		return nil, err
	}
	p := aiConfig.GetProvider()
	if !aiConfig.Enabled || p.APIKey == "" || p.APIHost == "" {
		return nil, errors.BadRequest(reason.AINotConfigured)
	}

	return c.newProvider(p.APIHost, p.APIKey, p.Model), nil
}

func (c *openAICompatibleClient) newProvider(apiHost, apiKey, model string) *provider {
	config := openai.DefaultConfig(apiKey)
	config.BaseURL = baseURL(apiHost)
	config.HTTPClient = c.httpClient
	return &provider{client: openai.NewClientWithConfig(config), model: model}
}

// Chat sends one request and returns the whole answer.
func (c *openAICompatibleClient) Chat(ctx context.Context, req *ChatReq) (*ChatResp, error) {
	p, err := c.resolveProvider(ctx)
	if err != nil {
		return nil, err
	}

	// the request can hold base64 images, so it is never logged
	resp, err := p.client.CreateChatCompletion(ctx, c.buildRequest(req, p.model))
	if err != nil {
		// a done context comes back as itself: the pacman error below has no
		// Unwrap, so wrapped in it, errors.Is could not tell a call that ran out
		// of time (worth one retry) from a provider that refused
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		return nil, errors.InternalServer(reason.AIRequestFailed).WithError(err).WithStack()
	}
	if len(resp.Choices) == 0 {
		return nil, errors.InternalServer(reason.AIRequestFailed).
			WithError(fmt.Errorf("provider returned no choices")).WithStack()
	}

	msg := resp.Choices[0].Message
	return &ChatResp{
		Content:   msg.Content,
		Reasoning: msg.ReasoningContent,
		Model:     resp.Model,
		ToolCalls: fromSDKToolCalls(msg.ToolCalls),
		Usage: Usage{
			PromptTokens:     resp.Usage.PromptTokens,
			CompletionTokens: resp.Usage.CompletionTokens,
			TotalTokens:      resp.Usage.TotalTokens,
		},
	}, nil
}

// buildRequest maps our request onto the sdk one, filling in what the caller
// left empty from the configured provider.
func (c *openAICompatibleClient) buildRequest(req *ChatReq, fallbackModel string) openai.ChatCompletionRequest {
	model := req.Model
	if model == "" {
		model = fallbackModel
	}
	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = defaultMaxTokens
	}

	out := openai.ChatCompletionRequest{
		Model:     model,
		Messages:  toSDKMessages(req.Messages),
		MaxTokens: maxTokens,
		Tools:     toSDKTools(req.Tools),
	}
	if req.Temperature != nil {
		// the sdk field is tagged omitempty, so a temperature of exactly 0 is
		// dropped from the payload and the provider applies its own default
		out.Temperature = *req.Temperature
	}
	if req.JSONOutput {
		out.ResponseFormat = &openai.ChatCompletionResponseFormat{
			Type: openai.ChatCompletionResponseFormatTypeJSONObject,
		}
	}
	return out
}

// toSDKTools declares the tools as functions, the only kind of tool the chat
// completions protocol has. With no tools the slice is empty and the sdk leaves
// the field out, so a plain chat request looks the same as before.
func toSDKTools(tools []Tool) []openai.Tool {
	out := make([]openai.Tool, 0, len(tools))
	for _, tool := range tools {
		out = append(out, openai.Tool{
			Type: openai.ToolTypeFunction,
			Function: &openai.FunctionDefinition{
				Name:        tool.Name,
				Description: tool.Description,
				Parameters:  tool.Parameters,
			},
		})
	}
	return out
}

// toSDKToolCalls puts an earlier assistant turn's calls back into the
// conversation, so the tool results that follow have something to answer.
func toSDKToolCalls(calls []ToolCall) []openai.ToolCall {
	out := make([]openai.ToolCall, 0, len(calls))
	for _, call := range calls {
		out = append(out, openai.ToolCall{
			ID:   call.ID,
			Type: openai.ToolTypeFunction,
			Function: openai.FunctionCall{
				Name:      call.Name,
				Arguments: call.Arguments,
			},
		})
	}
	return out
}

// fromSDKToolCalls flattens the calls in a response. Arguments stay the raw
// string the model wrote; each tool decodes and checks its own.
func fromSDKToolCalls(calls []openai.ToolCall) []ToolCall {
	out := make([]ToolCall, 0, len(calls))
	for _, call := range calls {
		out = append(out, ToolCall{
			ID:        call.ID,
			Name:      call.Function.Name,
			Arguments: call.Function.Arguments,
		})
	}
	return out
}

// toSDKMessages fills Content for text-only messages and MultiContent when a
// message carries an image. The two are mutually exclusive: the sdk marshals
// Content as a plain string and MultiContent as a parts array, and rejects a
// message that sets both.
func toSDKMessages(messages []Message) []openai.ChatCompletionMessage {
	out := make([]openai.ChatCompletionMessage, 0, len(messages))
	for _, m := range messages {
		hasImage := false
		for _, part := range m.Parts {
			if part.ImageURL != "" {
				hasImage = true
				break
			}
		}

		if !hasImage {
			text := make([]string, 0, len(m.Parts))
			for _, part := range m.Parts {
				if part.Text != "" {
					text = append(text, part.Text)
				}
			}
			out = append(out, openai.ChatCompletionMessage{
				Role:             m.Role,
				Content:          strings.Join(text, "\n"),
				ReasoningContent: m.Reasoning,
				ToolCalls:        toSDKToolCalls(m.ToolCalls),
				ToolCallID:       m.ToolCallID,
			})
			continue
		}

		parts := make([]openai.ChatMessagePart, 0, len(m.Parts))
		for _, part := range m.Parts {
			if part.ImageURL != "" {
				parts = append(parts, openai.ChatMessagePart{
					Type:     openai.ChatMessagePartTypeImageURL,
					ImageURL: &openai.ChatMessageImageURL{URL: part.ImageURL},
				})
				continue
			}
			if part.Text != "" {
				parts = append(parts, openai.ChatMessagePart{
					Type: openai.ChatMessagePartTypeText,
					Text: part.Text,
				})
			}
		}
		out = append(out, openai.ChatCompletionMessage{
			Role:             m.Role,
			MultiContent:     parts,
			ReasoningContent: m.Reasoning,
			ToolCalls:        toSDKToolCalls(m.ToolCalls),
			ToolCallID:       m.ToolCallID,
		})
	}
	return out
}

// baseURL normalises the configured host, which admins write with or without
// the /v1 suffix. The sdk appends the endpoint path itself.
func baseURL(host string) string {
	host = strings.TrimSuffix(strings.TrimSpace(host), "/")
	if !strings.HasSuffix(host, "/v1") {
		host += "/v1"
	}
	return host
}
