package store

import (
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"testing"
)

func TestStopDeliveryRaceAndLateCompletion(t *testing.T) {
	for i := 0; i < 12; i++ {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			s, _, dir := groupFixture(t)
			d := scheduleLegacyLeadTest(t, s, "race")
			r, _ := claimTest(t, s, d.Runs[0])
			r.Text = "停止时保留的部分内容"
			r.Revision += 8
			start := make(chan struct{})
			var wg sync.WaitGroup
			var sent Delivery
			var sendErr, stopErr error
			wg.Add(2)
			go func() {
				defer wg.Done()
				<-start
				sent, sendErr = s.Deliver(r.ID, "race-call", SendInput{TargetAgentID: "b", Content: "check"}, "sent", "receiver")
			}()
			go func() { defer wg.Done(); <-start; _, stopErr = s.StopChain(r.ChainID, &r) }()
			close(start)
			wg.Wait()
			if stopErr != nil {
				t.Fatal(stopErr)
			}
			saved, err := s.ConversationRun(r.ID)
			if err != nil || saved.Status != "cancelled" || saved.Text != r.Text || saved.Revision <= r.Revision {
				t.Fatal(saved, err)
			}
			if sendErr == nil {
				child, _ := s.ConversationRun(sent.Runs[0].ID)
				if child.Status != "cancelled" {
					t.Fatal("committed child survived stop", child)
				}
			}
			if _, err = s.Deliver(r.ID, "new-call", SendInput{TargetAgentID: "b", Content: "late"}, "late", "late-run"); err == nil {
				t.Fatal("late delivery accepted")
			}
			// A provider that returns after cancel must not publish or overwrite anything.
			finishTest(t, s, r, "completed", "late success")
			messages, _ := s.Messages("group")
			want := 1
			if sendErr == nil {
				want++
			}
			if len(messages) != want {
				t.Fatal("late reply published", messages)
			}
			s.Close()
			s, err = Open(dir)
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			chains, _ := s.Chains("group")
			if chains[0].Status != "stopped" {
				t.Fatal(chains)
			}
			if _, err = s.QueuedRun(); err != sql.ErrNoRows {
				t.Fatal("stopped work resumed", err)
			}
		})
	}
}

func TestRetryPreservesTriggerBudgetAndSurvivesRestart(t *testing.T) {
	s, _, dir := groupFixture(t)
	d := scheduleLegacyLeadTest(t, s, "first")
	original, _ := claimTest(t, s, d.Runs[0])
	sent, err := s.Deliver(original.ID, "sent", SendInput{TargetAgentID: "b", Content: "old committed task"}, "sent-m", "sent-r")
	if err != nil {
		t.Fatal(err)
	}
	finishTest(t, s, original, "failed", "failed partial")
	retry, err := s.RetryRun(original.ID, "retry-request", "retry-run")
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"retry-request", "different-request"} {
		dup, err := s.RetryRun(original.ID, key, "must-not-create")
		if err != nil || dup.ID != retry.ID {
			t.Fatal("duplicate retry", dup, err)
		}
	}
	if _, err = s.RetryRun(sent.Runs[0].ID, "retry-request", "wrong"); err == nil {
		t.Fatal("request reused for different origin")
	}
	if retry.MessageID != original.MessageID || retry.RetryOf != original.ID || retry.ChainID != original.ChainID {
		t.Fatal("retry lost linkage", retry)
	}
	before, _ := s.Messages("group")
	if len(before) != 2 {
		t.Fatal("retry duplicated public message", before)
	}
	s.Close()
	s, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	next, err := s.QueuedRun()
	if err != nil || next.ID != retry.ID {
		t.Fatal("historical failure cancelled queued retry", next, err)
	}
	old, _ := s.ConversationRun(original.ID)
	child, _ := s.ConversationRun(sent.Runs[0].ID)
	if old.Status != "failed" || child.Status != "cancelled" {
		t.Fatal("retry rewrote old records", old, child)
	}
	current := retry
	for reserved := 3; reserved <= 6; reserved++ {
		active, _ := claimTest(t, s, current)
		finishTest(t, s, active, "failed", "another failure")
		newer, err := s.RetryRun(current.ID, fmt.Sprintf("request-%d", reserved), fmt.Sprintf("run-%d", reserved))
		if reserved == 6 {
			if err == nil || !strings.Contains(err.Error(), "6 次") {
				t.Fatal("retry exceeded budget", err)
			}
		} else {
			if err != nil {
				t.Fatal(err)
			}
			current = newer
		}
	}
	chains, _ := s.Chains("group")
	runs, _ := s.ConversationRuns("group")
	if chains[0].Reserved != 6 || len(runs) != 6 {
		t.Fatal(chains, len(runs))
	}
}

func TestRetryRejectsStoppedDiscussionAndChangedConfiguration(t *testing.T) {
	for _, mode := range []string{"stopped", "round", "changed"} {
		t.Run(mode, func(t *testing.T) {
			s, c, _ := groupFixture(t)
			action := "lead"
			ids := []string{}
			if mode == "round" {
				action = "round"
				ids = []string{"a", "b"}
			}
			d := scheduleTest(t, s, "first", action, ids...)
			r, _ := claimTest(t, s, d.Runs[0])
			finishTest(t, s, r, "failed", "")
			if mode == "stopped" {
				if _, err := s.StopChain(r.ChainID, nil); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "changed" {
				c.Title = "changed"
				if _, err := s.SaveConversation(c, false); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := s.RetryRun(r.ID, "retry", "new"); err == nil {
				t.Fatal("invalid retry accepted")
			}
			runs, _ := s.ConversationRuns("group")
			if len(runs) != len(d.Runs) {
				t.Fatal("invalid retry side effects")
			}
		})
	}
}

func TestStopAndDisableAreAtomicAndPreserveOtherWork(t *testing.T) {
	s, _, _ := groupFixture(t)
	d := scheduleTest(t, s, "first", "round", "a", "b", "c")
	r, _ := claimTest(t, s, d.Runs[0])
	r.Text = "partial"
	r.Revision++
	unrelated, err := s.Enqueue("other", Message{ID: "other-message", ConversationID: "two", Content: "untouched"}, "other-run")
	if err != nil {
		t.Fatal(err)
	}
	s.db.Exec(`CREATE TRIGGER reject_cancel BEFORE UPDATE OF status ON runs WHEN NEW.status='cancelled' BEGIN SELECT RAISE(ABORT,'disk error'); END`)
	if _, _, err = s.SetAgentEnabledAndCancel("b", false, &r); err == nil {
		t.Fatal("expected disk error")
	}
	b, _ := s.Agent("b")
	chains, _ := s.Chains("group")
	if !b.Enabled || chains[0].Status != "active" {
		t.Fatal("disable partially committed")
	}
	s.db.Exec(`DROP TRIGGER reject_cancel`)
	_, changed, err := s.SetAgentEnabledAndCancel("b", false, &r)
	if err != nil || len(changed) != 3 {
		t.Fatal(changed, err)
	}
	active, _ := s.ConversationRun(r.ID)
	if active.Text != "partial" || active.Status != "cancelled" {
		t.Fatal(active)
	}
	if _, err = s.SetAgentEnabled("b", true); err != nil {
		t.Fatal(err)
	}
	next, err := s.QueuedRun()
	if err != nil || next.ID != unrelated.ID {
		t.Fatal("unrelated queue changed or disabled work replayed", next, err)
	}
}
