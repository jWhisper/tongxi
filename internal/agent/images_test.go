package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
	"github.com/cloudwego/eino/schema"
)

func testImageMessage() *schema.Message {
	url := "data:image/jpeg;base64," + strings.Repeat("A", 10000)
	return &schema.Message{Role: schema.Tool, ToolName: "read_image", ToolCallID: "image-1", UserInputMultiContent: []schema.MessageInputPart{
		{Type: schema.ChatMessagePartTypeText, Text: "source_id=photo, hash=123, name=poster.png"},
		{Type: schema.ChatMessagePartTypeImageURL, Image: &schema.MessageInputImage{MessagePartCommon: schema.MessagePartCommon{URL: &url}}},
	}}
}

func TestImagesRetainReferencesWithoutPersistingPixels(t *testing.T) {
	m := testImageMessage()
	clean := WithoutImageData([]*schema.Message{m})
	encoded, _ := json.Marshal(clean)
	if strings.Contains(string(encoded), "base64") || !strings.Contains(clean[0].Content, "poster.png") || clean[0].ToolCallID != "image-1" {
		t.Fatal("image history was not stripped correctly")
	}
	if len(m.UserInputMultiContent) != 2 {
		t.Fatal("mutated active run")
	}
	if estimateTokens([]*schema.Message{m}, nil) < 4096 {
		t.Fatal("image missing from budget")
	}
	request := imageRequest([]*schema.Message{m, {Role: schema.Tool, ToolCallID: "second", Content: "done"}})
	if len(request) != 3 || request[0].Role != schema.Tool || request[1].ToolCallID != "second" || request[2].Role != schema.User {
		t.Fatal("tool-call pairing changed")
	}
	if len(request[0].UserInputMultiContent) != 0 || len(request[2].UserInputMultiContent) != 2 {
		t.Fatal("image not placed in user message")
	}
}

func TestEnhancedImageToolReachesCompatibleAPI(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []struct {
				Role       string          `json:"role"`
				Content    json.RawMessage `json:"content"`
				ToolCallID string          `json:"tool_call_id"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		if calls.Add(1) == 1 {
			fmt.Fprint(w, `data: {"choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"image-1","type":"function","function":{"name":"read_image","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}`+"\n\n")
		} else {
			last := body.Messages[len(body.Messages)-1]
			prior := body.Messages[len(body.Messages)-2]
			if last.Role != "user" || !strings.Contains(string(last.Content), "image_url") || !strings.Contains(string(last.Content), "data:image/png;base64,") || prior.Role != "tool" || prior.ToolCallID != "image-1" || !strings.HasPrefix(string(prior.Content), `"`) {
				t.Errorf("compatible request lacks paired tool result and image")
			}
			fmt.Fprint(w, `data: {"choices":[{"index":0,"delta":{"role":"assistant","content":"图片已分析"},"finish_reason":"stop"}]}`+"\n\n")
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer server.Close()
	cm, err := NewModel(context.Background(), server.URL+"/v1", "any-vision-model", "test-key")
	if err != nil {
		t.Fatal(err)
	}
	imageTool, err := utils.InferEnhancedTool("read_image", "read image", func(context.Context, *struct{}) (*schema.ToolResult, error) {
		url := "data:image/png;base64,aW1hZ2U="
		return &schema.ToolResult{Parts: []schema.ToolOutputPart{{Type: schema.ToolPartTypeText, Text: "poster.png"}, {Type: schema.ToolPartTypeImage, Image: &schema.ToolOutputImage{MessagePartCommon: schema.MessagePartCommon{URL: &url}}}}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	config := Config{Name: "test", ExtraTools: []tool.BaseTool{imageTool}, Context: &ContextConfig{Capacity: 32768}}
	_, err = RunConversation(context.Background(), cm, config, []*schema.Message{schema.UserMessage("查看图片")}, func(Update) {})
	if err != nil || calls.Load() != 2 {
		t.Fatal(calls.Load(), err)
	}
	stored, _ := json.Marshal(config.Context.Result)
	if strings.Contains(string(stored), "base64,") {
		t.Fatal("stored context includes pixels")
	}
}

func TestUnsupportedImageErrorPreservesProviderReason(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(400)
		fmt.Fprint(w, `{"error":{"message":"this model does not support image input","type":"invalid_request_error"}}`)
	}))
	defer server.Close()
	cm, err := NewModel(context.Background(), server.URL, "text-only-model", "test-key")
	if err != nil {
		t.Fatal(err)
	}
	_, err = cm.Stream(context.Background(), []*schema.Message{testImageMessage()})
	if err == nil || !strings.Contains(err.Error(), "不支持图片") || !strings.Contains(err.Error(), "does not support image") {
		t.Fatal(err)
	}
	_, err = cm.Stream(context.Background(), []*schema.Message{schema.UserMessage("hello")})
	if err == nil || strings.Contains(err.Error(), "当前模型接口不支持图片") {
		t.Fatal("misclassified request without images", err)
	}
}

func TestImageSummaryOmitsPixelsAndKeepsFileReference(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			return
		}
		requests.Add(1)
		if len(body.Messages) != 2 || !strings.Contains(body.Messages[1].Content, "poster.png") || strings.Contains(body.Messages[1].Content, "base64") {
			t.Error("summary missing reference or includes pixels")
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"choices":[{"index":0,"message":{"role":"assistant","content":"已读取 poster.png，需核对时重新读图。"},"finish_reason":"stop"}]}`)
	}))
	defer server.Close()
	cm, err := NewModel(context.Background(), server.URL, "any-model", "test-key")
	if err != nil {
		t.Fatal(err)
	}
	m := &contextManager{model: cm, config: &ContextConfig{Capacity: 32768, Ratio: 1}, inputBudget: 20000, outputBudget: 4000}
	_, err = m.summarize(context.Background(), []*schema.Message{testImageMessage()})
	if err != nil || requests.Load() != 1 {
		t.Fatal(err, requests.Load())
	}
}
