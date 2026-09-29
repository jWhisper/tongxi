package store

import (
	"database/sql"
	"strings"
	"testing"
)

func TestLegacyAutomaticConclusionWaitsForAllMembersAndSurvivesRestart(t *testing.T) {
	s, c, dir := groupFixture(t)
	// Existing discussion rooms use their first enabled member automatically.
	c.Mode, c.LeadAgentID = "discussion", ""
	if _, err := s.SaveConversation(c, false); err != nil {
		t.Fatal(err)
	}
	d := scheduleLegacyLeadTest(t, s, "automatic")
	lead, _ := claimTest(t, s, d.Runs[0])
	for _, id := range []string{"b", "c"} {
		if _, err := s.Deliver(lead.ID, id, SendInput{TargetAgentID: id, Content: "请提供意见"}, id+"-request", id+"-run"); err != nil {
			t.Fatal(err)
		}
	}
	finishTest(t, s, lead, "completed", "已邀请伙伴")
	b, _ := s.QueuedRun()
	b, _ = claimTest(t, s, b)
	finishTest(t, s, b, "completed", "B 的观点")
	runs, _ := s.ConversationRuns(c.ID)
	if len(runs) != 3 {
		t.Fatal("summary ran before all members", runs)
	}
	last, _ := s.QueuedRun()
	last, _ = claimTest(t, s, last)
	finishTest(t, s, last, "completed", "C 的观点")
	finishTest(t, s, last, "completed", "duplicate completion")
	s.Close()
	var err error
	s, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	summary, err := s.QueuedRun()
	if err != nil || summary.AgentID != "a" || summary.MessageID != d.MessageID || summary.ParentRunID != last.ID {
		t.Fatal("summary missing or wrong trigger", summary, err)
	}
	summary, input := claimTest(t, s, summary)
	if !strings.Contains(input, "B 的观点") || !strings.Contains(input, "C 的观点") || !strings.Contains(input, "automatic") {
		t.Fatal("summary missed original question or feedback", input)
	}
	if allowed, _ := s.CanCollaborate(summary.ID); allowed {
		t.Fatal("final conclusion can start another discussion")
	}
	if _, err = s.Deliver(summary.ID, "late", SendInput{TargetAgentID: "b", Content: "loop"}, "late-m", "late-r"); err == nil {
		t.Fatal("summary bypassed delivery policy")
	}
	finishTest(t, s, summary, "completed", "最终结论")
	chains, _ := s.Chains(c.ID)
	runs, _ = s.ConversationRuns(c.ID)
	if len(runs) != 4 || chains[0].Reserved != 4 || chains[0].Status != "completed" || chains[0].LeadAgentID != "a" {
		t.Fatal("unexpected additional work", chains, runs)
	}
	if _, err = s.QueuedRun(); err != sql.ErrNoRows {
		t.Fatal("work remains", err)
	}
}

func TestLegacyAutomaticConclusionStopDisableAndRetry(t *testing.T) {
	for _, mode := range []string{"stop", "disable-lead", "retry-member", "retry-summary"} {
		t.Run(mode, func(t *testing.T) {
			s, _, _ := groupFixture(t)
			d := scheduleLegacyLeadTest(t, s, "automatic")
			lead, _ := claimTest(t, s, d.Runs[0])
			child, err := s.Deliver(lead.ID, "invite", SendInput{TargetAgentID: "b", Content: "review"}, "invite-m", "invite-r")
			if err != nil {
				t.Fatal(err)
			}
			finishTest(t, s, lead, "completed", "已邀请")
			member, _ := claimTest(t, s, child.Runs[0])
			switch mode {
			case "stop":
				_, err = s.StopChain(d.ChainID, &member)
			case "disable-lead":
				_, _, err = s.SetAgentEnabledAndCancel("a", false, &member)
			case "retry-member":
				finishTest(t, s, member, "failed", "")
				member, err = s.RetryRun(member.ID, "retry-member", "retry-member-run")
				if err == nil {
					member, _ = claimTest(t, s, member)
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			finishTest(t, s, member, "completed", "评审意见")
			summary, err := s.QueuedRun()
			if mode == "stop" || mode == "disable-lead" {
				if err != sql.ErrNoRows {
					t.Fatal("stopped collaboration still summarized", summary, err)
				}
				return
			}
			if err != nil || summary.AgentID != "a" {
				t.Fatal(summary, err)
			}
			summary, _ = claimTest(t, s, summary)
			if mode == "retry-summary" {
				finishTest(t, s, summary, "failed", "")
				summary, err = s.RetryRun(summary.ID, "retry-summary", "retry-summary-run")
				if err != nil {
					t.Fatal(err)
				}
				summary, _ = claimTest(t, s, summary)
			}
			finishTest(t, s, summary, "completed", "最终结论")
			chains, _ := s.Chains("group")
			if chains[0].Status != "completed" || chains[0].Reserved != 4 {
				t.Fatal(chains)
			}
		})
	}
}

// Old queued chains exercise their pre-0.1.3 routing.
func scheduleLegacyLeadTest(t *testing.T, s *Store, key string) Delivery {
	t.Helper()
	d := scheduleTest(t, s, key, "lead")
	if _, err := s.db.Exec(`UPDATE chains SET lead_policy=0 WHERE id=?`, d.ChainID); err != nil {
		t.Fatal(err)
	}
	return d
}
