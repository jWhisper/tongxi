package store

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/cloudwego/eino/schema"
)

func queueFixture(t *testing.T) (*Store, Agent, string) {
	t.Helper()
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	a := addAgent(t, s, "a")
	for _, id := range []string{"one", "two"} {
		if _, err = s.SaveConversation(Conversation{ID: id, Title: id, Kind: "private", Mode: "lead", LeadAgentID: a.ID, MemberIDs: []string{a.ID}}, true); err != nil {
			t.Fatal(err)
		}
	}
	return s, a, dir
}

func TestQueueTransactionsIdempotencyAndRecovery(t *testing.T) {
	s, a, dir := queueFixture(t)
	_, err := s.db.Exec(`CREATE TRIGGER reject_run BEFORE INSERT ON runs BEGIN SELECT RAISE(ABORT,'disk failure'); END`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Enqueue("request", Message{ID: "m", ConversationID: "one", Content: "hello"}, "r"); err == nil {
		t.Fatal("expected failure")
	}
	messages, _ := s.Messages("one")
	if len(messages) != 0 {
		t.Fatal("message survived failed enqueue")
	}
	s.db.Exec(`DROP TRIGGER reject_run`)
	r, err := s.Enqueue("request", Message{ID: "m", ConversationID: "one", Content: "hello"}, "r")
	if err != nil {
		t.Fatal(err)
	}
	duplicate, err := s.Enqueue("request", Message{ID: "m-other", ConversationID: "one", Content: "hello"}, "r-other")
	if err != nil || duplicate.ID != r.ID {
		t.Fatalf("duplicate: %+v %v", duplicate, err)
	}
	if _, err = s.Enqueue("request", Message{ID: "m-other", ConversationID: "two", Content: "hello"}, "r-other"); err == nil {
		t.Fatal("request reused for another conversation")
	}
	if err = s.CreateConversationRun(ConversationRun{ID: "additional", ConversationID: "one", AgentID: a.ID, MessageID: r.MessageID}); err != nil {
		t.Fatal("schema rejects multiple runs per message:", err)
	}
	r.Status, r.Revision, r.StartedAt = "running", 2, timestamp()
	if err = s.StartConversationRun(r, a); err != nil {
		t.Fatal(err)
	}
	var snapshot string
	s.db.QueryRow(`SELECT config FROM runs WHERE id=?`, r.ID).Scan(&snapshot)
	if !strings.Contains(snapshot, a.Instruction) || strings.Contains(snapshot, a.KeyRef) {
		t.Fatal("invalid/private snapshot:", snapshot)
	}
	_, err = s.db.Exec(`CREATE TRIGGER reject_reply BEFORE INSERT ON messages WHEN NEW.sender_type='agent' BEGIN SELECT RAISE(ABORT,'disk failure'); END`)
	if err != nil {
		t.Fatal(err)
	}
	r.Status, r.Text, r.FinishedAt, r.Revision = "completed", "reply", timestamp(), 3
	if err = s.FinishConversationRun(r, []*schema.Message{schema.UserMessage("hello"), schema.AssistantMessage("reply", nil)}, "reply"); err == nil {
		t.Fatal("expected reply failure")
	}
	saved, _ := s.ConversationRun(r.ID)
	if saved.Status != "running" {
		t.Fatal("completion survived failed reply")
	}
	s.db.Exec(`DROP TRIGGER reject_reply`)
	s.Close()
	reopened, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	saved, _ = reopened.ConversationRun(r.ID)
	if saved.Status != "interrupted" {
		t.Fatal("running replayed after restart")
	}
	queued, err := reopened.QueuedRun()
	if err != nil || queued.ID != "additional" {
		t.Fatal("queue not retained", queued, err)
	}
	// A stale completion cannot publish after interruption.
	if err = reopened.FinishConversationRun(r, nil, "late"); err != nil {
		t.Fatal(err)
	}
	messages, _ = reopened.Messages("one")
	if len(messages) != 1 {
		t.Fatal("late reply published")
	}
}

func TestHistoryKeepsAllToolPairsAndIsolation(t *testing.T) {
	s, a, _ := queueFixture(t)
	for i := 0; i < 14; i++ {
		id := fmt.Sprint(i)
		r, err := s.Enqueue(id, Message{ID: "m" + id, ConversationID: "one", Content: id}, "r"+id)
		if err != nil {
			t.Fatal(err)
		}
		call := schema.AssistantMessage("", []schema.ToolCall{{ID: "call-" + id, Type: "function", Function: schema.FunctionCall{Name: "count_characters", Arguments: `{"text":"同席"}`}}})
		call.ReasoningContent = "reasoning " + id
		turn := []*schema.Message{schema.UserMessage(id), call, schema.ToolMessage(`{"count":2}`, "call-"+id), schema.AssistantMessage("2", nil)}
		r.Status, r.Text, r.FinishedAt, r.Revision = "completed", "2", timestamp(), 2
		if err = s.FinishConversationRun(r, turn, "reply"+id); err != nil {
			t.Fatal(err)
		}
	}
	history, err := s.ModelHistory(a.ID, "one")
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 56 || history[0].Content != "0" {
		t.Fatal("history omitted completed turns", len(history))
	}
	for i := 0; i < len(history); i += 4 {
		if history[i+1].ReasoningContent == "" || history[i+1].ToolCalls[0].ID != history[i+2].ToolCallID {
			t.Fatal("tool pair or reasoning lost")
		}
	}
	for _, ids := range [][2]string{{a.ID, "two"}, {"other", "one"}} {
		history, err = s.ModelHistory(ids[0], ids[1])
		if err != nil || len(history) != 0 {
			t.Fatal("history crossed agent/conversation", err)
		}
	}
	r, _ := s.Enqueue("oversized", Message{ID: "huge", ConversationID: "one", Content: "big"}, "huge-run")
	r.Status, r.Text, r.FinishedAt = "completed", "big", timestamp()
	if err = s.FinishConversationRun(r, []*schema.Message{schema.UserMessage("big"), schema.AssistantMessage(strings.Repeat("长", 48001), nil)}, "huge-reply"); err != nil {
		t.Fatal(err)
	}
	history, err = s.ModelHistory(a.ID, "one")
	if err != nil || len(history) != 58 {
		t.Fatal("oversized turn was omitted before budget management")
	}
	var data string
	s.db.QueryRow(`SELECT transcript FROM runs WHERE id=?`, r.ID).Scan(&data)
	var full []*schema.Message
	if err = json.Unmarshal([]byte(data), &full); err != nil || len(full[1].Content) == 0 {
		t.Fatal("archived history was truncated")
	}
}
