package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
)

func groupFixture(t *testing.T) (*Store, Conversation, string) {
	s, _, dir := queueFixture(t)
	addAgent(t, s, "b")
	addAgent(t, s, "c")
	c, err := s.SaveConversation(Conversation{ID: "group", Title: "group", Kind: "group", Mode: "lead", LeadAgentID: "a", MemberIDs: []string{"a", "b", "c"}}, true)
	if err != nil {
		t.Fatal(err)
	}
	return s, c, dir
}
func scheduleTest(t *testing.T, s *Store, key, action string, ids ...string) Delivery {
	t.Helper()
	runs := []string{}
	for i := 0; i < 6; i++ {
		runs = append(runs, fmt.Sprintf("%s-r%d", key, i))
	}
	d, err := s.Schedule(ScheduleRequest{RequestID: key, ConversationID: "group", Content: key, Action: action, AgentIDs: ids}, key+"-m", key+"-ch", runs)
	if err != nil {
		t.Fatal(err)
	}
	return d
}
func claimTest(t *testing.T, s *Store, r ConversationRun) (ConversationRun, string) {
	t.Helper()
	a, err := s.Agent(r.AgentID)
	if err != nil {
		t.Fatal(err)
	}
	r.Status = "running"
	r.StartedAt = timestamp()
	r.Revision++
	in, err := s.ClaimConversationRun(r, a)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(in)
	return r, string(data)
}
func finishTest(t *testing.T, s *Store, r ConversationRun, status, text string) {
	t.Helper()
	r.Status = status
	r.Text = text
	r.FinishedAt = timestamp()
	r.Revision++
	if err := s.FinishConversationRun(r, nil, r.ID+"-reply"); err != nil {
		t.Fatal(err)
	}
}

func TestGroupRoundAtomicityIdempotencyAndPublicContext(t *testing.T) {
	s, c, _ := groupFixture(t)
	published := scheduleTest(t, s, "publish", "publish")
	if len(published.Runs) != 0 {
		t.Fatal("ordinary public message triggered a run")
	}
	for _, ids := range [][]string{{"a", "b", "missing"}, {"a", "a"}, {"a", "b", "c", "d", "e", "f", "g"}} {
		if _, err := s.Schedule(ScheduleRequest{RequestID: "invalid", ConversationID: c.ID, Content: "bad", Action: "round", AgentIDs: ids}, "bad-m", "bad-c", nil); err == nil {
			t.Fatal("invalid selection accepted")
		}
	}
	messages, _ := s.Messages(c.ID)
	if len(messages) != 1 {
		t.Fatal("failed schedule leaked messages")
	}
	s.db.Exec(`CREATE TRIGGER reject_second BEFORE INSERT ON runs WHEN NEW.agent_id='b' BEGIN SELECT RAISE(ABORT,'disk failure'); END`)
	if _, err := s.Schedule(ScheduleRequest{RequestID: "disk", ConversationID: c.ID, Content: "bad", Action: "round", AgentIDs: []string{"a", "b"}}, "bad-m", "bad-c", []string{"bad-a", "bad-b"}); err == nil {
		t.Fatal("expected disk error")
	}
	s.db.Exec(`DROP TRIGGER reject_second`)
	messages, _ = s.Messages(c.ID)
	chains, _ := s.Chains(c.ID)
	if len(messages) != 1 || len(chains) != 0 {
		t.Fatal("partial round survived rollback")
	}
	d := scheduleTest(t, s, "round", "round", "b", "a", "c")
	dup := scheduleTest(t, s, "round", "round", "b", "a", "c")
	if len(dup.Runs) != 3 || dup.Runs[0].ID != d.Runs[0].ID {
		t.Fatal("duplicate round")
	}
	if _, err := s.SaveConversation(c, false); err == nil {
		t.Fatal("in-flight mode edit accepted")
	}
	if _, err := s.Schedule(ScheduleRequest{RequestID: "round", ConversationID: c.ID, Content: "round", Action: "round", AgentIDs: []string{"a", "b", "c"}}, "other", "other", nil); err == nil {
		t.Fatal("reused request changed ordering")
	}
	for i, r := range d.Runs {
		allowed, _ := s.CanCollaborate(r.ID)
		if allowed {
			t.Fatal("round exposed collaboration")
		}
		next, err := s.QueuedRun()
		if err != nil || next.ID != r.ID {
			t.Fatal("wrong dependency ordering", next, err)
		}
		if i == 0 {
			a, _ := s.Agent("a")
			if _, err = s.ClaimConversationRun(d.Runs[1], a); err == nil {
				t.Fatal("claimed successor before predecessor")
			}
		}
		active, input := claimTest(t, s, r)
		if i > 0 && !strings.Contains(input, fmt.Sprintf("public-%d", i-1)) {
			t.Fatal("next speaker missed prior public reply", input)
		}
		if strings.Contains(input, "private-trace") {
			t.Fatal("private trace shared")
		}
		s.db.Exec(`UPDATE runs SET transcript='[{"role":"assistant","content":"private-trace"}]' WHERE id=?`, r.ID)
		finishTest(t, s, active, "completed", fmt.Sprintf("public-%d", i))
	}
	if _, err := s.QueuedRun(); err != sql.ErrNoRows {
		t.Fatal("public reply caused more work", err)
	}
	chains, _ = s.Chains(c.ID)
	if len(chains) != 1 || chains[0].Reserved != 3 || chains[0].Status != "completed" {
		t.Fatal(chains)
	}
	c.Mode = "discussion"
	c.LeadAgentID = ""
	if _, err := s.SaveConversation(c, false); err != nil {
		t.Fatal(err)
	}
	summary := scheduleTest(t, s, "summary", "summary", "a")
	if len(summary.Runs) != 1 {
		t.Fatal("summary must run once")
	}
	active, input := claimTest(t, s, summary.Runs[0])
	if !strings.Contains(input, "总结") || !strings.Contains(input, "public-2") {
		t.Fatal(input)
	}
	finishTest(t, s, active, "completed", "summary")
	chains, _ = s.Chains(c.ID)
	if chains[0].Mode != "lead" || chains[0].LeadAgentID != "a" {
		t.Fatal("mode edit changed old chain snapshot")
	}
}

