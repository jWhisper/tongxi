package app

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"tongxi/internal/agent"
	"tongxi/internal/store"
)

type scriptedModel struct {
	stream func(context.Context, []*schema.Message) (*schema.StreamReader[*schema.Message], error)
}

func (m *scriptedModel) WithTools([]*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	return m, nil
}
func (m *scriptedModel) Generate(context.Context, []*schema.Message, ...model.Option) (*schema.Message, error) {
	return nil, errors.New("expected streaming")
}
func (m *scriptedModel) Stream(ctx context.Context, in []*schema.Message, _ ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	return m.stream(ctx, in)
}

func chatFixture(t *testing.T, s *Service) (store.Agent, store.Conversation, store.Conversation) {
	t.Helper()
	a, err := s.SaveAgent(AgentInput{Name: "原名", Instruction: "原始指令", BaseURL: "https://example.test/v1", Model: "test", APIKey: "chat-secret", Tools: []string{"count_characters"}})
	if err != nil {
		t.Fatal(err)
	}
	create := func(title string) store.Conversation {
		c, err := s.SaveConversation(store.Conversation{Title: title, Kind: "private", Mode: "lead", LeadAgentID: a.ID, MemberIDs: []string{a.ID}})
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	return a, create("one"), create("two")
}

func waitRun(t *testing.T, events <-chan store.ConversationRun, id, status string) store.ConversationRun {
	t.Helper()
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	for {
		select {
		case r := <-events:
			if r.ID == id && r.Status == status {
				return r
			}
		case <-timer.C:
			t.Fatalf("timed out waiting for %s %s", id, status)
		}
	}
}

func TestSchedulerQueueHistorySnapshotAndRestart(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	vault := memoryVault{}
	s := NewService(context.Background(), db, vault, func(store.ProbeRun) {})
	a, one, two := chatFixture(t, s)
	calls := make(chan []*schema.Message, 16)
	gate := make(chan struct{})
	s.newChatModel = func(context.Context, string, string, string) (model.ToolCallingChatModel, error) {
		return &scriptedModel{stream: func(ctx context.Context, in []*schema.Message) (*schema.StreamReader[*schema.Message], error) {
			calls <- in
			if in[len(in)-1].Content == "first" {
				select {
				case <-gate:
				case <-ctx.Done():
					return nil, ctx.Err()
				}
			}
			return (&agent.LocalModel{}).Stream(ctx, in)
		}}, nil
	}
	events := make(chan store.ConversationRun, 1000)
	s.StartScheduler(func(r store.ConversationRun) { events <- r })
	t.Cleanup(func() { s.Close() })
	first, err := s.SendMessage(one.ID, "first", "delivery-00000001")
	if err != nil {
		t.Fatal(err)
	}
	firstInput := <-calls
	second, err := s.SendMessage(one.ID, "second", "delivery-00000002")
	if err != nil {
		t.Fatal(err)
	}
	third, err := s.SendMessage(two.ID, "third", "delivery-00000003")
	if err != nil {
		t.Fatal(err)
	}
	dup, err := s.SendMessage(one.ID, "first", "delivery-00000001")
	if err != nil || dup.ID != first.ID {
		t.Fatal("duplicate delivery", err)
	}
	detail, _ := s.Conversation(one.ID)
	if len(detail.Messages) != 2 || len(detail.Runs) != 2 {
		t.Fatal("duplicate persisted or queue missing")
	}
	if _, err = s.StartProbe("local", ""); err == nil {
		t.Fatal("probe overlapped global execution slot")
	}
	if _, err = s.SaveAgent(AgentInput{ID: a.ID, Version: a.Version, Name: "新名", Instruction: "新指令", BaseURL: a.BaseURL, Model: a.Model, Tools: a.Tools}); err != nil {
		t.Fatal(err)
	}
	if firstInput[0].Content != "原始指令" {
		t.Fatal("initial configuration incorrect", firstInput[0])
	}
	if err = s.StopRun(third.ID); err != nil {
		t.Fatal(err)
	}
	close(gate)
	r := waitRun(t, events, first.ID, "completed")
	if r.AgentName != "原名" || len(r.Tools) != 1 {
		t.Fatal("running config changed or tool missing", r)
	}
	waitRun(t, events, second.ID, "completed")
	<-calls // First tool result request.
	secondInput := <-calls
	if secondInput[0].Content != "新指令" || len(secondInput) != 6 || len(secondInput[2].ToolCalls) != 1 || secondInput[3].Role != schema.Tool {
		t.Fatalf("second turn lost transcript: %+v", secondInput)
	}
	<-calls // Second tool result request.
	other, err := s.SendMessage(two.ID, "isolated", "delivery-00000004")
	if err != nil {
		t.Fatal(err)
	}
	waitRun(t, events, other.ID, "completed")
	otherInput := <-calls
	if len(otherInput) != 2 {
		t.Fatal("conversation history crossed", len(otherInput))
	}
	<-calls
	detail, _ = s.Conversation(one.ID)
	if len(detail.Messages) != 4 || detail.Messages[2].SenderName != "原名" {
		t.Fatal("public replies or frozen name incorrect", detail.Messages)
	}
	s.Close()
	db, err = store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	s = NewService(context.Background(), db, vault, func(store.ProbeRun) {})
	s.newChatModel = func(context.Context, string, string, string) (model.ToolCallingChatModel, error) {
		return &scriptedModel{stream: func(ctx context.Context, in []*schema.Message) (*schema.StreamReader[*schema.Message], error) {
			calls <- in
			return (&agent.LocalModel{}).Stream(ctx, in)
		}}, nil
	}
	// Enqueue before the worker exists: startup must scan durable work.
	after, err := s.SendMessage(one.ID, "after restart", "delivery-00000005")
	if err != nil {
		t.Fatal(err)
	}
	s.StartScheduler(func(r store.ConversationRun) { events <- r })
	waitRun(t, events, after.ID, "completed")
	afterInput := <-calls
	if len(afterInput) != 10 {
		t.Fatal("restart lost transcript", len(afterInput))
	}
}

func TestSchedulerCancelErrorAndTimeout(t *testing.T) {
	for _, mode := range []string{"cancel", "error", "timeout"} {
		t.Run(mode, func(t *testing.T) {
			s := newService(t)
			_, one, _ := chatFixture(t, s)
			if mode == "timeout" {
				s.chatTimeout = 40 * time.Millisecond
			}
			s.newChatModel = func(context.Context, string, string, string) (model.ToolCallingChatModel, error) {
				return &scriptedModel{stream: func(ctx context.Context, in []*schema.Message) (*schema.StreamReader[*schema.Message], error) {
					reader, writer := schema.Pipe[*schema.Message](3)
					go func() {
						defer writer.Close()
						writer.Send(schema.AssistantMessage("部分内容", nil), nil)
						if mode == "error" {
							writer.Send(nil, errors.New("provider failed chat-secret"))
							return
						}
						<-ctx.Done()
						writer.Send(schema.AssistantMessage("迟到内容", nil), nil)
						writer.Send(nil, ctx.Err())
					}()
					return reader, nil
				}}, nil
			}
			events := make(chan store.ConversationRun, 100)
			s.StartScheduler(func(r store.ConversationRun) { events <- r })
			r, err := s.SendMessage(one.ID, "run", "delivery-failure1")
			if err != nil {
				t.Fatal(err)
			}
			if mode == "cancel" {
				for {
					update := waitRun(t, events, r.ID, "running")
					if update.Text != "" {
						break
					}
				}
				if err = s.StopRun(r.ID); err != nil {
					t.Fatal(err)
				}
				waitRun(t, events, r.ID, "cancelled")
				waitRun(t, events, r.ID, "cancelled") // Worker drained the deliberately late event.
			} else {
				waitRun(t, events, r.ID, "failed")
			}
			detail, err := s.Conversation(one.ID)
			if err != nil {
				t.Fatal(err)
			}
			if len(detail.Messages) != 1 {
				t.Fatal("failed/partial reply published")
			}
			saved := detail.Runs[0]
			persisted, err := s.db.ConversationRun(saved.ID)
			if err != nil || persisted.Status != saved.Status || persisted.Text != saved.Text || persisted.Revision != saved.Revision {
				t.Fatal("database and UI snapshot disagree", persisted, saved, err)
			}
			if mode == "cancel" && saved.Text != "部分内容" {
				t.Fatal("late output displayed", saved)
			}
			if strings.Contains(saved.Error, "chat-secret") {
				t.Fatal("secret in error")
			}
			if mode == "timeout" && !strings.Contains(saved.Error, "超时") {
				t.Fatal("deadline not explained", saved)
			}
			history, err := s.db.ModelHistory(saved.AgentID, one.ID)
			if err != nil || len(history) != 0 {
				t.Fatal("incomplete turn replayed")
			}
		})
	}
}

func TestStopStillCancelsWhenStorageUnavailable(t *testing.T) {
	s := newService(t)
	_, one, _ := chatFixture(t, s)
	stopped := make(chan struct{})
	s.newChatModel = func(context.Context, string, string, string) (model.ToolCallingChatModel, error) {
		return &scriptedModel{stream: func(ctx context.Context, _ []*schema.Message) (*schema.StreamReader[*schema.Message], error) {
			reader, writer := schema.Pipe[*schema.Message](1)
			go func() {
				defer writer.Close()
				writer.Send(schema.AssistantMessage("部分", nil), nil)
				<-ctx.Done()
				close(stopped)
				writer.Send(nil, ctx.Err())
			}()
			return reader, nil
		}}, nil
	}
	events := make(chan store.ConversationRun, 32)
	s.StartScheduler(func(r store.ConversationRun) { events <- r })
	r, err := s.SendMessage(one.ID, "run", "delivery-disk-failure")
	if err != nil {
		t.Fatal(err)
	}
	for {
		update := waitRun(t, events, r.ID, "running")
		if update.Text != "" {
			break
		}
	}
	s.db.Close()
	if err = s.StopRun(r.ID); err == nil {
		t.Fatal("missing persistence error")
	}
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("storage failure prevented model cancellation")
	}
	waitRun(t, events, r.ID, "failed")
}
