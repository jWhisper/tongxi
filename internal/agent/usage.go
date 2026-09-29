package agent

import (
	"context"
	"errors"
	"io"
	"math"
	"sync"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

type TokenUsage struct {
	Input, Output, Cached int
	Estimated             bool
}

// MeterModel wraps the underlying model, so normal replies, tool iterations,
// selectors and summarization all follow the same accounting and budget checks.
func MeterModel(cm model.ToolCallingChatModel, ratio float64, before func() error, record func(TokenUsage) error) model.ToolCallingChatModel {
	return &meteredModel{model: cm, ratio: max(1, ratio), before: before, record: record}
}

type meteredModel struct {
	model  model.ToolCallingChatModel
	tools  []*schema.ToolInfo
	ratio  float64
	before func() error
	record func(TokenUsage) error
}

func (m *meteredModel) WithTools(tools []*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	bound, err := m.model.WithTools(tools)
	if err != nil {
		return nil, err
	}
	copy := *m
	copy.model, copy.tools = bound, tools
	return &copy, nil
}

func (m *meteredModel) usage(input []*schema.Message, chunks []*schema.Message) TokenUsage {
	// Usage chunks are cumulative for a single provider request. ConcatMessages
	// takes maxima, rather than charging the same snapshot more than once.
	response, _ := schema.ConcatMessages(chunks)
	if response != nil && response.ResponseMeta != nil && response.ResponseMeta.Usage != nil {
		u := response.ResponseMeta.Usage
		if u.PromptTokens > 0 || u.CompletionTokens > 0 {
			return TokenUsage{Input: u.PromptTokens, Output: u.CompletionTokens, Cached: u.PromptTokenDetails.CachedTokens}
		}
	}
	output := 0
	if response != nil {
		output = max(0, estimateTokens([]*schema.Message{response}, nil)-11)
	}
	return TokenUsage{Input: int(math.Ceil(float64(estimateTokens(input, m.tools)) * m.ratio)), Output: output, Estimated: true}
}

func (m *meteredModel) Generate(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	if err := m.before(); err != nil {
		return nil, err
	}
	response, err := m.model.Generate(ctx, input, opts...)
	if response != nil {
		if usageErr := m.record(m.usage(input, []*schema.Message{response})); usageErr != nil {
			return response, usageErr
		}
	}
	return response, err
}

func (m *meteredModel) Stream(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	if err := m.before(); err != nil {
		return nil, err
	}
	source, err := m.model.Stream(ctx, input, opts...)
	if err != nil {
		return nil, err
	}
	chunks := []*schema.Message{}
	var once sync.Once
	var usageErr error
	save := func() error {
		once.Do(func() {
			if len(chunks) > 0 {
				usageErr = m.record(m.usage(input, chunks))
			}
		})
		return usageErr
	}
	return schema.StreamReaderWithConvert(source, func(chunk *schema.Message) (*schema.Message, error) {
		if chunk != nil {
			chunks = append(chunks, chunk)
		}
		return chunk, nil
	}, schema.WithOnEOF(func() (any, error) {
		if err := save(); err != nil {
			return nil, err
		}
		return nil, io.EOF
	}), schema.WithErrWrapper(func(err error) error {
		return errors.Join(err, save())
	})), nil
}