func TestLegacyToolDeliveryBindsIdentityValidatesAndReservesConcurrently(t *testing.T) {
	s, _, _ := groupFixture(t)
	addAgent(t, s, "outsider")
	d := scheduleLegacyLeadTest(t, s, "lead")
	r, _ := claimTest(t, s, d.Runs[0])
	external, err := s.AppendMessage(Message{ID: "external", ConversationID: "two", SenderType: "user", Content: "private"})
	if err != nil {
		t.Fatal(err)
	}
	for i, in := range []SendInput{{TargetAgentID: "a", Content: "self"}, {TargetAgentID: "missing", Content: "bad"}, {TargetAgentID: "outsider", Content: "bad"}, {TargetAgentID: "b", Content: "bad", ReplyToMessageID: external.ID}} {
		if _, err = s.Deliver(r.ID, fmt.Sprint(i), in, "invalid-m", "invalid-r"); err == nil {
			t.Fatal("invalid delivery accepted")
		}
	}
	s.SetAgentEnabled("b", false)
	if _, err = s.Deliver(r.ID, "disabled", SendInput{TargetAgentID: "b", Content: "bad"}, "invalid-m", "invalid-r"); err == nil {
		t.Fatal("disabled target accepted")
	}
	s.SetAgentEnabled("b", true)
	in := SendInput{TargetAgentID: "b", Content: "review", ReplyToMessageID: r.MessageID}
	sent, err := s.Deliver(r.ID, "call", in, "sent-m", "sent-r")
	if err != nil {
		t.Fatal(err)
	}
	dup, err := s.Deliver(r.ID, "call", in, "dup-m", "dup-r")
	if err != nil || dup.Runs[0].ID != sent.Runs[0].ID {
		t.Fatal("tool duplicate", err)
	}
	messages, _ := s.Messages("group")
	if len(messages) != 2 || messages[1].SenderID != "a" || messages[1].TargetAgentID != "b" || messages[1].SourceRunID != r.ID {
		t.Fatal("unbound sender", messages)
	}
	if _, err = s.QueuedRun(); err != sql.ErrNoRows {
		t.Fatal("receiver ran before sender finished")
	}
	var wg sync.WaitGroup
	results := make(chan error, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := s.Deliver(r.ID, fmt.Sprintf("parallel-%d", i), in, fmt.Sprintf("m-%d", i), fmt.Sprintf("r-%d", i))
			results <- err
		}(i)
	}
	wg.Wait()
	close(results)
	success := 0
	for err := range results {
		if err == nil {
			success++
		} else if !strings.Contains(err.Error(), "上限") {
			t.Fatal(err)
		}
	}
	if success != 3 {
		t.Fatal("budget race", success)
	}
	chains, _ := s.Chains("group")
	runs, _ := s.ConversationRuns("group")
	if len(runs) != 5 || chains[0].Reserved != 5 || !strings.Contains(chains[0].Reason, "上限") {
		t.Fatal("budget exceeded", chains, len(runs))
	}
	finishTest(t, s, r, "completed", "已邀请评审")
	dup, err = s.Deliver(r.ID, "call", in, "dup-m", "dup-r")
	if err != nil || dup.MessageID != sent.MessageID {
		t.Fatal("replay after sender completed", err)
	}
	next, err := s.QueuedRun()
	if err != nil || next.ID != "sent-r" {
		t.Fatal(next, err)
	}
}

