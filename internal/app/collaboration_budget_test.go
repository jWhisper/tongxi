package app

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"tongxi/internal/store"
)

func TestSchedulerHonorsCollaborationDeadlineBeforeAndDuringReply(t *testing.T) {
	for _, expired := range []bool{false, true} {
		t.Run(map[bool]string{false: "during-reply", true: "before-reply"}[expired], func(t *testing.T) {
			s, c, members := freeService(t)
			d, err := s.Schedule(store.ScheduleRequest{RequestID: "deadline-discussion", ConversationID: c.ID, Action: "discussion", Content: "讨论"})
			if err != nil {
				t.Fatal(err)
			}
			selector := d.Runs[0]
			selector.Status, selector.StartedAt = "running", time.Now().UTC().Format(time.RFC3339Nano)
			if _, err = s.db.ClaimConversationRun(selector, members[0]); err != nil {
				t.Fatal(err)
			}
			child, err := s.db.SelectDiscussionSpeaker(selector.ID, members[1].ID, "deadline-reply")
			if err != nil {
				t.Fatal(err)
			}
			selector.Status, selector.FinishedAt = "completed", time.Now().UTC().Format(time.RFC3339Nano)
			if err = s.db.FinishConversationRun(selector, nil, ""); err != nil {
				t.Fatal(err)
			}
			remaining := 500 * time.Millisecond
			if expired {
				remaining = -time.Second
			}
			fixture, err := sql.Open("sqlite", filepath.Join(s.db.Directory(), "tongxi.db"))
			if err != nil {
				t.Fatal(err)
			}
			_, err = fixture.Exec(`UPDATE runs SET started_at=? WHERE id=?`, time.Now().Add(-(store.DefaultCollaborationMinutes*time.Minute)+remaining).UTC().Format(time.RFC3339Nano), selector.ID)
			fixture.Close()
			if err != nil {
				t.Fatal(err)
			}
			called := false
			s.newChatModel = func(context.Context, string, string, string) (model.ToolCallingChatModel, error) {
				called = true
				return &scriptedModel{stream: func(ctx context.Context, _ []*schema.Message) (*schema.StreamReader[*schema.Message], error) {
					r, w := schema.Pipe[*schema.Message](2)
					w.Send(schema.AssistantMessage("已经收到的部分内容", nil), nil)
					go func() { defer w.Close(); <-ctx.Done(); w.Send(nil, ctx.Err()) }()
					return r, nil
				}}, nil
			}
			events := make(chan store.ConversationRun, 100)
			s.StartScheduler(func(r store.ConversationRun) { events <- r })
			finished := waitRun(t, events, child.ID, "interrupted")
			if called == expired || finished.Error != store.CollaborationTimeReason {
				t.Fatal("wrong deadline behavior", called, finished)
			}
			if !expired && !strings.Contains(finished.Text, "部分内容") {
				t.Fatal("partial response lost", finished)
			}
			detail, err := s.Conversation(c.ID)
			if err != nil || detail.Chains[0].Status != "incomplete" || len(detail.Messages) != 1 {
				t.Fatal("timeout published a reply or resumed discussion", detail, err)
			}
		})
	}
}

func TestTokenBudgetStopsBeforeLeadToolAndPersistsUsage(t *testing.T) {
	s, c, _ := groupService(t)
	c.TimeBudgetMinutes, c.TokenBudget = 0, 120
	var err error
	c, err = s.SaveConversation(c)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	s.newChatModel = func(context.Context, string, string, string) (model.ToolCallingChatModel, error) {
		return &scriptedModel{stream: func(context.Context, []*schema.Message) (*schema.StreamReader[*schema.Message], error) {
			calls++
			response := toolMessage("advance_work", `{"action":"complete","checks":[{"criterion":"可使用","status":"met","evidence":"已完成"}],"result":"结果","reason":"完成"}`)
			response.Content = "已经生成的内容"
			response.ResponseMeta = &schema.ResponseMeta{Usage: &schema.TokenUsage{PromptTokens: 100, CompletionTokens: 20, TotalTokens: 120, PromptTokenDetails: schema.PromptTokenDetails{CachedTokens: 60}}}
			return schema.StreamReaderFromArray([]*schema.Message{response}), nil
		}}, nil
	}
	events := make(chan store.ConversationRun, 100)
	s.StartScheduler(func(r store.ConversationRun) { events <- r })
	d, err := s.Schedule(store.ScheduleRequest{RequestID: "token-limit-integration", ConversationID: c.ID, Action: "lead", Content: "完成方案"})
	if err != nil {
		t.Fatal(err)
	}
	r := waitRun(t, events, d.Runs[0].ID, "interrupted")
	if r.Error != store.CollaborationTokenReason || calls != 1 || r.InputTokens != 100 || r.OutputTokens != 20 || r.CachedTokens != 60 || r.UsageEstimated {
		t.Fatal(r, calls)
	}
	detail, err := s.Conversation(c.ID)
	if err != nil || detail.Chains[0].Status != "incomplete" || len(detail.LeadSteps) != 0 || len(detail.Versions) != 0 || len(detail.Messages) != 1 {
		t.Fatal(detail, err)
	}
	persisted, err := s.db.ConversationRun(r.ID)
	if err != nil || persisted.InputTokens != 100 || persisted.CachedTokens != 60 {
		t.Fatal(persisted, err)
	}
}
