package store

import (
	"database/sql"
	"errors"
	"testing"
	"time"
)

func TestCollaborationDeadlineStartsWithExecutionAndSurvivesRetryRestart(t *testing.T) {
	s, _, dir := groupFixture(t)
	d := scheduleTest(t, s, "deadline", "lead")
	if _, err := s.db.Exec(`UPDATE chains SET created_at='2000-01-01T00:00:00Z' WHERE id=?`, d.ChainID); err != nil {
		t.Fatal(err)
	}
	deadline, err := s.CollaborationDeadline(d.Runs[0].ID)
	if err != nil || !deadline.IsZero() {
		t.Fatal("queue waiting spent execution budget", deadline, err)
	}
	r, _ := claimTest(t, s, d.Runs[0])
	deadline, err = s.CollaborationDeadline(r.ID)
	started, _ := time.Parse(time.RFC3339Nano, r.StartedAt)
	if err != nil || !deadline.Equal(started.Add((DefaultCollaborationMinutes * time.Minute))) {
		t.Fatal(deadline, err)
	}
	finishTest(t, s, r, "failed", "partial")
	retry, err := s.RetryRun(r.ID, "retry-deadline", "retry-run")
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	after, err := s.CollaborationDeadline(retry.ID)
	if err != nil || !deadline.Equal(after) {
		t.Fatal("retry or restart reset deadline", deadline, after, err)
	}
	if _, err = s.db.Exec(`UPDATE runs SET started_at=? WHERE id=?`, time.Now().Add(-(DefaultCollaborationMinutes*time.Minute)-time.Second).UTC().Format(time.RFC3339Nano), r.ID); err != nil {
		t.Fatal(err)
	}
	finishTest(t, s, retry, "interrupted", "")
	c, _ := s.RunChain(retry.ID)
	if c.Status != "incomplete" || c.Reason != CollaborationTimeReason {
		t.Fatal("expired collaboration was not paused", c)
	}
	if _, err = s.RetryRun(retry.ID, "reset-budget", "bad-retry"); err == nil {
		t.Fatal("expired collaboration restarted through retry")
	}
	if _, err = s.QueuedRun(); err != sql.ErrNoRows {
		t.Fatal("expired work remained queued", err)
	}
	next := scheduleTest(t, s, "continue", "lead")
	deadline, err = s.CollaborationDeadline(next.Runs[0].ID)
	if err != nil || !deadline.IsZero() {
		t.Fatal("new user request inherited expired budget", deadline, err)
	}
}

func TestExpiredDiscussionCannotSelectOrInviteMoreSpeakers(t *testing.T) {
	for _, stage := range []string{"select", "invite"} {
		t.Run(stage, func(t *testing.T) {
			s, _, _ := discussionFixture(t)
			d := scheduleTest(t, s, "deadline", "discussion")
			selector, _ := claimTest(t, s, d.Runs[0])
			r := selector
			if stage == "invite" {
				child, err := s.SelectDiscussionSpeaker(selector.ID, "b", "speaker")
				if err != nil {
					t.Fatal(err)
				}
				finishTest(t, s, selector, "completed", "")
				r, _ = claimTest(t, s, child)
			}
			if _, err := s.db.Exec(`UPDATE runs SET started_at=? WHERE id=?`, time.Now().Add(-(DefaultCollaborationMinutes*time.Minute)-time.Second).UTC().Format(time.RFC3339Nano), selector.ID); err != nil {
				t.Fatal(err)
			}
			var err error
			if stage == "select" {
				_, err = s.SelectDiscussionSpeaker(r.ID, "b", "too-late")
			} else {
				_, err = s.Deliver(r.ID, "invite", SendInput{TargetAgentID: "c", Content: "继续"}, "late-message", "too-late")
			}
			if err == nil || err.Error() != CollaborationTimeReason {
				t.Fatal("time budget bypassed", err)
			}
			finishTest(t, s, r, "completed", "已有观点")
			c, _ := s.RunChain(r.ID)
			if c.Status != "incomplete" || c.Reason != CollaborationTimeReason {
				t.Fatal(c)
			}
			if _, err = s.QueuedRun(); err != sql.ErrNoRows {
				t.Fatal("expired discussion continued", err)
			}
		})
	}
}

