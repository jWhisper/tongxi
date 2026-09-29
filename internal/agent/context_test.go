package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/cloudwego/eino/adk/filesystem"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
	"github.com/cloudwego/eino/schema"
)

type memoryContextBackend struct {
	sync.Mutex
	records map[string]string
}

func (b *memoryContextBackend) Write(_ context.Context, r *filesystem.WriteRequest) error {
	b.Lock()
	defer b.Unlock()
	if _, ok := b.records[r.FilePath]; ok {
		return errors.New("overwrote a tool record")
	}
	b.records[r.FilePath] = r.Content
	return nil
}

type contextModel struct {
	t           *testing.T
	capacity    int
	summaries   int
	requests    int
	seenEarly   bool
	failSummary bool
	toolRounds  int
	tools       []*schema.ToolInfo
	last        []*schema.Message
}

func (m *contextModel) WithTools(tools []*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	m.tools = tools
	return m, nil
}
func (m *contextModel) Generate(_ context.Context, in []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	m.summaries++
	if m.failSummary {
		return nil, errors.New("summary unavailable")
	}
	o := model.GetCommonOptions(&model.Options{}, opts...)
	if o.MaxTokens == nil || estimateTokens(in, nil)+*o.MaxTokens > m.capacity {
		m.t.Fatal("summary request exceeded capacity")
	}
	for _, msg := range in {
		if strings.Contains(msg.Content, "场地A因噪音被拒绝") {
			m.seenEarly = true
		}
	}
	answer := schema.AssistantMessage("历史决定：场地A因噪音被拒绝（消息1）；旧预算800，后来用户明确改为200。尚待核对实际费用。", nil)
	// Kimi may return substantial reasoning alongside a short summary. Only
	// the summary body becomes the saved context; reasoning must not reject it.
	answer.ReasoningContent = strings.Repeat("internal reasoning ", 1000)
	return answer, nil
}
func (m *contextModel) Stream(_ context.Context, in []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	m.requests++
	m.last = in
	o := model.GetCommonOptions(&model.Options{}, opts...)
	tools := m.tools
	if o.Tools != nil {
		tools = o.Tools
	}
	if o.MaxTokens == nil || estimateTokens(in, tools)+*o.MaxTokens > m.capacity-m.capacity/10 {
		m.t.Fatal("model input/output exceeded budget")
	}
	if m.requests <= m.toolRounds {
		return schema.StreamReaderFromArray([]*schema.Message{schema.AssistantMessage("", []schema.ToolCall{{ID: fmt.Sprint(m.requests), Type: "function", Function: schema.FunctionCall{Name: "large_result", Arguments: `{"text":"读取"}`}}})}), nil
	}
	return schema.StreamReaderFromArray([]*schema.Message{schema.AssistantMessage("按当前200元预算继续，场地A曾因噪音被拒绝。", nil)}), nil
}

