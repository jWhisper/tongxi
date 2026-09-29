package store

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestSchemaSevenPreservesLegacyChainsAndForeignKeys(t *testing.T) {
	dir := t.TempDir()
	old, err := sql.Open("sqlite", filepath.Join(dir, "tongxi.db"))
	if err != nil {
		t.Fatal(err)
	}
	for _, migration := range migrations[:6] {
		if _, err = old.Exec(migration); err != nil {
			t.Fatal(err)
		}
	}
	_, err = old.Exec(`PRAGMA user_version=6;
	INSERT INTO agents VALUES('a','lead','','instruction','https://example.test/v1','test','ref','[]',1,1,'','');
	INSERT INTO agents VALUES('b','member','','instruction','https://example.test/v1','test','ref','[]',1,1,'','');
	INSERT INTO conversations VALUES('group','old','group','lead','a','','',1);
	INSERT INTO conversation_members VALUES('group','a',0),('group','b',1);
	INSERT INTO messages(id,conversation_id,sequence,sender_type,sender_name,content,created_at) VALUES('root','group',1,'user','you','task','');
	INSERT INTO chains(id,conversation_id,message_id,mode,action,lead_agent_id,participants,reserved,created_at) VALUES('old-chain','group','root','lead','lead','a','["a"]',2,'');
	INSERT INTO runs(id,conversation_id,agent_id,message_id,status,error,created_at,chain_id) VALUES('old-lead','group','a','root','completed','','','old-chain');
	INSERT INTO runs(id,conversation_id,agent_id,message_id,status,error,created_at,chain_id,parent_run_id,previous_run_id) VALUES('old-member','group','b','root','queued','','','old-chain','old-lead','old-lead');`)
	if err != nil {
		t.Fatal(err)
	}
	old.Close()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	chain, err := s.RunChain("old-member")
	if err != nil || chain.LeadPolicy != 0 || chain.Limit != 6 || chain.Work != nil || chain.Reserved != 2 {
		t.Fatal(chain, err)
	}
	r, err := s.QueuedRun()
	if err != nil || r.ID != "old-member" {
		t.Fatal(r, err)
	}
	r, _ = claimTest(t, s, r)
	finishTest(t, s, r, "completed", "旧成员意见")
	final, err := s.QueuedRun()
	if err != nil || final.AgentID != "a" {
		t.Fatal("legacy continuation lost", final, err)
	}
	if _, err = s.db.Exec(`INSERT INTO lead_steps VALUES('missing','{}','{}',NULL)`); err == nil {
		t.Fatal("foreign keys not restored")
	}
	var version, violations int
	s.db.QueryRow(`PRAGMA user_version`).Scan(&version)
	s.db.QueryRow(`SELECT count(*) FROM pragma_foreign_key_check`).Scan(&violations)
	if version != len(migrations) || violations != 0 {
		t.Fatal(version, violations)
	}
}

func pendingStep(target, result string) LeadStep {
	return LeadStep{Action: "delegate", Checks: []AcceptanceCheck{{Criterion: "总时长为30分钟", Status: "pending"}, {Criterion: "每个环节均可自愿跳过", Status: "unmet", Evidence: "需要补充退出方式"}}, Result: result, Reason: "请完善未满足的条件", NextAgentID: target, Task: "完善方案并提供完整版本"}
}

func TestPublishedLeadStepsExcludeUnfinishedAndOtherConversations(t *testing.T) {
	for _, status := range []string{"completed", "failed", "cancelled", "interrupted"} {
		t.Run(status, func(t *testing.T) {
			s, _, _ := groupFixture(t)
			d := scheduleTest(t, s, "display", "lead")
			r, _ := claimTest(t, s, d.Runs[0])
			out, err := s.AdvanceLead(r.ID, pendingStep("b", "完整草稿"), "invitation", "member")
			if err != nil {
				t.Fatal(err)
			}
			steps, err := s.PublishedLeadSteps("group")
			if err != nil || len(steps) != 0 {
				t.Fatal("unpublished decision returned", steps, err)
			}
			finishTest(t, s, r, status, out.Step.PublicText())
			steps, err = s.PublishedLeadSteps("group")
			if err != nil {
				t.Fatal(err)
			}
			if status == "completed" {
				if len(steps) != 1 || steps[r.ID].Result != "完整草稿" {
					t.Fatal("published draft missing", steps)
				}
			} else if len(steps) != 0 {
				t.Fatal("unsuccessful decision returned", steps)
			}
			steps, err = s.PublishedLeadSteps("other")
			if err != nil || len(steps) != 0 {
				t.Fatal("decision crossed conversation boundary", steps, err)
			}
		})
	}
}

