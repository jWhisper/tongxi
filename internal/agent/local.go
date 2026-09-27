package agent

import (
	"context"
	"fmt"
	"time"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

// LocalModel is a deterministic probe, never represented as a real LLM.
// It requests the registered tool, then streams the result it actually receives.
type LocalModel struct{ Delay time.Duration }

func (m *LocalModel) WithTools([]*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	copy := *m
	return &copy, nil
}
func (m *LocalModel) Generate(ctx context.Context, in []*schema.Message, _ ...model.Option) (*schema.Message, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	last := in[len(in)-1]
	if last.Role == schema.Tool {
		return schema.AssistantMessage(fmt.Sprintf("本地检查完成。字符统计工具返回 %s；Eino 已完成工具调用与流式输出。这是固定测试，不代表真实模型连接成功。", last.Content), nil), nil
	}
	index := 0
	return schema.AssistantMessage("", []schema.ToolCall{{Index: &index, ID: "local-count", Type: "function", Function: schema.FunctionCall{Name: "count_characters", Arguments: `{"text":"同席"}`}}}), nil
}

func (m *LocalModel) Stream(ctx context.Context, in []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	msg, err := m.Generate(ctx, in, opts...)
	if err != nil {
		return nil, err
	}
	reader, writer := schema.Pipe[*schema.Message](1)
	go func() {
		defer writer.Close()
		if len(msg.ToolCalls) > 0 {
			writer.Send(msg, nil)
			return
		}
		for _, r := range msg.Content {
			if m.Delay > 0 {
				select {
				case <-ctx.Done():
					writer.Send(nil, ctx.Err())
					return
				case <-time.After(m.Delay):
				}
			}
			if err := ctx.Err(); err != nil {
				writer.Send(nil, err)
				return
			}
			if writer.Send(schema.AssistantMessage(string(r), nil), nil) {
				return
			}
		}
	}()
	return reader, nil
}
