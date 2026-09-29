package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"tongxi/internal/store"
)

func TestHistoryToolsSearchThenReadOriginalPublicMessage(t *testing.T) {
	s := newService(t)
	_, one, two := chatFixture(t, s)
	m, err := s.db.AppendMessage(store.Message{ID: newID(), ConversationID: one.ID, SenderType: "user", SenderName: "你", Content: "第一个场地因为隔壁施工，噪音太大，所以没有选。"})
	if err != nil {
		t.Fatal(err)
	}
	s.db.AppendMessage(store.Message{ID: newID(), ConversationID: two.ID, SenderType: "user", Content: "别的会话秘密"})
	step := 0
	s.newChatModel = func(context.Context, string, string, string) (model.ToolCallingChatModel, error) {
		return &collaborationModel{next: func(_ context.Context, in []*schema.Message, tools map[string]bool) (*schema.Message, error) {
			if !tools["search_chat_history"] || !tools["read_chat_history"] {
				t.Fatal("history tools missing")
			}
			step++
			switch step {
			case 1:
				return toolMessage("search_chat_history", `{"query":"场地","before_sequence":0}`), nil
			case 2:
				var found historyResult
				if err := json.Unmarshal([]byte(in[len(in)-1].Content), &found); err != nil {
					t.Fatal(err)
				}
				seen := false
				for _, hit := range found.Messages {
					if hit.ID == m.ID {
						seen = true
					}
				}
				if !seen || strings.Contains(in[len(in)-1].Content, "别的会话秘密") {
					t.Fatal("search result incorrect")
				}
				return toolMessage("read_chat_history", fmt.Sprintf(`{"message_id":%q,"sequence":0,"radius":1,"offset":0}`, m.ID)), nil
			default:
				if !strings.Contains(in[len(in)-1].Content, "隔壁施工") {
					t.Fatal("original reason unavailable")
				}
				return schema.AssistantMessage("第1条消息中你说隔壁施工、噪音太大，因此没有选第一个场地。", nil), nil
			}
		}}, nil
	}
	events := make(chan store.ConversationRun, 100)
	s.StartScheduler(func(r store.ConversationRun) { events <- r })
	r, err := s.SendMessage(one.ID, "为什么之前没有选第一个场地？", newID())
	if err != nil {
		t.Fatal(err)
	}
	finished := waitRun(t, events, r.ID, "completed")
	if len(finished.Tools) != 2 {
		t.Fatal("history tools not actually called", finished.Tools)
	}
}

func TestCancelDuringCompactionClearsLiveActivity(t *testing.T) {
	s, c, members := groupService(t)
	for i := 0; i < 5; i++ {
		if _, err := s.db.AppendMessage(store.Message{ID: newID(), ConversationID: c.ID, SenderType: "user", Content: strings.Repeat("历史讨论", 1000)}); err != nil {
			t.Fatal(err)
		}
	}
	s.newChatModel = func(context.Context, string, string, string) (model.ToolCallingChatModel, error) {
		return &collaborationModel{next: func(ctx context.Context, in []*schema.Message, _ map[string]bool) (*schema.Message, error) {
			if !strings.Contains(in[0].Content, "把历史资料压缩") {
				return nil, fmt.Errorf("expected compaction")
			}
			<-ctx.Done()
			return nil, ctx.Err()
		}}, nil
	}
	events := make(chan store.ConversationRun, 100)
	s.StartScheduler(func(r store.ConversationRun) { events <- r })
	d, err := s.Schedule(store.ScheduleRequest{RequestID: newID(), ConversationID: c.ID, Action: "mention", AgentIDs: []string{members[0].ID}, Content: "继续讨论"})
	if err != nil {
		t.Fatal(err)
	}
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	for {
		select {
		case r := <-events:
			if r.Status == "failed" {
				t.Fatal(r.Error)
			}
			if !r.ContextCompacting {
				continue
			}
			detail, err := s.Conversation(c.ID)
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, run := range detail.Runs {
				if run.ID == r.ID {
					found = run.ContextCompacting
				}
			}
			if !found {
				t.Fatal("conversation refresh lost live compaction activity")
			}
			if err = s.StopRun(r.ID); err != nil {
				t.Fatal(err)
			}
			stopped := waitRun(t, events, d.Runs[0].ID, "cancelled")
			if stopped.ContextCompacting {
				t.Fatal("cancelled run still shows compaction")
			}
			return
		case <-timer.C:
			t.Fatal("no compression status received")
		}
	}
}

func TestSchedulerCompactionPersistsAndFailureLeavesUnread(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprint(fail), func(t *testing.T) {
			s, c, members := groupService(t)
			for i := 0; i < 110; i++ {
				text := fmt.Sprintf("消息%d：", i) + strings.Repeat("讨论活动的安排。", 60)
				if i == 0 {
					text = "场地A因噪音被拒绝。预算原来800元。" + text
				}
				if _, err := s.db.AppendMessage(store.Message{ID: newID(), ConversationID: c.ID, SenderType: "user", Content: text}); err != nil {
					t.Fatal(err)
				}
			}
			summaries := 0
			s.newChatModel = func(context.Context, string, string, string) (model.ToolCallingChatModel, error) {
				return &collaborationModel{next: func(_ context.Context, in []*schema.Message, _ map[string]bool) (*schema.Message, error) {
					if strings.Contains(in[0].Content, "把历史资料压缩") {
						summaries++
						if fail {
							return nil, fmt.Errorf("summary connection failed")
						}
						return schema.AssistantMessage("场地A因为噪音被否决。原预算800，最新要求优先。", nil), nil
					}
					if !strings.Contains(in[len(in)-1].Content, "200元") {
						t.Fatal("latest requirement omitted")
					}
					return schema.AssistantMessage("按200元继续，场地A因噪音被否决。", nil), nil
				}}, nil
			}
			events := make(chan store.ConversationRun, 100)
			s.StartScheduler(func(r store.ConversationRun) { events <- r })
			d, err := s.Schedule(store.ScheduleRequest{RequestID: newID(), ConversationID: c.ID, Action: "mention", AgentIDs: []string{members[0].ID}, Content: "预算改为200元，继续完善"})
			if err != nil {
				t.Fatal(err)
			}
			status := "completed"
			if fail {
				status = "failed"
			}
			waitRun(t, events, d.Runs[0].ID, status)
			if summaries == 0 {
				t.Fatal("budget did not trigger compression")
			}
			history, err := s.db.ModelHistory(members[0].ID, c.ID)
			if err != nil {
				t.Fatal(err)
			}
			if fail {
				if len(history) != 0 {
					t.Fatal("failed summary committed context")
				}
				d, err = s.Schedule(store.ScheduleRequest{RequestID: newID(), ConversationID: c.ID, Action: "mention", AgentIDs: []string{members[0].ID}, Content: "重试200元要求"})
				if err != nil {
					t.Fatal(err)
				}
				// The repeated attempt must still summarize the original backlog.
				waitRun(t, events, d.Runs[0].ID, "failed")
				if summaries != 2 {
					t.Fatal("failed attempt lost unread backlog", summaries)
				}
				return
			}
			if !strings.Contains(promptText(history), "历史摘要") || len(history) >= 112 {
				t.Fatal("compacted state was not stored")
			}
			// System/task instructions must be freshly loaded on the next run.
			for _, msg := range history {
				if msg.Role == schema.System {
					t.Fatal("persisted stale system state")
				}
			}
		})
	}
}