func TestLeadRevisesOneMemberAtATimeAndKeepsCriteriaAcrossRestart(t *testing.T) {
	s, _, dir := groupFixture(t)
	d := scheduleTest(t, s, "work", "lead")
	leader, _ := claimTest(t, s, d.Runs[0])
	step := pendingStep("b", "")
	first, err := s.AdvanceLead(leader.ID, step, "task1", "member1")
	if err != nil {
		t.Fatal(err)
	}
	dup, err := s.AdvanceLead(leader.ID, step, "duplicate", "duplicate-run")
	if err != nil || dup.Run.ID != first.Run.ID {
		t.Fatal(dup, err)
	}
	step.NextAgentID = "c"
	if _, err = s.AdvanceLead(leader.ID, step, "broadcast", "broadcast-run"); err == nil {
		t.Fatal("broadcast accepted")
	}
	if _, err = s.QueuedRun(); err != sql.ErrNoRows {
		t.Fatal("member ran before lead finished")
	}
	finishTest(t, s, leader, "completed", first.Step.PublicText())
	member, _ := claimTest(t, s, *first.Run)
	if _, err = s.AdvanceLead(member.ID, step, "hijack", "hijack-run"); err == nil {
		t.Fatal("member changed lead plan")
	}
	finishTest(t, s, member, "completed", "初稿：5分钟签到，25分钟讨论，缺少跳过方式")
	finishTest(t, s, member, "completed", "duplicate")
	s.Close()
	s, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	checkRun, err := s.QueuedRun()
	if err != nil || checkRun.AgentID != "a" || checkRun.MessageID != d.MessageID {
		t.Fatal(checkRun, err)
	}
	checkRun, input := claimTest(t, s, checkRun)
	if !strings.Contains(input, "初稿") {
		t.Fatal("missing member feedback")
	}
	chain, _ := s.RunChain(checkRun.ID)
	if chain.Work == nil || len(chain.Work.Checks) != 2 || chain.Limit != 13 {
		t.Fatal(chain)
	}
	step = pendingStep("b", "初稿：5分钟签到，25分钟讨论")
	step.Checks[0].Status, step.Checks[0].Evidence = "met", "5+25=30"
	bad := step
	bad.Checks = append([]AcceptanceCheck{}, step.Checks...)
	bad.Checks[1].Criterion = "大家都说过话就算通过"
	if _, err = s.AdvanceLead(checkRun.ID, bad, "lower", "lower-run"); err == nil {
		t.Fatal("criteria lowered")
	}
	bad = step
	bad.Action, bad.NextAgentID, bad.Task = "complete", "", ""
	if _, err = s.AdvanceLead(checkRun.ID, bad, "early", "early-run"); err == nil {
		t.Fatal("unmet work accepted")
	}
	revision, err := s.AdvanceLead(checkRun.ID, step, "task2", "member2")
	if err != nil {
		t.Fatal(err)
	}
	finishTest(t, s, checkRun, "completed", revision.Step.PublicText())
	member, _ = claimTest(t, s, *revision.Run)
	finishTest(t, s, member, "completed", "修订稿：可以旁听或跳过")
	checkRun, _ = s.QueuedRun()
	checkRun, _ = claimTest(t, s, checkRun)
	step.Action, step.NextAgentID, step.Task = "complete", "", ""
	step.Result = "5分钟签到，25分钟讨论；签到可略过，讨论可旁听或跳过。"
	step.Checks[1].Status, step.Checks[1].Evidence = "met", "两个环节都有跳过方式"
	last, err := s.AdvanceLead(checkRun.ID, step, "unused", "unused-run")
	if err != nil {
		t.Fatal(err)
	}
	chain, _ = s.RunChain(checkRun.ID)
	if chain.Status != "active" {
		t.Fatal("accepted before run completed")
	}
	finishTest(t, s, checkRun, "completed", last.Step.PublicText())
	chain, _ = s.RunChain(checkRun.ID)
	runs, _ := s.ConversationRuns("group")
	if chain.Status != "completed" || chain.Reserved != 5 || len(runs) != 5 {
		t.Fatal(chain, runs)
	}
	for _, run := range runs {
		if run.AgentID == "c" {
			t.Fatal("unused member invited")
		}
	}
	if _, err = s.QueuedRun(); err != sql.ErrNoRows {
		t.Fatal("work remains", err)
	}
}

func TestLeadBudgetsAndNoProgressNeverBecomeAccepted(t *testing.T) {
	for _, cause := range []string{"budget", "stalled", "time"} {
		t.Run(cause, func(t *testing.T) {
			s, _, _ := groupFixture(t)
			d := scheduleTest(t, s, cause, "lead")
			current := d.Runs[0]
			if cause == "time" {
				s.db.Exec(`UPDATE chains SET created_at=? WHERE id=?`, time.Now().Add(-LeadDuration-time.Second).UTC().Format(time.RFC3339Nano), d.ChainID)
			}
			for i := 0; i < 8; i++ {
				leader, _ := claimTest(t, s, current)
				result := fmt.Sprintf("第%d版成果，仍缺少退出方式", i)
				if cause == "stalled" {
					result = "相同成果"
				}
				out, err := s.AdvanceLead(leader.ID, pendingStep("b", result), fmt.Sprintf("task%d", i), fmt.Sprintf("member%d", i))
				if err != nil {
					t.Fatal(err)
				}
				finishTest(t, s, leader, "completed", out.Step.PublicText())
				if out.Step.Action == "pause" {
					chain, _ := s.RunChain(leader.ID)
					if chain.Status != "incomplete" || out.Run != nil || !strings.Contains(out.Step.PublicText(), "尚未完成") {
						t.Fatal(chain, out)
					}
					if cause == "budget" && chain.Reserved != LeadLimit {
						t.Fatal("wrong execution limit", chain)
					}
					if cause == "stalled" && chain.Stalled != 2 {
						t.Fatal("no-progress guard", chain)
					}
					if _, err = s.QueuedRun(); err != sql.ErrNoRows {
						t.Fatal("work after pause")
					}
					return
				}
				member, _ := claimTest(t, s, *out.Run)
				finishTest(t, s, member, "completed", "成员反馈")
				current, err = s.QueuedRun()
				if err != nil {
					t.Fatal(err)
				}
			}
			t.Fatal("budget failed to stop")
		})
	}
}

