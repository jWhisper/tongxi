package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
		a, err := s.SaveAgent(AgentInput{Name: name, Instruction: name + "-system", BaseURL: "https://example.test/v1", Model: name, APIKey: "group-secret", Tools: []string{"count_characters"}})
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
func TestEinoAsynchronousCollaborationAndBudget(t *testing.T) {
	for _, loop := range []bool{false, true} {
		t.Run(fmt.Sprint(loop), func(t *testing.T) {
			s, c, agents := groupService(t)
			created := 0
			s.newChatModel = func(_ context.Context, _ string, name string, _ string) (model.ToolCallingChatModel, error) {
				created++
				turn := created
				return &collaborationModel{next: func(ctx context.Context, in []*schema.Message, tools map[string]bool) (*schema.Message, error) {
					final := (!loop && turn == 3) || (loop && turn == 6)
					if final {
						if tools["send_message"] || name != "writer" || !strings.Contains(in[0].Content, "最终总结") {
							return nil, errors.New("final conclusion was not handed back to lead")
						}
					} else if !tools["list_agents"] || !tools["send_message"] {
						return nil, errors.New("collaboration tools missing")
					}
					if turn == 1 && (!strings.Contains(in[0].Content, agents[1].ID) || !strings.Contains(in[0].Content, "必须先")) {
						return nil, errors.New("lead missing roster or proactive orchestration instructions")
					}
					last := in[len(in)-1]
					if last.Role == schema.User {
						if turn > 1 && !strings.Contains(last.Content, fmt.Sprintf("public-turn-%d", turn-1)) {
							return nil, errors.New("missing predecessor public result")
						}
						for _, msg := range in[:len(in)-1] {
							if name == "reviewer" && turn == 2 && msg.Role == schema.Tool {
								return nil, errors.New("another agent's private trace leaked")
							}
						}
						if final {
							return schema.AssistantMessage(fmt.Sprintf("public-turn-%d 汇总完成", turn), nil), nil
						}
						if !loop && turn == 2 {
							// The member simply speaks: no explicit tool call back to the lead.
							return schema.AssistantMessage("public-turn-2 评审意见", nil), nil
						}
						return toolMessage("list_agents", `{}`), nil
					}
					if last.ToolName == "list_agents" {
						var members []store.Member
						if err := json.Unmarshal([]byte(last.Content), &members); err != nil || len(members) != 3 {
							return nil, errors.New("invalid member tool result")
						}
						target := agents[1].ID
						if name == "reviewer" {
							target = agents[2].ID
						}
						args, _ := json.Marshal(store.SendInput{TargetAgentID: target, Content: fmt.Sprintf("request-%d", turn)})
						return toolMessage("send_message", string(args)), nil
					}
					var result sendResult
					if err := json.Unmarshal([]byte(last.Content), &result); err != nil {
						return nil, err
					}
					if turn < 5 && result.Status != "queued" {
						return nil, fmt.Errorf("not queued: %s", last.Content)
					}
					if turn == 5 && (result.Status != "rejected" || !strings.Contains(result.Error, "上限")) {
						return nil, errors.New("loop limit not reported")
					}
					return schema.AssistantMessage(fmt.Sprintf("public-turn-%d", turn), nil), nil
				}}, nil
			}
			events := make(chan store.ConversationRun, 1000)
			s.StartScheduler(func(r store.ConversationRun) { events <- r })
			d, err := s.Schedule(store.ScheduleRequest{RequestID: "group-initial-delivery", ConversationID: c.ID, Action: "lead", Content: "合作写作"})
			if err != nil {
				t.Fatal(err)
			}
			n := 3
			if loop {
				n = 6
			}
			awaitGroup(t, events, n)
			detail, err := s.Conversation(c.ID)
			if err != nil {
				t.Fatal(err)
			}
			if len(detail.Runs) != n || detail.Chains[0].Status != "completed" || detail.Chains[0].Reserved != n {
				t.Fatal("incorrect collaboration", detail.Chains, len(detail.Runs))
			}
			for i, r := range detail.Runs {
				want := agents[0].ID
				if i > 0 && i < n-1 {
					want = agents[1+(i-1)%2].ID
				}
				if r.AgentID != want || r.ChainID != d.ChainID {
					t.Fatal("identity/chain crossed", r)
				}
			}
			if loop && !strings.Contains(detail.Chains[0].Reason, "上限") {
				t.Fatal("limit missing in UI")
			}
			if len(detail.Messages) != 2*n-1 {
				t.Fatal("unexpected broadcast/duplicate messages", len(detail.Messages))
			}
			// A final public message never schedules the unused third member.
			for _, r := range detail.Runs {
				if !loop && r.AgentID == agents[2].ID {
					t.Fatal("public reply broadcast to third member")
				}
			}
		})
	}
}

func TestEinoRoundMentionAndSummaryCannotSendMessages(t *testing.T) {
	s, c, agents := groupService(t)
	s.newChatModel = func(_ context.Context, _ string, name string, _ string) (model.ToolCallingChatModel, error) {
		return &collaborationModel{next: func(_ context.Context, in []*schema.Message, tools map[string]bool) (*schema.Message, error) {
			if tools["send_message"] || tools["list_agents"] {
				return nil, errors.New("scheduled speaker exposed collaboration tools")
			}
			last := in[len(in)-1]
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
