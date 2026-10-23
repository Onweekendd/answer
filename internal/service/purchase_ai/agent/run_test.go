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

package agent

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/apache/answer/internal/service/llm"
	"github.com/apache/answer/internal/service/purchase_ai/tool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 核心的测试只用最简单的假模型和假插件：循环规则在这里，真实工具的行为在各个插件的测试里。

type fakeModel struct {
	replies []*llm.ChatResp
	reqs    []llm.ChatReq
}

func (f *fakeModel) Chat(_ context.Context, req *llm.ChatReq) (*llm.ChatResp, error) {
	snapshot := *req
	snapshot.Messages = append([]llm.Message(nil), req.Messages...)
	f.reqs = append(f.reqs, snapshot)
	if len(f.reqs) > len(f.replies) {
		return nil, fmt.Errorf("fakeModel: no reply for request %d", len(f.reqs))
	}
	return f.replies[len(f.reqs)-1], nil
}

type fakeRecorder struct {
	messages []llm.Message
	metas    []Meta
}

func (f *fakeRecorder) Keep(_ context.Context, m llm.Message, meta Meta) {
	f.messages = append(f.messages, m)
	f.metas = append(f.metas, meta)
}

type fakeObserver struct{ events []string }

func (f *fakeObserver) Describe(call llm.ToolCall) string { return "做 " + call.Name }
func (f *fakeObserver) Reasoning(context.Context, string) {
	f.events = append(f.events, "reasoning")
}
func (f *fakeObserver) ToolStart(_ context.Context, id string, call llm.ToolCall, summary string) {
	f.events = append(f.events, "start "+id+" "+call.Name+" "+summary)
}
func (f *fakeObserver) ToolEnd(_ context.Context, id string, call llm.ToolCall, _ tool.Result) {
	f.events = append(f.events, "end "+id+" "+call.Name)
}

type noArgs struct{}

// echo 是一个什么都不做的工具；res 决定它的结果
func echo(name string, res tool.Result) Plugin {
	return func(r *Registry) {
		r.Tool(tool.Spec{Name: name, Description: name, Args: noArgs{}, Run: func(context.Context, string) (tool.Result, error) {
			return res, nil
		}})
	}
}

func call(id, name string) llm.ToolCall { return llm.ToolCall{ID: id, Name: name, Arguments: "{}"} }

func calls(cs ...llm.ToolCall) *llm.ChatResp { return &llm.ChatResp{ToolCalls: cs} }

func say(text string) *llm.ChatResp { return &llm.ChatResp{Content: text} }

func doneCall(id, text string) llm.ToolCall {
	return llm.ToolCall{ID: id, Name: DoneName, Arguments: `{"text":"` + text + `","success":true}`}
}

func start() []llm.Message {
	return []llm.Message{llm.Text(llm.RoleSystem, "sys"), llm.Text(llm.RoleUser, "买不买")}
}

func newAgent(t *testing.T, model llm.Client, plugins ...Plugin) *Agent {
	t.Helper()
	a, err := New(Config{
		Model:    model,
		MaxSteps: 5,
		Timeouts: Timeouts{Run: time.Second, Model: time.Second},
		Plugins:  plugins,
	})
	require.NoError(t, err)
	return a
}

// toolReplies 是第 i 次请求里的工具消息，按顺序
func toolReplies(model *fakeModel, i int) []llm.Message {
	var out []llm.Message
	for _, m := range model.reqs[i].Messages {
		if m.Role == llm.RoleTool {
			out = append(out, m)
		}
	}
	return out
}

func TestNew_DuplicateToolIsAnError(t *testing.T) {
	// Act
	_, err := New(Config{Plugins: []Plugin{echo("a", tool.Result{}), echo("a", tool.Result{})}})

	// Assert
	require.ErrorContains(t, err, `"a" is registered twice`)
}