func TestLeadDecisionRollbackValidationAndStopRace(t *testing.T) {
	s, _, _ := groupFixture(t)
	d := scheduleTest(t, s, "atomic", "lead")
	r, _ := claimTest(t, s, d.Runs[0])
	for _, target := range []string{"a", "outsider", "missing"} {
		if _, err := s.AdvanceLead(r.ID, pendingStep(target, "draft"), "bad", "bad-r"); err == nil {
			t.Fatal("bad target accepted")
		}
	}
	s.db.Exec(`CREATE TRIGGER fail_lead_step BEFORE INSERT ON lead_steps BEGIN SELECT RAISE(ABORT,'simulated disk failure'); END`)
	if _, err := s.AdvanceLead(r.ID, pendingStep("b", "draft"), "rollback", "rollback-r"); err == nil {
		t.Fatal("expected failure")
	}
	s.db.Exec(`DROP TRIGGER fail_lead_step`)
	chain, _ := s.RunChain(r.ID)
	msgs, _ := s.Messages("group")
	if chain.Work != nil || chain.Reserved != 1 || len(msgs) != 1 {
		t.Fatal("partial decision escaped transaction")
	}
	var wg sync.WaitGroup
	var out LeadAdvance
	var advanceErr, stopErr error
	start := make(chan struct{})
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		out, advanceErr = s.AdvanceLead(r.ID, pendingStep("b", "draft"), "race-m", "race-r")
	}()
	go func() { defer wg.Done(); <-start; _, stopErr = s.StopChain(d.ChainID, &r) }()
	close(start)
	wg.Wait()
	if stopErr != nil {
		t.Fatal(stopErr)
	}
	if advanceErr == nil {
		child, _ := s.ConversationRun(out.Run.ID)
		if child.Status != "cancelled" {
			t.Fatal("child survived stop", child)
		}
	}
	if _, err := s.AdvanceLead(r.ID, pendingStep("c", "late"), "late", "late-r"); err == nil {
		t.Fatal("late decision accepted")
	}
	finishTest(t, s, r, "completed", "late result")
	chain, _ = s.RunChain(r.ID)
	if chain.Status != "stopped" {
		t.Fatal(chain)
	}
	if _, err := s.QueuedRun(); err != sql.ErrNoRows {
		t.Fatal(err)
	}
}

func TestLeadRetryReservesReturnAndRejectsMissingEvidence(t *testing.T) {
	s, _, _ := groupFixture(t)
	d := scheduleTest(t, s, "retry-work", "lead")
	r, _ := claimTest(t, s, d.Runs[0])
	step := pendingStep("b", "draft")
	out, err := s.AdvanceLead(r.ID, step, "m", "member")
	if err != nil {
		t.Fatal(err)
	}
	finishTest(t, s, r, "completed", out.Step.PublicText())
	member, _ := claimTest(t, s, *out.Run)
	finishTest(t, s, member, "failed", "partial")
	retry, err := s.RetryRun(member.ID, "retry", "retried-member")
	if err != nil {
		t.Fatal(err)
	}
	retry, _ = claimTest(t, s, retry)
	finishTest(t, s, retry, "completed", "修订结果")
	lead, _ := s.QueuedRun()
	lead, _ = claimTest(t, s, lead)
	step.Action, step.NextAgentID, step.Task = "complete", "", ""
	for i := range step.Checks {
		step.Checks[i].Status = "met"
		step.Checks[i].Evidence = ""
	}
	if _, err = s.AdvanceLead(lead.ID, step, "invalid", "invalid-r"); err == nil {
		t.Fatal("empty evidence accepted")
	}
	step.Action, step.Reason = "pause", "缺少参加人数，请用户补充"
	step.Checks[0].Status, step.Checks[1].Status = "pending", "unmet"
	out, err = s.AdvanceLead(lead.ID, step, "pause", "pause-r")
	if err != nil {
		t.Fatal(err)
	}
	finishTest(t, s, lead, "completed", out.Step.PublicText())
	chain, _ := s.RunChain(lead.ID)
	if chain.Status != "incomplete" || chain.Reserved != 4 {
		t.Fatal(chain)
	}
}
