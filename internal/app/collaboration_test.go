package app

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"tongxi/internal/store"
)

type collaborationModel struct {
	tools map[string]bool
	next  func(context.Context, []*schema.Message, map[string]bool) (*schema.Message, error)
}

func (m *collaborationModel) WithTools(in []*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	copy := *m
	copy.tools = map[string]bool{}
	for _, t := range in {
		copy.tools[t.Name] = true
	}
	return &copy, nil
}
func (m *collaborationModel) Generate(ctx context.Context, in []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	tools := m.tools
	if info := model.GetCommonOptions(nil, opts...).Tools; info != nil {
		tools = map[string]bool{}
		for _, item := range info {
			tools[item.Name] = true
		}
	}
	return m.next(ctx, in, tools)
}
func (m *collaborationModel) Stream(ctx context.Context, in []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	msg, err := m.Generate(ctx, in, opts...)
	if err != nil {
		return nil, err
	}
	r, w := schema.Pipe[*schema.Message](1)
	w.Send(msg, nil)
	w.Close()
	return r, nil
}
func toolMessage(name, args string) *schema.Message {
	index := 0
	return schema.AssistantMessage("", []schema.ToolCall{{Index: &index, ID: "call-" + name, Type: "function", Function: schema.FunctionCall{Name: name, Arguments: args}}})
}
func groupService(t *testing.T) (*Service, store.Conversation, []store.Agent) {
	t.Helper()
	s := newService(t)
	members := []store.Agent{}
	ids := []string{}
	for _, name := range []string{"writer", "reviewer", "planner"} {
		m := saveTestModel(t, s, name, "group-secret")
		a, err := s.SaveAgent(AgentInput{Name: name, Instruction: name + "-system", ModelID: m.ID, Tools: []string{"count_characters"}})
		if err != nil {
			t.Fatal(err)
		}
		members = append(members, a)
		ids = append(ids, a.ID)
	}
	c, err := s.SaveConversation(store.Conversation{Title: "group", Kind: "group", Mode: "lead", LeadAgentID: ids[0], MemberIDs: ids})
	if err != nil {
		t.Fatal(err)
	}
	return s, c, members
}
func awaitGroup(t *testing.T, events <-chan store.ConversationRun, n int) {
	t.Helper()
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	for done := 0; done < n; {
		select {
		case r := <-events:
			if r.Status == "failed" {
				t.Fatalf("unexpected failure: %s", r.Error)
			}
			if r.Status == "completed" {
				done++
			}
		case <-timer.C:
			t.Fatal("collaboration stalled")
		}
	}
}
func TestEinoRoundMentionAndSummaryCannotSendMessages(t *testing.T) {
	s, c, agents := groupService(t)
	s.newChatModel = func(_ context.Context, _ string, name string, _ string) (model.ToolCallingChatModel, error) {
		return &collaborationModel{next: func(_ context.Context, in []*schema.Message, tools map[string]bool) (*schema.Message, error) {
			if tools["send_message"] || tools["list_agents"] {
				return nil, errors.New("scheduled speaker exposed collaboration tools")
			}
			last := &schema.Message{Content: promptText(in)}
			if name == "reviewer" && !strings.Contains(last.Content, "writer-public") {
				return nil, errors.New("reviewer missed writer")
			}
			if name == "planner" && !strings.Contains(last.Content, "reviewer-public") {
				return nil, errors.New("planner missed reviewer")
			}
			return schema.AssistantMessage(name+"-public", nil), nil
		}}, nil
	}
	events := make(chan store.ConversationRun, 100)
	s.StartScheduler(func(r store.ConversationRun) { events <- r })
	d, err := s.Schedule(store.ScheduleRequest{RequestID: "group-round-delivery", ConversationID: c.ID, Content: "讨论", Action: "round", AgentIDs: c.MemberIDs})
	if err != nil {
		t.Fatal(err)
	}
	awaitGroup(t, events, 3)
	for _, action := range []string{"mention", "summary"} {
		one, err := s.Schedule(store.ScheduleRequest{RequestID: "group-" + action + "-delivery", ConversationID: c.ID, Content: action, Action: action, AgentIDs: []string{agents[0].ID}})
		if err != nil || len(one.Runs) != 1 {
			t.Fatal(err)
		}
		awaitGroup(t, events, 1)
	}
	detail, _ := s.Conversation(c.ID)
	if len(detail.Runs) != 5 || len(d.Runs) != 3 {
		t.Fatal("extra runs", len(detail.Runs))
	}
	for _, r := range detail.Runs {
		if r.Status != "completed" {
			t.Fatal(r)
		}
	}
}

func promptText(messages []*schema.Message) string {
	var out strings.Builder
	for _, message := range messages {
		out.WriteString(message.Content)
		out.WriteByte('\n')
	}
	return out.String()
}