func TestNew_ToolDefinitionsFollowRegistrationOrder(t *testing.T) {
	// Act
	a := newAgent(t, &fakeModel{}, echo("b", tool.Result{}), echo("a", tool.Result{}), Done())

	// Assert
	var names []string
	for _, d := range a.defs {
		names = append(names, d.Name)
	}
	assert.Equal(t, []string{"b", "a", DoneName}, names, "顺序就是发给模型的顺序")
}

func TestRun_NoToolsMeansTheFirstTextIsTheAnswer(t *testing.T) {
	// Arrange
	model := &fakeModel{replies: []*llm.ChatResp{say("直接回答")}}

	// Act
	res, err := newAgent(t, model).Run(context.Background(), start())

	// Assert
	require.NoError(t, err)
	assert.Equal(t, "直接回答", res.Text)
	assert.True(t, res.Success)
	assert.Empty(t, res.Finish)
	assert.Empty(t, model.reqs[0].Tools, "没有任何插件，不给工具")
}

func TestRun_ToolThenDone(t *testing.T) {
	// Arrange
	model := &fakeModel{replies: []*llm.ChatResp{
		calls(call("c1", "look")),
		calls(doneCall("c2", "买")),
	}}
	rec, obs := &fakeRecorder{}, &fakeObserver{}
	a, err := New(Config{Model: model, MaxSteps: 5, Timeouts: Timeouts{Run: time.Second, Model: time.Second},
		Plugins: []Plugin{echo("look", tool.Result{Text: "看到了"}), Done()}, Recorder: rec, Observer: obs})
	require.NoError(t, err)

	// Act
	res, err := a.Run(context.Background(), start())

	// Assert
	require.NoError(t, err)
	assert.Equal(t, "买", res.Text)
	assert.True(t, res.Success)
	assert.Len(t, res.Steps, 2)
	assert.Equal(t, []string{"start 0-0 look 做 look", "end 0-0 look"}, obs.events, "done 不是一张工具卡片，不发事件")
	roles := make([]string, 0, len(rec.messages))
	for _, m := range rec.messages {
		roles = append(roles, m.Role)
	}
	assert.Equal(t, []string{llm.RoleAssistant, llm.RoleTool, llm.RoleAssistant, llm.RoleTool}, roles)
	assert.True(t, rec.metas[3].Final, "带着回答的是 done 的回复")
	assert.Equal(t, "look", rec.metas[1].ToolName)
	assert.Equal(t, "做 look", rec.metas[1].Summary)
}

func TestRun_EveryCallGetsAReply(t *testing.T) {
	cases := []struct {
		name    string
		first   tool.Result
		skipped string
	}{
		{"页面变了", tool.Result{PageChanged: true}, SkippedText},
		{"失败了", tool.Result{Failed: true}, SkippedAfterFailText},
		{"结束了", tool.Result{Terminate: true, Answer: "好了", Success: true}, SkippedAfterDoneText},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Arrange：同一轮三个调用，第一个决定了后面两个不执行
			ran := 0
			second := func(r *Registry) {
				r.Tool(tool.Spec{Name: "second", Description: "x", Args: noArgs{}, Run: func(context.Context, string) (tool.Result, error) {
					ran++
					return tool.Result{}, nil
				}})
			}
			model := &fakeModel{replies: []*llm.ChatResp{
				calls(call("c1", "first"), call("c2", "second"), call("c3", "second")),
				calls(doneCall("c4", "好")),
			}}
			a := newAgent(t, model, echo("first", tc.first), second, Done())

			// Act
			_, err := a.Run(context.Background(), start())

			// Assert
			require.NoError(t, err)
			assert.Zero(t, ran, "后面的调用没有执行")
			if tc.first.Terminate {
				return // 结束了就不再请求模型
			}
			replies := toolReplies(model, 1)
			require.Len(t, replies, 3, "每个调用都有一条回复，不然服务商会拒绝这段对话")
			assert.Equal(t, tc.skipped, replies[1].Parts[0].Text)
			assert.Equal(t, tc.skipped, replies[2].Parts[0].Text)
		})
	}
}

