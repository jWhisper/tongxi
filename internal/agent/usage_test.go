package agent

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

type usageModel struct {
	chunks    []*schema.Message
	streamErr error
}

func (m *usageModel) WithTools([]*schema.ToolInfo) (model.ToolCallingChatModel, error) { return m, nil }
func (m *usageModel) Generate(context.Context, []*schema.Message, ...model.Option) (*schema.Message, error) {
	return m.chunks[0], m.streamErr
}
func (m *usageModel) Stream(context.Context, []*schema.Message, ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	r, w := schema.Pipe[*schema.Message](len(m.chunks) + 1)
	for _, chunk := range m.chunks {
		w.Send(chunk, nil)
	}
	if m.streamErr != nil {
		w.Send(nil, m.streamErr)
	}
	w.Close()
	return r, nil
}
func usageMessage(text string, input, output, cached int) *schema.Message {
	return &schema.Message{Role: schema.Assistant, Content: text, ResponseMeta: &schema.ResponseMeta{Usage: &schema.TokenUsage{PromptTokens: input, CompletionTokens: output, TotalTokens: input + output, PromptTokenDetails: schema.PromptTokenDetails{CachedTokens: cached}}}}
}

func TestMeterCountsStreamOnceIncludingCacheAndInterruptions(t *testing.T) {
	for _, interrupted := range []bool{false, true} {
		t.Run(map[bool]string{false: "complete", true: "interrupted"}[interrupted], func(t *testing.T) {
			base := &usageModel{chunks: []*schema.Message{schema.AssistantMessage("正文", nil), usageMessage("", 100, 20, 60), usageMessage("", 100, 20, 60)}}
			if interrupted {
				base.streamErr = context.Canceled
			}
			calls := 0
			var got TokenUsage
			meter := MeterModel(base, 1, func() error { return nil }, func(u TokenUsage) error { calls++; got = u; return nil })
			stream, err := meter.Stream(context.Background(), []*schema.Message{schema.UserMessage("问题")})
			if err != nil {
				t.Fatal(err)
			}
			err = consume(stream, func(*schema.Message) {})
			if interrupted && !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
			if calls != 1 || got != (TokenUsage{Input: 100, Output: 20, Cached: 60}) {
				t.Fatal(calls, got)
			}
		})
	}
}

func TestMeterStopsBeforeNextRequestAndEstimatesMissingUsage(t *testing.T) {
	limit := errors.New("budget reached")
	var got TokenUsage
	calls := 0
	meter := MeterModel(&usageModel{chunks: []*schema.Message{schema.AssistantMessage("一段输出", nil)}}, 1, func() error {
		if calls > 0 {
			return limit
		}
		return nil
	}, func(u TokenUsage) error { calls++; got = u; return limit })
	bound, err := meter.WithTools([]*schema.ToolInfo{{Name: "read_file", Desc: "读取文件"}})
	if err != nil {
		t.Fatal(err)
	}
	stream, err := bound.Stream(context.Background(), []*schema.Message{schema.UserMessage("输入")})
	if err != nil {
		t.Fatal(err)
	}
	if err = consume(stream, func(*schema.Message) {}); !errors.Is(err, limit) {
		t.Fatal(err)
	}
	if !got.Estimated || got.Input <= estimateTokens([]*schema.Message{schema.UserMessage("输入")}, nil) || got.Output <= 0 || got.Cached != 0 {
		t.Fatal(got)
	}
	if _, err = bound.Generate(context.Background(), nil); !errors.Is(err, limit) || calls != 1 {
		t.Fatal(calls, err)
	}
}

func TestMeterIncludesGeneratedSummary(t *testing.T) {
	calls := 0
	meter := MeterModel(&usageModel{chunks: []*schema.Message{usageMessage("摘要", 30, 10, 0)}}, 1, func() error { return nil }, func(u TokenUsage) error {
		calls++
		if u.Input+u.Output != 40 {
			t.Fatal(u)
		}
		return nil
	})
	result, err := meter.Generate(context.Background(), []*schema.Message{schema.UserMessage("历史")})
	if err != nil || result.Content != "摘要" || calls != 1 {
		t.Fatal(result, err, calls)
	}
	stream, err := meter.Stream(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for {
		_, err = stream.Recv()
		if err != nil {
			break
		}
	}
	if err != io.EOF {
		t.Fatal(err)
	}
	_, _ = stream.Recv()
	stream.Close()
	if calls != 2 {
		t.Fatal("EOF counted twice", calls)
	}
}
