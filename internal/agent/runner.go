package agent

import (
	"context"
	"errors"
	"io"
	"net/http"
	"time"
	"unicode/utf8"

	"github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
)

const LocalPrompt = "请调用 count_characters 工具，统计「同席」的 Unicode 字符数，再用一句中文告诉我结果。"

type Update struct {
	Text string
	Tool string
}
type CountInput struct {
	Text string `json:"text" jsonschema:"description=要统计的文本"`
}
type CountOutput struct {
	Count int `json:"count"`
}

func NewModel(ctx context.Context, baseURL, name, key string) (model.ToolCallingChatModel, error) {
	return openai.NewChatModel(ctx, &openai.ChatModelConfig{
		BaseURL: baseURL, Model: name, APIKey: key,
		HTTPClient: &http.Client{Timeout: 90 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
	})
}

// Run exercises a single isolated conversation through the real Eino runner.
func Run(ctx context.Context, cm model.ToolCallingChatModel, prompt string, emit func(Update)) error {
	_, err := RunConversation(ctx, cm, Config{
		Name: "tongxi_probe", Description: "同席连接验证助手",
		Instruction: "你是同席的连接验证助手。用中文简洁回答。用户要求统计字符时必须调用 count_characters，并根据实际工具结果作答。",
		Tools:       []string{"count_characters"},
	}, []*schema.Message{schema.UserMessage(prompt)}, emit)
	return err
}

type Config struct {
	Name, Description, Instruction string
	Tools                          []string
	ExtraTools                     []tool.BaseTool
	ReturnDirectly                 map[string]bool
}

// RunConversation returns complete model/tool messages, including reasoning and
// tool-call IDs needed by providers when a conversation is continued.
func RunConversation(ctx context.Context, cm model.ToolCallingChatModel, config Config, history []*schema.Message, emit func(Update)) ([]*schema.Message, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	count, err := utils.InferTool("count_characters", "统计文本的 Unicode 码点数量（不是字节数或组合字素数）。", func(ctx context.Context, in *CountInput) (*CountOutput, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return &CountOutput{Count: utf8.RuneCountInString(in.Text)}, nil
	})
	if err != nil {
		return nil, err
	}
	tools := append([]tool.BaseTool{}, config.ExtraTools...)
	for _, name := range config.Tools {
		if name == "count_characters" {
			tools = append(tools, count)
		}
	}
	a, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name: config.Name, Description: config.Description, Model: cm, MaxIterations: 4,
		Instruction: config.Instruction,
		ToolsConfig: adk.ToolsConfig{ToolsNodeConfig: compose.ToolsNodeConfig{Tools: tools}, ReturnDirectly: config.ReturnDirectly},
	})
	if err != nil {
		return nil, err
	}
	iter := adk.NewRunner(ctx, adk.RunnerConfig{Agent: a, EnableStreaming: true}).Run(ctx, history)
	messages := []*schema.Message{}
	var firstErr error
	hasText := false
	direct := false
	for {
		event, ok := iter.Next()
		if !ok {
			break
		}
		if event.Err != nil && firstErr == nil {
			firstErr = event.Err
			cancel()
		}
		if event.Output == nil || event.Output.MessageOutput == nil {
			continue
		}
		out := event.Output.MessageOutput
		if firstErr != nil {
			if out.MessageStream != nil {
				out.MessageStream.Close()
			}
			continue
		}
		if out.Role == schema.Tool {
			emit(Update{Tool: out.ToolName})
			direct = direct || config.ReturnDirectly[out.ToolName]
		}
		messageHasText := false
		chunks := []*schema.Message{}
		onMessage := func(m *schema.Message) {
			if m != nil {
				chunks = append(chunks, m)
			}
			if m != nil && out.Role == schema.Assistant && m.Content != "" {
				if !messageHasText && hasText {
					emit(Update{Text: "\n\n"})
				}
				emit(Update{Text: m.Content})
				messageHasText = true
				hasText = true
			}
		}
		if out.IsStreaming {
			err = consume(out.MessageStream, onMessage)
		} else {
			onMessage(out.Message)
			err = nil
		}
		if len(chunks) > 0 {
			message, concatErr := schema.ConcatMessages(chunks)
			if concatErr == nil {
				messages = append(messages, message)
			}
			if err == nil {
				err = concatErr
			}
		}
		if err != nil {
			firstErr = err
			cancel()
		}
	}
	if ctx.Err() != nil && firstErr == nil {
		return messages, ctx.Err()
	}
	if firstErr != nil {
		return messages, firstErr
	}
	if !hasText && !direct {
		return messages, errors.New("模型未返回可展示的文本")
	}
	return messages, nil
}

func consume(stream *schema.StreamReader[*schema.Message], emit func(*schema.Message)) error {
	defer stream.Close()
	for {
		m, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		emit(m)
	}
}