func TestRun_ScreenshotComesAfterTheWholeBatchOfReplies(t *testing.T) {
	// Arrange
	model := &fakeModel{replies: []*llm.ChatResp{
		calls(call("c1", "shot"), call("c2", "look")),
		calls(doneCall("c3", "好")),
	}}
	a := newAgent(t, model, echo("shot", tool.Result{Image: "data:image/png;base64,AAAA"}), echo("look", tool.Result{}), Done())

	// Act
	_, err := a.Run(context.Background(), start())

	// Assert
	require.NoError(t, err)
	got := model.reqs[1].Messages
	n := len(got)
	assert.Equal(t, llm.RoleTool, got[n-3].Role)
	assert.Equal(t, llm.RoleTool, got[n-2].Role)
	assert.Equal(t, llm.RoleUser, got[n-1].Role)
	assert.Equal(t, TagScreenshot, got[n-1].Tag)
	assert.Equal(t, "data:image/png;base64,AAAA", got[n-1].Parts[1].ImageURL)
}

func TestRun_AbortFinishesWithTheToolsReason(t *testing.T) {
	// Arrange
	gone := func(r *Registry) {
		r.Tool(tool.Spec{Name: "open", Description: "x", Args: noArgs{}, Run: func(context.Context, string) (tool.Result, error) {
			return tool.Result{}, fmt.Errorf("open: %w", tool.ErrBrowserGone)
		}})
	}
	model := &fakeModel{replies: []*llm.ChatResp{calls(call("c1", "open"), call("c2", "open")), say("根据已有信息")}}

	// Act
	res, err := newAgent(t, model, gone, Done()).Run(context.Background(), start())

	// Assert
	require.NoError(t, err)
	assert.Equal(t, "根据已有信息", res.Text)
	assert.Equal(t, Reason("browser_gone"), res.Finish)
	assert.False(t, res.Success)
	replies := toolReplies(model, 1)
	require.Len(t, replies, 2)
	assert.Equal(t, tool.ErrBrowserGone.Text, replies[0].Parts[0].Text)
	assert.Equal(t, SkippedText, replies[1].Parts[0].Text)
	last := model.reqs[1].Messages[len(model.reqs[1].Messages)-1]
	assert.Equal(t, tool.ErrBrowserGone.Prompt, last.Parts[0].Text)
	assert.Empty(t, model.reqs[1].Tools, "收尾时不给工具")
}

func TestRun_TextAfterToolsIsRemindedOnceThenTaken(t *testing.T) {
	// Arrange
	model := &fakeModel{replies: []*llm.ChatResp{say("我觉得可以"), say("真的可以")}}

	// Act
	res, err := newAgent(t, model, echo("look", tool.Result{}), Done()).Run(context.Background(), start())

	// Assert
	require.NoError(t, err)
	assert.Equal(t, "真的可以", res.Text)
	require.Len(t, model.reqs, 2)
	assert.Equal(t, DoneReminder, model.reqs[1].Messages[len(model.reqs[1].Messages)-1].Parts[0].Text)
}

func TestRun_EmptyRepliesEndInAForcedFinish(t *testing.T) {
	// Arrange：空回复 → 提醒 → 又是空的 → 收尾
	model := &fakeModel{replies: []*llm.ChatResp{say(""), say(""), say("硬写出来的回答")}}

	// Act
	res, err := newAgent(t, model, echo("look", tool.Result{}), Done()).Run(context.Background(), start())

	// Assert
	require.NoError(t, err)
	assert.Equal(t, ReasonEmpty, res.Finish)
	assert.Equal(t, "硬写出来的回答", res.Text)
	for _, m := range model.reqs[1].Messages {
		if m.Role == llm.RoleAssistant {
			assert.Equal(t, EmptyReply, m.Parts[0].Text, "空回复换成一句话放回对话，服务商会拒绝空的 assistant 消息")
		}
	}
}