func TestCollaborationBudgetsUseEitherLimitAndZeroDisablesEach(t *testing.T) {
	for _, tc := range []struct {
		name                                   string
		minutes, tokens, input, output, cached int
		old                                    bool
		want                                   error
	}{
		{"time", 1, 10000, 10, 10, 0, true, ErrCollaborationTime},
		{"tokens", 20, 120, 100, 20, 80, false, ErrCollaborationTokens},
		{"cache-is-input-subset", 20, 121, 100, 20, 80, false, nil},
		{"unlimited-time", 0, 1000, 100, 20, 80, true, nil},
		{"unlimited-tokens", 20, 0, 1000000, 200000, 500000, false, nil},
		{"both-unlimited", 0, 0, 1000000, 200000, 500000, true, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, c, _ := groupFixture(t)
			c.TimeBudgetMinutes, c.TokenBudget = tc.minutes, tc.tokens
			if _, err := s.SaveConversation(c, false); err != nil {
				t.Fatal(err)
			}
			d := scheduleTest(t, s, "budget", "lead")
			r, _ := claimTest(t, s, d.Runs[0])
			if tc.old {
				if _, err := s.db.Exec(`UPDATE runs SET started_at='2000-01-01T00:00:00Z' WHERE id=?`, r.ID); err != nil {
					t.Fatal(err)
				}
			}
			if err := s.RecordTokenUsage(r.ID, tc.input, tc.output, tc.cached, false, r.Revision+1); err != nil {
				t.Fatal(err)
			}
			if err := s.CheckCollaborationBudget(r.ID); !errors.Is(err, tc.want) {
				t.Fatal(err, tc.want)
			}
			chain, err := s.RunChain(r.ID)
			if err != nil || chain.TimeBudgetMinutes != tc.minutes || chain.TokenBudget != tc.tokens {
				t.Fatal(chain, err)
			}
		})
	}
}

func TestTokenUsagePersistsAcrossRetryAndDoesNotResetForNewSettings(t *testing.T) {
	s, c, dir := groupFixture(t)
	c.TokenBudget = 300
	if _, err := s.SaveConversation(c, false); err != nil {
		t.Fatal(err)
	}
	d := scheduleTest(t, s, "first", "lead")
	r, _ := claimTest(t, s, d.Runs[0])
	if err := s.RecordTokenUsage(r.ID, 100, 20, 60, false, r.Revision+1); err != nil {
		t.Fatal(err)
	}
	finishTest(t, s, r, "failed", "partial")
	retry, err := s.RetryRun(r.ID, "retry-tokens", "retry-tokens-run")
	if err != nil {
		t.Fatal(err)
	}
	retry, _ = claimTest(t, s, retry)
	if err = s.RecordTokenUsage(retry.ID, 150, 30, 80, false, retry.Revision+1); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(s.CheckCollaborationBudget(retry.ID), ErrCollaborationTokens) {
		t.Fatal("retry reset the budget")
	}
	finishTest(t, s, retry, "interrupted", "partial")
	s.Close()
	s, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	chain, _ := s.RunChain(r.ID)
	if chain.Status != "incomplete" || chain.Reason != CollaborationTokenReason {
		t.Fatal(chain)
	}
	c.TokenBudget = 0
	if _, err = s.SaveConversation(c, false); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(s.CheckCollaborationBudget(r.ID), ErrCollaborationTokens) {
		t.Fatal("new settings reset an old chain")
	}
	next := scheduleTest(t, s, "next", "lead")
	if err = s.CheckCollaborationBudget(next.Runs[0].ID); err != nil {
		t.Fatal(err)
	}
	runs, err := s.ConversationRuns(c.ID)
	if err != nil {
		t.Fatal(err)
	}
	var input, output, cached int
	for _, run := range runs {
		input += run.InputTokens
		output += run.OutputTokens
		cached += run.CachedTokens
	}
	if input != 250 || output != 50 || cached != 140 {
		t.Fatal(input, output, cached)
	}
}
