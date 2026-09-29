package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"tongxi/internal/store"
)

func freeService(t *testing.T) (*Service, store.Conversation, []store.Agent) {
	t.Helper()
	s, c, agents := groupService(t)
	c.Mode, c.LeadAgentID = "discussion", ""
	var err error
	c, err = s.SaveConversation(c)
	if err != nil {
		t.Fatal(err)
	}
	return s, c, agents
}

func awaitDiscussion(t *testing.T, s *Service, conversationID, chainID string, events <-chan store.ConversationRun) ConversationDetail {
	t.Helper()
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	for {
		select {
		case <-events:
			detail, err := s.Conversation(conversationID)
			if err != nil {
				t.Fatal(err)
			}
			for _, c := range detail.Chains {
				if c.ID == chainID && c.Status != "active" {
					if c.Status != "completed" {
						t.Fatalf("discussion failed: %s; runs: %+v", c.Reason, detail.Runs)
					}
					return detail
				}
			}
		case <-timer.C:
			t.Fatal("discussion stalled")
		}
	}
}

func TestEinoFreeDiscussionChoosesSkipsAndPausesWithoutHost(t *testing.T) {
	s, c, agents := freeService(t)
	selections := 0
	s.newChatModel = func(_ context.Context, _, name, _ string) (model.ToolCallingChatModel, error) {
		return &collaborationModel{next: func(_ context.Context, in []*schema.Message, tools map[string]bool) (*schema.Message, error) {
			if tools["choose_speaker"] {
				selections++
				if strings.Contains(in[0].Content, "writer-system") {
					return nil, errors.New("selector inherited member personality")
				}
				for _, m := range in {
					if m.Role == schema.Tool && m.ToolName != "choose_speaker" && m.ToolName != "pause_discussion" {
						return nil, errors.New("selector read member's private history")
					}
				}
				if selections == 4 {
					return toolMessage("pause_discussion", `{}`), nil
				}
				if selections == 2 && strings.Contains(in[0].Content, agents[1].ID) {
					return nil, errors.New("passed member selected again")
				}
				id := []string{agents[1].ID, agents[2].ID, agents[0].ID}[selections-1]
				return toolMessage("choose_speaker", fmt.Sprintf(`{"agent_id":%q}`, id)), nil
			}
			if !tools["send_message"] || !tools["skip_reply"] {
				return nil, errors.New("discussion member lacks participation tools")
			}
			if name == "reviewer" {
				return toolMessage("skip_reply", `{}`), nil
			}
			if name == "writer" && !strings.Contains(promptText(in), "planner-public") {
				return nil, errors.New("speaker missed previous opinion")
			}
			return schema.AssistantMessage(name+"-public", nil), nil
		}}, nil
	}
	events := make(chan store.ConversationRun, 500)
	s.StartScheduler(func(r store.ConversationRun) { events <- r })
	d, err := s.Schedule(store.ScheduleRequest{RequestID: "free-discussion-request", ConversationID: c.ID, Content: "讨论一个问题", Action: "discussion"})
	if err != nil {
		t.Fatal(err)
	}
	detail := awaitDiscussion(t, s, c.ID, d.ChainID, events)
	if len(detail.Messages) != 3 || len(detail.Runs) != 7 || detail.Chains[0].Reserved != 3 {
		t.Fatal("selection or silence appeared in chat", detail)
	}
	if !detail.Runs[1].Silent || detail.Messages[1].SenderID != agents[2].ID || detail.Messages[2].SenderID != agents[0].ID {
		t.Fatal("wrong free discussion sequence", detail)
	}
}

func TestNewUserMessageInterruptsSelectingOrSpeaking(t *testing.T) {
	for _, stage := range []string{"selector", "reply"} {
		t.Run(stage, func(t *testing.T) {
			s, c, agents := freeService(t)
			started := make(chan struct{})
			s.newChatModel = func(context.Context, string, string, string) (model.ToolCallingChatModel, error) {
				return &scriptedModel{stream: func(ctx context.Context, in []*schema.Message) (*schema.StreamReader[*schema.Message], error) {
					isSelector := strings.Contains(in[0].Content, "后台发言选择器")
					r, w := schema.Pipe[*schema.Message](3)
					if strings.Contains(in[len(in)-1].Content, "NEW-TOPIC") {
						w.Send(toolMessage("pause_discussion", `{}`), nil)
						w.Close()
						return r, nil
					}
					if stage == "reply" && isSelector {
						w.Send(toolMessage("choose_speaker", fmt.Sprintf(`{"agent_id":%q}`, agents[1].ID)), nil)
						w.Close()
						return r, nil
					}
					go func() {
						defer w.Close()
						close(started)
						<-ctx.Done()
						w.Send(schema.AssistantMessage("迟到的旧话题结果", nil), nil)
						w.Send(toolMessage("send_message", fmt.Sprintf(`{"target_agent_id":%q,"content":"late"}`, agents[2].ID)), nil)
					}()
					return r, nil
				}}, nil
			}
			events := make(chan store.ConversationRun, 100)
			s.StartScheduler(func(r store.ConversationRun) { events <- r })
			old, err := s.Schedule(store.ScheduleRequest{RequestID: "old-discussion-request", ConversationID: c.ID, Content: "OLD-TOPIC", Action: "discussion"})
			if err != nil {
				t.Fatal(err)
			}
			select {
			case <-started:
			case <-time.After(5 * time.Second):
				t.Fatal("old discussion never started")
			}
			newer, err := s.Schedule(store.ScheduleRequest{RequestID: "new-discussion-request", ConversationID: c.ID, Content: "NEW-TOPIC", Action: "discussion"})
			if err != nil {
				t.Fatal(err)
			}
			detail := awaitDiscussion(t, s, c.ID, newer.ChainID, events)
			if detail.Chains[0].ID != old.ChainID || detail.Chains[0].Status != "stopped" || len(detail.Messages) != 2 {
				t.Fatal("old discussion leaked work", detail)
			}
		})
	}
}