func TestRun_StepsRunOutThenFinish(t *testing.T) {
	// Arrange
	var replies []*llm.ChatResp
	for i := range 5 {
		replies = append(replies, calls(call(fmt.Sprintf("c%d", i), "look")))
	}
	model := &fakeModel{replies: append(replies, say("到此为止"))}

	// Act
	res, err := newAgent(t, model, echo("look", tool.Result{}), Done()).Run(context.Background(), start())

	// Assert
	require.NoError(t, err)
	assert.Equal(t, ReasonSteps, res.Finish)
	assert.Len(t, model.reqs, 6)
	assert.Len(t, res.Steps, 5)
}

func TestRun_EndOfTurn(t *testing.T) {
	remind := func(turns *[]Turn) Plugin {
		return func(r *Registry) {
			r.EndOfTurn(func(t Turn) Verdict {
				*turns = append(*turns, t)
				return Verdict{Messages: []llm.Message{llm.Text(llm.RoleUser, "提醒")}}
			})
		}
	}
	stop := func(r *Registry) {
		r.EndOfTurn(func(t Turn) Verdict {
			if t.Step == 1 {
				return Verdict{Finish: &Finish{Reason: "enough", Prompt: "够了"}}
			}
			return Verdict{}
		})
	}
	never := func(steps *[]int) Plugin {
		return func(r *Registry) {
			r.EndOfTurn(func(t Turn) Verdict { *steps = append(*steps, t.Step); return Verdict{} })
		}
	}

	t.Run("提醒追加进对话，第一个要求收尾的赢，后面的不再被问", func(t *testing.T) {
		// Arrange
		var turns []Turn
		var laterSteps []int
		model := &fakeModel{replies: []*llm.ChatResp{calls(call("c1", "look")), calls(call("c2", "look")), say("收尾的回答")}}
		a := newAgent(t, model, echo("look", tool.Result{}), remind(&turns), stop, never(&laterSteps), Done())

		// Act
		res, err := a.Run(context.Background(), start())

		// Assert
		require.NoError(t, err)
		assert.Equal(t, Reason("enough"), res.Finish)
		assert.Equal(t, []int{0}, laterSteps, "第 1 步已经有人要求收尾，排在后面的插件没被问")
		require.Len(t, turns, 2)
		assert.Equal(t, 4, turns[0].Left, "5 步，第 0 步结束后还剩 4 步")
		assert.Equal(t, 3, turns[1].Left)
		assert.Len(t, turns[1].Results, 1)
		got := model.reqs[2].Messages
		assert.Equal(t, "够了", got[len(got)-1].Parts[0].Text)
		assert.Equal(t, "提醒", got[len(got)-2].Parts[0].Text)
	})
}

func TestRun_AfterToolCanReadAndChangeTheResult(t *testing.T) {
	// Arrange
	var seen []string
	watch := func(r *Registry) {
		r.AfterTool(func(ctx context.Context, c llm.ToolCall, res *tool.Result) {
			seen = append(seen, c.ID+" "+CallID(ctx))
			res.Text += "（已核对）"
		})
	}
	model := &fakeModel{replies: []*llm.ChatResp{calls(call("c1", "look")), calls(doneCall("c2", "好"))}}

	// Act
	_, err := newAgent(t, model, echo("look", tool.Result{Text: "结果"}), watch, Done()).Run(context.Background(), start())

	// Assert
	require.NoError(t, err)
	assert.Equal(t, []string{"c1 0-0", "c2 1-0"}, seen)
	assert.Equal(t, "结果（已核对）", toolReplies(model, 1)[0].Parts[0].Text)
}

