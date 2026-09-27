package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cloudwego/eino/schema"
)

func TestLocalToolAndStream(t *testing.T) {
	var text strings.Builder
	tools, chunks := 0, 0
	err := Run(context.Background(), &LocalModel{}, LocalPrompt, func(u Update) {
		if u.Tool != "" {
			if u.Tool != "count_characters" {
				t.Errorf("unexpected tool %s", u.Tool)
			}
			tools++
		}
		if u.Text != "" {
			text.WriteString(u.Text)
			chunks++
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if tools != 1 || chunks < 2 || !strings.Contains(text.String(), `"count":2`) {
		t.Fatalf("tools=%d chunks=%d text=%s", tools, chunks, text.String())
	}
}

func TestCancelStreamingRun(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, &LocalModel{Delay: time.Hour}, LocalPrompt, func(u Update) {
			if u.Tool != "" {
				cancel()
			}
		})
	}()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected cancelled, got %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("cancelled runner did not release streams")
	}
}

func TestDeadlineStopsEinoRun(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	err := Run(ctx, &LocalModel{Delay: time.Hour}, LocalPrompt, func(Update) {})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected deadline error, got %v", err)
	}
}

func TestCompatibleAPIStreamsToolResultBackToModel(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" || r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("unexpected request path or authorization")
		}
		var body struct {
			Stream   bool              `json:"stream"`
			Messages []*schema.Message `json:"messages"`
			Tools    []json.RawMessage `json:"tools"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		if !body.Stream || len(body.Tools) != 1 {
			t.Errorf("missing stream or tool binding")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		if calls.Add(1) == 1 {
			fmt.Fprint(w, "data: {\"id\":\"first\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"tool_calls\":[{\"index\":0,\"id\":\"call-count\",\"type\":\"function\",\"function\":{\"name\":\"count_characters\",\"arguments\":\"{\\\"text\\\":\\\"同席\\\"}\"}}]},\"finish_reason\":null}]}\n\n")
			fmt.Fprint(w, "data: {\"id\":\"first\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"tool_calls\"}]}\n\n")
		} else {
			last := body.Messages[len(body.Messages)-1]
			if last.Role != schema.Tool || last.ToolCallID != "call-count" || !strings.Contains(last.Content, `"count":2`) {
				t.Errorf("tool result not correctly paired: %+v", last)
			}
			for _, chunk := range []string{"共有", " 2 ", "个字符。"} {
				encoded, _ := json.Marshal(chunk)
				fmt.Fprintf(w, "data: {\"id\":\"second\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":%s},\"finish_reason\":null}]}\n\n", encoded)
			}
			fmt.Fprint(w, "data: {\"id\":\"second\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n")
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer server.Close()
	cm, err := NewModel(context.Background(), server.URL+"/v1", "test-model", "test-key")
	if err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	err = Run(context.Background(), cm, LocalPrompt, func(u Update) { out.WriteString(u.Text) })
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 || out.String() != "共有 2 个字符。" {
		t.Fatalf("calls=%d text=%q", calls.Load(), out.String())
	}
}

func TestCompatibleAPICancellationClosesRequest(t *testing.T) {
	started, closed := make(chan struct{}), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		w.(http.Flusher).Flush()
		close(started)
		<-r.Context().Done()
		close(closed)
	}))
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cm, err := NewModel(ctx, server.URL+"/v1", "test-model", "test-key")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- Run(ctx, cm, "你好", func(Update) {}) }()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("request did not start")
	}
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected cancellation error")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("runner stuck")
	}
	select {
	case <-closed:
	case <-time.After(3 * time.Second):
		t.Fatal("HTTP request not cancelled")
	}
}
