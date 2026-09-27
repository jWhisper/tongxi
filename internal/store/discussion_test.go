package store

import (
	"database/sql"
	"fmt"
	"testing"
)

func discussionFixture(t *testing.T) (*Store, Conversation, string) {
	t.Helper()
	s, c, dir := groupFixture(t)
	c.Mode, c.LeadAgentID = "discussion", ""
	if _, err := s.SaveConversation(c, false); err != nil {
		t.Fatal(err)
	}
	return s, c, dir
}

func TestDiscussionChoicePassAndPause(t *testing.T) {
	s, c, _ := discussionFixture(t)
	d := scheduleTest(t, s, "free", "discussion")
	if len(d.Runs) != 1 || d.Runs[0].Kind != "selector" {
		t.Fatal(d)
	}
	selector, _ := claimTest(t, s, d.Runs[0])
	var cursors int
	s.db.QueryRow(`SELECT count(*) FROM member_cursors`).Scan(&cursors)
	if cursors != 0 {
		t.Fatal("selector consumed member context")
	}
	if _, err := s.SelectDiscussionSpeaker(selector.ID, "outsider", "bad"); err == nil {
		t.Fatal("invalid speaker accepted")
	}
	b, err := s.SelectDiscussionSpeaker(selector.ID, "b", "b-run")
	if err != nil {
		t.Fatal(err)
	}
	dup, err := s.SelectDiscussionSpeaker(selector.ID, "b", "duplicate")
	if err != nil || dup.ID != b.ID {
		t.Fatal("duplicate choice", dup, err)
	}
	if _, err = s.SelectDiscussionSpeaker(selector.ID, "c", "c-run"); err == nil {
		t.Fatal("selector selected twice")
	}
	if _, err = s.QueuedRun(); err != sql.ErrNoRows {
		t.Fatal("speaker did not wait for selector")
	}
	finishTest(t, s, selector, "completed", "private routing text")
	b, _ = claimTest(t, s, b)
	if allowed, err := s.PassDiscussion(b.ID); err != nil || !allowed {
		t.Fatal("could not pass", allowed, err)
	}
	if _, err = s.Deliver(b.ID, "after-pass", SendInput{TargetAgentID: "c", Content: "late"}, "late-m", "late-r"); err == nil {
		t.Fatal("silent member scheduled more work")
	}
	b.Silent = true
	finishTest(t, s, b, "completed", "")
	selector, err = s.QueuedRun()
	if err != nil || selector.Kind != "selector" {
		t.Fatal(selector, err)
	}
	members, _ := s.DiscussionCandidates(selector.ID)
	for _, m := range members {
		if m.ID == "b" {
			t.Fatal("member asked twice after passing")
		}
	}
	selector, _ = claimTest(t, s, selector)
	if allowed, err := s.PassDiscussion(selector.ID); err != nil || !allowed {
		t.Fatal("could not pause", allowed, err)
	}
	if _, err = s.SelectDiscussionSpeaker(selector.ID, "c", "after-pause"); err == nil {
		t.Fatal("paused selector scheduled more work")
	}
	finishTest(t, s, selector, "completed", "pause")
	chains, _ := s.Chains(c.ID)
	messages, _ := s.Messages(c.ID)
	if len(messages) != 1 || chains[0].Status != "completed" || chains[0].Reserved != 1 {
		t.Fatal("routing/pass leaked publicly or did not pause", messages, chains)
	}
	history, _ := s.ModelHistory("b", c.ID)
	if len(history) != 0 {
		t.Fatal("silent turn entered model history")
	}
}

func TestDiscussionBudgetsAndCandidateTurnTaking(t *testing.T) {
	s, c, _ := discussionFixture(t)
	scheduleTest(t, s, "bounded", "discussion")
	for i := 0; i < ChainLimit; i++ {
		selector, err := s.QueuedRun()
		if err != nil || selector.Kind != "selector" {
			t.Fatal(selector, err)
		}
		selector, _ = claimTest(t, s, selector)
		id := []string{"b", "c"}[i%2]
		if i > 0 {
			if _, err = s.SelectDiscussionSpeaker(selector.ID, []string{"c", "b"}[i%2], "repeat"); err == nil {
				t.Fatal("immediately repeated speaker")
			}
		}
		speaker, err := s.SelectDiscussionSpeaker(selector.ID, id, fmt.Sprint("speaker-", i))
		if err != nil {
			t.Fatal(err)
		}
		finishTest(t, s, selector, "completed", "")
		speaker, _ = claimTest(t, s, speaker)
		finishTest(t, s, speaker, "completed", fmt.Sprint("观点-", i))
	}
	if _, err := s.QueuedRun(); err != sql.ErrNoRows {
		t.Fatal("discussion did not reach its bound", err)
	}
	chains, _ := s.Chains(c.ID)
	runs, _ := s.ConversationRuns(c.ID)
	messages, _ := s.Messages(c.ID)
	if chains[0].Reserved != 6 || chains[0].Status != "completed" || len(runs) != 12 || len(messages) != 7 {
		t.Fatal(chains, len(runs), len(messages))
	}
}

func TestDiscussionInterruptionIsAtomicAndOldChoiceCannotResume(t *testing.T) {
	s, c, dir := discussionFixture(t)
	old := scheduleTest(t, s, "old", "discussion")
	active, _ := claimTest(t, s, old.Runs[0])
	active.Text, active.Revision = "private routing", active.Revision+1
	in := ScheduleRequest{RequestID: "new", ConversationID: c.ID, Content: "new topic", Action: "discussion"}
	s.db.Exec(`CREATE TRIGGER fail_message BEFORE INSERT ON messages WHEN NEW.id='new-m' BEGIN SELECT RAISE(ABORT,'disk failure'); END`)
	if _, err := s.ScheduleWithActive(in, "new-m", "new-chain", []string{"new-r"}, &active); err == nil {
		t.Fatal("expected rollback")
	}
	chains, _ := s.Chains(c.ID)
	if chains[0].Status != "active" {
		t.Fatal("failed send stopped prior discussion")
	}
	s.db.Exec(`DROP TRIGGER fail_message`)
	newer, err := s.ScheduleWithActive(in, "new-m", "new-chain", []string{"new-r"}, &active)
	if err != nil || len(newer.Cancelled) != 1 {
		t.Fatal(newer, err)
	}
	if _, err = s.SelectDiscussionSpeaker(active.ID, "b", "late-r"); err == nil {
		t.Fatal("late selector restarted old topic")
	}
	finishTest(t, s, active, "completed", "late")
	dup, err := s.ScheduleWithActive(in, "dup-m", "dup-chain", []string{"dup-r"}, nil)
	if err != nil || dup.ChainID != newer.ChainID || len(dup.Cancelled) != 0 {
		t.Fatal(dup, err)
	}
	s.Close()
	s, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	next, err := s.QueuedRun()
	if err != nil || next.ID != newer.Runs[0].ID {
		t.Fatal("replacement lost on restart", next, err)
	}
	claimTest(t, s, next)
	s.Close()
	s, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	chains, _ = s.Chains(c.ID)
	if chains[0].Status != "stopped" || chains[1].Status != "failed" {
		t.Fatal("crashed selector replayed", chains)
	}
}
