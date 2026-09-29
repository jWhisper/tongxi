package store

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/cloudwego/eino/schema"
)

func TestUnreadCoverageHasNoWindowHolesAndCommitsOnlyOnSuccess(t *testing.T) {
	s, c, _ := groupFixture(t)
	for i := 1; i <= 120; i++ {
		text := fmt.Sprintf("原文-%03d", i)
		if i == 1 {
			text += "：场地A因噪音被拒绝，预算800元"
		}
		if i == 60 {
			text += "：预算改成200元"
		}
		if _, err := s.AppendMessage(Message{ID: fmt.Sprint(i), ConversationID: c.ID, SenderType: "user", Content: text}); err != nil {
			t.Fatal(err)
		}
	}
	d := scheduleTest(t, s, "later", "mention", "a")
	r, input := claimTest(t, s, d.Runs[0])
	for _, want := range []string{"原文-001", "原文-060", "原文-120", "200元"} {
		if !strings.Contains(input, want) {
			t.Fatal("uncovered message", want)
		}
	}
	var cursor int
	s.db.QueryRow(`SELECT COALESCE((SELECT sequence FROM member_cursors WHERE conversation_id=? AND agent_id='a'),0)`, c.ID).Scan(&cursor)
	if cursor != 0 {
		t.Fatal("claim prematurely consumed unread messages")
	}
	finishTest(t, s, r, "failed", "")
	d = scheduleTest(t, s, "retry-unread", "mention", "a")
	r, input = claimTest(t, s, d.Runs[0])
	if !strings.Contains(input, "原文-001") {
		t.Fatal("failed run lost unread history")
	}
	later, err := s.AppendMessage(Message{ID: "arrived-during-run", ConversationID: c.ID, SenderType: "user", Content: "执行期间的新要求"})
	if err != nil {
		t.Fatal(err)
	}
	finishTest(t, s, r, "completed", "已核对")
	s.db.QueryRow(`SELECT sequence FROM member_cursors WHERE conversation_id=? AND agent_id='a'`, c.ID).Scan(&cursor)
	if cursor != later.Sequence-1 {
		t.Fatal("cursor includes unseen arrivals", cursor, later.Sequence)
	}
}

func TestContextCheckpointRestartIsolationAndAtomicFailure(t *testing.T) {
	s, c, dir := groupFixture(t)
	d := scheduleTest(t, s, "checkpoint", "mention", "a")
	r, _ := claimTest(t, s, d.Runs[0])
	r.Status = "completed"
	r.FinishedAt = timestamp()
	r.Text = "预算200"
	state := &ContextState{Messages: []*schema.Message{schema.UserMessage("历史摘要：预算从800改为200；场地A因噪音被拒绝")}}
	original := []*schema.Message{schema.UserMessage("完整原文只保存在执行记录")}
	if _, err := s.db.Exec(`CREATE TRIGGER fail_context BEFORE INSERT ON context_states BEGIN SELECT RAISE(ABORT,'disk failure'); END`); err != nil {
		t.Fatal(err)
	}
	if err := s.FinishConversationRun(r, original, "reply", state); err == nil {
		t.Fatal("expected save failure")
	}
	var cursor int
	s.db.QueryRow(`SELECT count(*) FROM member_cursors`).Scan(&cursor)
	if cursor != 0 {
		t.Fatal("cursor committed without context")
	}
	s.db.Exec(`DROP TRIGGER fail_context`)
	if err := s.FinishConversationRun(r, original, "reply", state); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	history, err := s.ModelHistory("a", c.ID)
	if err != nil || len(history) != 1 || !strings.Contains(history[0].Content, "200") {
		t.Fatal("checkpoint was not reused", err, history)
	}
	var raw string
	s.db.QueryRow(`SELECT transcript FROM runs WHERE id=?`, r.ID).Scan(&raw)
	if !strings.Contains(raw, "完整原文") {
		t.Fatal("original transcript lost")
	}
	for _, scope := range [][3]string{{"b", c.ID, "reply"}, {"a", "two", "reply"}, {"a", c.ID, "selector"}} {
		got, e := s.ContextHistory(scope[0], scope[1], scope[2])
		if e != nil || len(got) != 0 {
			t.Fatal("private context leaked", scope, e)
		}
	}
	// A later successful turn without a saved checkpoint is appended exactly once.
	d = scheduleTest(t, s, "after-restart", "mention", "a")
	r, _ = claimTest(t, s, d.Runs[0])
	r.Status = "completed"
	r.Text = "后来一轮"
	if err = s.FinishConversationRun(r, []*schema.Message{schema.UserMessage("后来一轮")}, "later-reply"); err != nil {
		t.Fatal(err)
	}
	history, err = s.ModelHistory("a", c.ID)
	if err != nil || len(history) != 2 || history[1].Content != "后来一轮" {
		t.Fatal("uncovered turn missing", err)
	}
}

func TestChatHistorySearchReadPagingAndScope(t *testing.T) {
	s, _, _ := queueFixture(t)
	for i := 0; i < 23; i++ {
		_, err := s.AppendMessage(Message{ID: fmt.Sprint(i), ConversationID: "one", SenderType: "user", SenderName: "你", Content: fmt.Sprintf("场地-%d：噪音大，预算200。", i)})
		if err != nil {
			t.Fatal(err)
		}
	}
	long := strings.Repeat("甲", 9000) + "100%_匹配"
	m, err := s.AppendMessage(Message{ID: "long", ConversationID: "one", SenderType: "user", Content: long})
	if err != nil {
		t.Fatal(err)
	}
	hits, err := s.SearchChatHistory("one", "场地", 0)
	if err != nil || len(hits) != 20 {
		t.Fatal(hits, err)
	}
	page, err := s.SearchChatHistory("one", "场地", hits[len(hits)-1].Sequence)
	if err != nil || len(page) != 3 {
		t.Fatal("search pagination", err)
	}
	hits, err = s.SearchChatHistory("one", "100%_", 0)
	if err != nil || len(hits) != 1 || !strings.Contains(hits[0].Content, "100%_") {
		t.Fatal("literal keyword matching", hits, err)
	}
	parts := ""
	offset := 0
	for {
		got, e := s.ReadChatHistory("one", m.ID, 0, 0, offset)
		if e != nil || len(got) != 1 {
			t.Fatal(e)
		}
		parts += got[0].Content
		if got[0].Next == 0 {
			break
		}
		offset = got[0].Next
	}
	if parts != long {
		t.Fatal("long message pagination lost text")
	}
	if _, err = s.ReadChatHistory("two", m.ID, 0, 3, 0); err == nil {
		t.Fatal("read crossed conversation")
	}
	if err = s.SaveContextToolResult("one", "a", "record", "私有工具结果"); err != nil {
		t.Fatal(err)
	}
	if _, _, err = s.ContextToolResult("two", "a", "record", 0); err == nil {
		t.Fatal("tool result crossed conversation")
	}
	if _, _, err = s.ContextToolResult("one", "other", "record", 0); err == nil {
		t.Fatal("tool result crossed role")
	}
	hits, _ = s.SearchChatHistory("one", "私有工具", 0)
	if len(hits) != 0 {
		data, _ := json.Marshal(hits)
		t.Fatal("search exposed private tool records", string(data))
	}
}