func TestContextBudgetBatchesOldHistoryAndPreservesCurrentRequirements(t *testing.T) {
	cm := &contextModel{t: t, capacity: 8192}
	history := []*schema.Message{schema.UserMessage("消息1：场地A因噪音被拒绝。预算800元。")}
	for i := 2; i <= 120; i++ {
		history = append(history, schema.UserMessage(fmt.Sprintf("消息%d：%s", i, strings.Repeat("历史讨论与资料。", 60))))
	}
	history = append(history, schema.UserMessage("最新要求：预算改成200元，继续修改。"))
	c := &ContextConfig{Capacity: cm.capacity}
	var phases []bool
	_, err := RunConversation(context.Background(), cm, Config{Name: "test", Instruction: "权威任务状态：预算200元，验收不超支，保留当前成果。", Context: c}, history, func(u Update) {
		if u.Compacting != nil {
			phases = append(phases, *u.Compacting)
		}
		if u.Text != "" && len(phases) > 0 && phases[len(phases)-1] {
			t.Fatal("reply started before compression status cleared")
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if cm.summaries < 2 || !cm.seenEarly || c.Compactions == 0 || len(c.Result) == 0 {
		t.Fatal("uncovered old history", cm.summaries, c.Compactions)
	}
	if !strings.Contains(cm.last[0].Content, "权威任务状态：预算200元") || cm.last[len(cm.last)-1].Content != "最新要求：预算改成200元，继续修改。" {
		t.Fatal("current state was summarized")
	}
	if len(c.Result) >= len(history) {
		t.Fatal("compaction was not retained")
	}
	if len(phases) != 2*c.Compactions {
		t.Fatal("missing compression lifecycle", phases)
	}
	for i, active := range phases {
		if active != (i%2 == 0) {
			t.Fatal("invalid compression lifecycle", phases)
		}
	}
}

func TestOversizedOldMessageAndSummaryFailure(t *testing.T) {
	for _, fail := range []bool{false, true} {
		cm := &contextModel{t: t, capacity: 8192, failSummary: fail}
		c := &ContextConfig{Capacity: cm.capacity}
		var phases []bool
		_, err := RunConversation(context.Background(), cm, Config{Name: "test", Context: c}, []*schema.Message{schema.UserMessage("场地A因噪音被拒绝" + strings.Repeat("很长的原始资料", 5000)), schema.AssistantMessage("已收到", nil), schema.UserMessage("继续，预算改为200")}, func(u Update) {
			if u.Compacting != nil {
				phases = append(phases, *u.Compacting)
			}
		})
		if len(phases) < 2 || !phases[0] || phases[len(phases)-1] {
			t.Fatal("compression status not cleared", phases)
		}
		if fail {
			if err == nil || cm.requests != 0 || c.Result != nil {
				t.Fatal("summary failure advanced execution")
			}
		} else if err != nil || cm.summaries < 2 {
			t.Fatal("oversized record not batched", err)
		}
	}
}

func TestReductionOffloadsGrowingToolResultsAndKeepsPairs(t *testing.T) {
	backend := &memoryContextBackend{records: map[string]string{}}
	cm := &contextModel{t: t, capacity: 8192, toolRounds: 5}
	large, err := utils.InferTool("large_result", "返回资料", func(context.Context, *CountInput) (string, error) {
		return strings.Repeat("工具真实输出", 4000), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	c := &ContextConfig{Capacity: cm.capacity, Backend: backend}
	_, err = RunConversation(context.Background(), cm, Config{Name: "test", Context: c, MaxIterations: 8, ExtraTools: []tool.BaseTool{large}}, []*schema.Message{schema.UserMessage("读取资料后核对")}, func(Update) {})
	if err != nil {
		t.Fatal(err)
	}
	if len(backend.records) < 5 || cm.requests != 6 || len(c.Result) == 0 {
		t.Fatal("tool results not reduced during run")
	}
	calls := map[string]bool{}
	for _, msg := range c.Result {
		for _, call := range msg.ToolCalls {
			calls[call.ID] = true
		}
		if msg.Role == schema.Tool && !calls[msg.ToolCallID] {
			t.Fatal("orphan tool result")
		}
	}
}

func TestTokenEstimateIncludesToolsAndCalibratesUsage(t *testing.T) {
	cm := &contextModel{t: t, capacity: 8192}
	c := &ContextConfig{Capacity: 8192}
	handlers, _, err := newContextHandlers(context.Background(), cm, c, nil)
	if err != nil {
		t.Fatal(err)
	}
	m := handlers[1].(*contextManager)
	info := &schema.ToolInfo{Name: "test", Desc: strings.Repeat("工具说明", 100), ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{"query": {Type: schema.String, Desc: strings.Repeat("详细参数", 100)}})}
	msgs := []*schema.Message{schema.SystemMessage("系统"), schema.UserMessage("预算200元")}
	if estimateTokens(msgs, []*schema.ToolInfo{info}) <= estimateTokens(msgs, nil)+1000 {
		t.Fatal("tool schema excluded")
	}
	m.calibrate(&schema.Message{ResponseMeta: &schema.ResponseMeta{Usage: &schema.TokenUsage{PromptTokens: 2000}}}, 1000)
	if c.Ratio != 2 || m.count(msgs, nil) <= estimateTokens(msgs, nil)*2 {
		t.Fatal("usage calibration not applied")
	}
}

func TestCurrentRequestUsesRemainingBudgetWithoutBeingSummarized(t *testing.T) {
	for _, size := range []int{1100, 4000} {
		cm := &contextModel{t: t, capacity: 8192}
		c := &ContextConfig{Capacity: 8192}
		request := strings.Repeat("当前", size)
		_, err := RunConversation(context.Background(), cm, Config{Name: "test", Context: c}, []*schema.Message{schema.UserMessage(request)}, func(Update) {})
		if size == 1100 {
			if err != nil || cm.requests != 1 || cm.last[0].Content != request {
				t.Fatal("soft threshold rejected an otherwise valid current request", err)
			}
		} else if err == nil || cm.requests != 0 {
			t.Fatal("oversized current requirement was sent or discarded")
		}
		if cm.summaries != 0 {
			t.Fatal("current request was summarized")
		}
	}
}