func TestRoundFailureRecoveryAndCursor(t *testing.T) {
	for _, mode := range []string{"failed", "restart"} {
		t.Run(mode, func(t *testing.T) {
			s, _, dir := groupFixture(t)
			d := scheduleTest(t, s, "round", "round", "a", "b", "c")
			r, input := claimTest(t, s, d.Runs[0])
			if !strings.Contains(input, "round") {
				t.Fatal(input)
			}
			s.AppendMessage(Message{ID: "late", ConversationID: "group", SenderType: "user", Content: "late-public"})
			var persisted string
			s.db.QueryRow(`SELECT input_messages FROM runs WHERE id=?`, r.ID).Scan(&persisted)
			if strings.Contains(persisted, "late-public") {
				t.Fatal("input not frozen")
			}
			if mode == "restart" {
				s.Close()
				var err error
				s, err = Open(dir)
				if err != nil {
					t.Fatal(err)
				}
				defer s.Close()
			} else {
				finishTest(t, s, r, "failed", "")
			}
			runs, _ := s.ConversationRuns("group")
			if runs[1].Status != "cancelled" || runs[2].Status != "cancelled" {
				t.Fatal("descendants survived failure", runs)
			}
			if _, err := s.QueuedRun(); err != sql.ErrNoRows {
				t.Fatal("round still running", err)
			}
			retry := scheduleTest(t, s, "mention", "mention", "a")
			active, next := claimTest(t, s, retry.Runs[0])
			if !strings.Contains(next, "late-public") || !strings.Contains(next, "round-m") {
				t.Fatal("failed input lost", next)
			}
			finishTest(t, s, active, "completed", "done")
			var cursor int
			s.db.QueryRow(`SELECT sequence FROM member_cursors WHERE conversation_id='group' AND agent_id='a'`).Scan(&cursor)
			if mode == "restart" {
				s.Close()
				s, _ = Open(dir)
				defer s.Close()
				var after int
				s.db.QueryRow(`SELECT sequence FROM member_cursors WHERE conversation_id='group' AND agent_id='a'`).Scan(&after)
				if cursor != after {
					t.Fatal("old interruption reset cursor again")
				}
			}
		})
	}
}

func TestCancellingUnclaimedRunPreservesReadCursor(t *testing.T) {
	s, _, _ := groupFixture(t)
	first := scheduleTest(t, s, "first", "mention", "a")
	active, _ := claimTest(t, s, first.Runs[0])
	finishTest(t, s, active, "completed", "done")
	var before int
	s.db.QueryRow(`SELECT sequence FROM member_cursors WHERE conversation_id='group' AND agent_id='a'`).Scan(&before)
	queued := scheduleTest(t, s, "queued", "mention", "a")
	finishTest(t, s, queued.Runs[0], "cancelled", "")
	var after int
	s.db.QueryRow(`SELECT sequence FROM member_cursors WHERE conversation_id='group' AND agent_id='a'`).Scan(&after)
	if before == 0 || after != before {
		t.Fatal("unclaimed run reset existing cursor", before, after)
	}
}