func TestRun_BeforeModelChangesOnlyWhatIsSent(t *testing.T) {
	// Arrange：钩子把所有 tool 消息换成占位语
	hide := func(r *Registry) {
		r.BeforeModel(func(msgs []llm.Message) []llm.Message {
			for i, m := range msgs {
				if m.Role == llm.RoleTool {
					msgs[i].Parts = []llm.Part{{Text: "占位"}}
				}
			}
			return msgs
		})
	}
	model := &fakeModel{replies: []*llm.ChatResp{calls(call("c1", "look")), calls(doneCall("c2", "好"))}}
	rec := &fakeRecorder{}
	a, err := New(Config{Model: model, MaxSteps: 5, Timeouts: Timeouts{Run: time.Second, Model: time.Second},
		Plugins: []Plugin{echo("look", tool.Result{Text: "真内容"}), hide, Done()}, Recorder: rec})
	require.NoError(t, err)

	// Act
	_, err = a.Run(context.Background(), start())

	// Assert
	require.NoError(t, err)
	assert.Equal(t, "占位", toolReplies(model, 1)[0].Parts[0].Text, "发给模型的是处理过的")
	assert.Equal(t, "真内容", rec.messages[1].Parts[0].Text, "记下来的是原样")
}

func TestRun_OnAnswerCleansTheAnswerAndItsExtrasAreRecorded(t *testing.T) {
	// Arrange
	strip := func(r *Registry) {
		r.OnAnswer(func(_ context.Context, a *Answer) {
			a.Text = strings.TrimSuffix(a.Text, "[建议]")
			a.Extras["tips"] = 1
		})
	}
	for name, replies := range map[string][]*llm.ChatResp{
		"done": {calls(doneCall("c1", "买[建议]"))},
		"直接回答": {say("买[建议]")},
	} {
		t.Run(name, func(t *testing.T) {
			rec := &fakeRecorder{}
			plugins := []Plugin{strip}
			if name == "done" {
				plugins = append(plugins, Done())
			}
			a, err := New(Config{Model: &fakeModel{replies: replies}, MaxSteps: 3, Timeouts: Timeouts{Run: time.Second, Model: time.Second},
				Plugins: plugins, Recorder: rec})
			require.NoError(t, err)

			// Act
			res, err := a.Run(context.Background(), start())

			// Assert
			require.NoError(t, err)
			assert.Equal(t, "买", res.Text)
			assert.Equal(t, 1, res.Extras["tips"])
			finals := 0
			for _, meta := range rec.metas {
				if meta.Final {
					finals++
					assert.Equal(t, 1, meta.Extras["tips"], "Extras 和回答一起记进日志")
				}
			}
			assert.Equal(t, 1, finals)
		})
	}
}

func TestRun_FinishAnswerIsRunThroughOnAnswerToo(t *testing.T) {
	// Arrange
	mark := func(r *Registry) { r.OnAnswer(func(_ context.Context, a *Answer) { a.Text += "!" }) }
	model := &fakeModel{replies: []*llm.ChatResp{say("")}} // 没有 done，空回复 → 收尾
	model.replies = append(model.replies, say("收尾"))

	// Act
	res, err := newAgent(t, model, echo("look", tool.Result{}), mark).Run(context.Background(), start())

	// Assert
	require.NoError(t, err)
	assert.Equal(t, "收尾!", res.Text)
	assert.Equal(t, ReasonEmpty, res.Finish)
}

func TestRun_CallerMessagesAreNotModified(t *testing.T) {
	// Arrange：切片有空余容量，append 会写进它的底层数组
	messages := append(make([]llm.Message, 0, 16), start()...)
	model := &fakeModel{replies: []*llm.ChatResp{say("好")}}

	// Act
	_, err := newAgent(t, model).Run(context.Background(), messages)

	// Assert
	require.NoError(t, err)
	assert.Len(t, messages, 2)
	assert.Empty(t, messages[:cap(messages)][2].Role, "底层数组的空余部分没被写")
}

func TestRun_ModelErrorEndsTheTask(t *testing.T) {
	// Arrange
	a := newAgent(t, &fakeModel{}, echo("look", tool.Result{}))

	// Act
	res, err := a.Run(context.Background(), start())

	// Assert
	require.Error(t, err)
	assert.Nil(t, res)
}
