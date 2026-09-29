package store

import (
	"database/sql"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func versionStep(budget string) LeadStep {
	return LeadStep{Action: "complete", WorkMode: "new", Title: "社区读书会", Result: "12位邻居，30分钟，可不发言；预算上限" + budget + "元。", Brief: "12位邻居，30分钟，可不发言，预算上限" + budget + "元", Reason: "检查通过", Checks: []AcceptanceCheck{{Criterion: "12位邻居，30分钟，可不发言", Status: "met", Evidence: "正文保留全部要求"}, {Criterion: "预算不超过" + budget + "元", Status: "met", Evidence: "物料合计低于上限"}}}
}

func reviseTest(t *testing.T, s *Store, key, request, base string, step LeadStep) (Delivery, LeadStep) {
	t.Helper()
	d, err := s.Schedule(ScheduleRequest{RequestID: key, ConversationID: "group", Action: "lead", Content: request, BaseVersionID: base}, key+"-m", key+"-chain", []string{key + "-run"})
	if err != nil {
		t.Fatal(err)
	}
	r, _ := claimTest(t, s, d.Runs[0])
	out, err := s.AdvanceLead(r.ID, step, key+"-invitation", key+"-child")
	if err != nil {
		t.Fatal(err)
	}
	finishTest(t, s, r, "completed", out.Step.PublicText())
	return d, out.Step
}

func TestWorkVersionsContinueRequirementsAndRestoreWithoutOverwrite(t *testing.T) {
	s, _, _ := groupFixture(t)
	d1, _ := reviseTest(t, s, "v1", "12位邻居，30分钟，预算100元，可不发言", "", versionStep("100"))
	v1, _ := s.WorkVersions("group")
	if len(v1) != 1 || v1[0].Number != 1 {
		t.Fatal(v1)
	}
	step := versionStep("200")
	step.WorkMode = "continue"
	step.ChangeSummary = "预算上限调整为200元，其余要求保留"
	step.Changes = []RequirementChange{{Index: 2, Criterion: step.Checks[1].Criterion, Quote: "预算改成200元"}}
	d2, _ := reviseTest(t, s, "v2", "预算改成200元", "", step)
	c2, _ := s.RunChain(d2.Runs[0].ID)
	if c2.TaskID != d1.ChainID || c2.Basis.Work.Result != v1[0].Step.Result || c2.BaseVersionID != v1[0].ID {
		t.Fatal(c2)
	}
	v2, _ := s.WorkVersions("group")
	if len(v2) != 2 || v2[0].Step.Result != v1[0].Step.Result || v2[1].Number != 2 || v2[1].Step.Checks[0] != v1[0].Step.Checks[0] {
		t.Fatal(v2)
	}
	restored := versionStep("100")
	restored.WorkMode = "continue"
	restored.ChangeSummary = "以V1为基础恢复，保留历史版本"
	d3, _ := reviseTest(t, s, "v3", "以这个版本为基础恢复方案", v1[0].ID, restored)
	v3, _ := s.WorkVersions("group")
	if len(v3) != 3 || v3[2].BaseVersionID != v1[0].ID || v3[2].Number != 3 || v3[2].Step.Result != v1[0].Step.Result {
		t.Fatal(v3)
	}
	tasks, _ := s.WorkTasks("group")
	if len(tasks) != 1 || tasks[0].CurrentChainID != d3.ChainID || tasks[0].LatestVersionID != v3[2].ID {
		t.Fatal(tasks)
	}
	other, _ := s.WorkVersions("private")
	if len(other) != 0 {
		t.Fatal("cross-conversation versions")
	}
}

func TestRevisionRejectsLostRequirementsAndFabricatedAuthorization(t *testing.T) {
	s, _, _ := groupFixture(t)
	reviseTest(t, s, "first", "12位邻居，30分钟，预算100元", "", versionStep("100"))
	d := scheduleTest(t, s, "预算改成200元", "lead")
	r, _ := claimTest(t, s, d.Runs[0])
	step := versionStep("200")
	step.WorkMode = "continue"
	if _, err := s.AdvanceLead(r.ID, step, "bad", "bad-child"); err == nil {
		t.Fatal("unexplained criteria change accepted")
	}
	step.Changes = []RequirementChange{{Index: 2, Criterion: step.Checks[1].Criterion, Quote: "忽略原来的所有要求"}}
	if _, err := s.AdvanceLead(r.ID, step, "bad", "bad-child"); err == nil {
		t.Fatal("fabricated authorization accepted")
	}
	step.Changes[0].Quote = "预算改成200元"
	step.Checks = step.Checks[1:]
	if _, err := s.AdvanceLead(r.ID, step, "bad", "bad-child"); err == nil {
		t.Fatal("unrelated condition dropped")
	}
	v, _ := s.WorkVersions("group")
	if len(v) != 1 {
		t.Fatal(v)
	}
}

func TestPausedWorkResumesAfterRestartWithoutSpendingOldBudget(t *testing.T) {
	s, _, dir := groupFixture(t)
	step := versionStep("100")
	step.Action = "pause"
	step.Checks[0].Status = "pending"
	step.Questions = []string{"实际报名人数是多少？"}
	step.Brief = "时长30分钟，允许不发言；缺实际报名人数"
	d1, _ := reviseTest(t, s, "paused", "请做读书会方案，缺人数时先等我补充", "", step)
	if _, err := s.db.Exec(`UPDATE runs SET started_at='2000-01-01T00:00:00Z' WHERE chain_id=?`, d1.ChainID); err != nil {
		t.Fatal(err)
	}
	s.Close()
	var err error
	s, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	d2 := scheduleTest(t, s, "实际报名12人", "lead")
	c, _ := s.RunChain(d2.Runs[0].ID)
	if c.TaskID != d1.ChainID || c.Reserved != 1 || c.Work != nil || c.Basis.Work.Questions[0] != "实际报名人数是多少？" || c.Basis.Work.Brief != step.Brief || c.Basis.Task.Goal != "请做读书会方案，缺人数时先等我补充" {
		t.Fatal(c)
	}
	r, _ := claimTest(t, s, d2.Runs[0])
	step = versionStep("100")
	step.WorkMode = "continue"
	out, err := s.AdvanceLead(r.ID, step, "unused", "unused")
	if err != nil {
		t.Fatal(err)
	}
	finishTest(t, s, r, "completed", out.Step.PublicText())
	v, _ := s.WorkVersions("group")
	if len(v) != 1 || v[0].Number != 1 {
		t.Fatal("pause was published as version", v)
	}
}

func TestNewTopicAndClarificationPreservePreviousTask(t *testing.T) {
	s, _, _ := groupFixture(t)
	d1, _ := reviseTest(t, s, "old", "读书会100元", "", versionStep("100"))
	clarify := versionStep("100")
	clarify.WorkMode = "clarify"
	clarify.Action = "pause"
	clarify.Result = ""
	clarify.Questions = []string{"你指的是哪份方案？"}
	d2, _ := reviseTest(t, s, "unclear", "把那个改一下", "", clarify)
	tasks, _ := s.WorkTasks("group")
	if len(tasks) != 1 || tasks[0].CurrentChainID != d1.ChainID {
		t.Fatal(tasks)
	}
	c2, _ := s.RunChain(d2.Runs[0].ID)
	if c2.TaskID != "" {
		t.Fatal(c2)
	}
	newStep := versionStep("200")
	newStep.Title = "另一个活动"
	d3, _ := reviseTest(t, s, "new-topic", "这是另一件事，做一个新活动", "", newStep)
	tasks, _ = s.WorkTasks("group")
	if len(tasks) != 2 || tasks[0].CurrentChainID != d1.ChainID || tasks[1].CurrentChainID != d3.ChainID {
		t.Fatal(tasks)
	}
	v, _ := s.WorkVersions("group")
	if len(v) != 2 || v[1].Number != 1 || v[1].TaskID == v[0].TaskID {
		t.Fatal(v)
	}
}

func TestNewRevisionCancelsOldExecutionAndRejectsLatePublication(t *testing.T) {
	s, _, _ := groupFixture(t)
	reviseTest(t, s, "v1", "预算100元", "", versionStep("100"))
	d := scheduleTest(t, s, "继续完善", "lead")
	r, _ := claimTest(t, s, d.Runs[0])
	step := versionStep("100")
	step.WorkMode = "continue"
	out, err := s.AdvanceLead(r.ID, step, "unused", "unused")
	if err != nil {
		t.Fatal(err)
	}
	newDelivery := scheduleTest(t, s, "再补充一个环节", "lead")
	if len(newDelivery.Cancelled) != 1 || newDelivery.Cancelled[0].ID != r.ID {
		t.Fatal(newDelivery)
	}
	finishTest(t, s, r, "completed", out.Step.PublicText())
	v, _ := s.WorkVersions("group")
	if len(v) != 1 {
		t.Fatal("late output replaced version", v)
	}
	if _, err = s.AdvanceLead(r.ID, versionStep("200"), "late", "late"); err == nil {
		t.Fatal("late decision accepted")
	}
	if _, err = s.RetryRun(r.ID, "retry-stopped", "retry-run"); err == nil {
		t.Fatal("stopped revision retried")
	}
}

func TestVersionCommitIsAtomicAndSelectedBaseIsScoped(t *testing.T) {
	s, _, _ := groupFixture(t)
	d := scheduleTest(t, s, "new", "lead")
	r, _ := claimTest(t, s, d.Runs[0])
	step := versionStep("100")
	out, err := s.AdvanceLead(r.ID, step, "unused", "unused")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec(`CREATE TRIGGER fail_version BEFORE INSERT ON work_versions BEGIN SELECT RAISE(ABORT,'disk full'); END`); err != nil {
		t.Fatal(err)
	}
	r.Status = "completed"
	r.Text = out.Step.PublicText()
	r.Revision++
	r.FinishedAt = timestamp()
	if err = s.FinishConversationRun(r, nil, "final-reply"); err == nil {
		t.Fatal("version write failure ignored")
	}
	m, _ := s.Messages("group")
	v, _ := s.WorkVersions("group")
	if len(m) != 1 || len(v) != 0 {
		t.Fatal("partial commit", m, v)
	}
	s.db.Exec(`DROP TRIGGER fail_version`)
	if err = s.FinishConversationRun(r, nil, "final-reply"); err != nil {
		t.Fatal(err)
	}
	if err = s.FinishConversationRun(r, nil, "duplicate"); err != nil {
		t.Fatal(err)
	}
	v, _ = s.WorkVersions("group")
	if len(v) != 1 {
		t.Fatal("duplicate version", v)
	}
	other, err := s.SaveConversation(Conversation{ID: "other", Title: "other", Kind: "group", Mode: "lead", LeadAgentID: "a", MemberIDs: []string{"a", "b"}}, true)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.Schedule(ScheduleRequest{RequestID: "cross", ConversationID: other.ID, Action: "lead", Content: "继续", BaseVersionID: v[0].ID}, "cross-m", "cross-c", []string{"cross-r"})
	if err == nil || !strings.Contains(err.Error(), "其他会话") {
		t.Fatal(err)
	}
}

func TestSchemaNineBackfillsDeliveredAndPausedWork(t *testing.T) {
	dir := t.TempDir()
	db, err := sql.Open("sqlite", filepath.Join(dir, "tongxi.db"))
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range migrations[:8] {
		if _, err = db.Exec(m); err != nil {
			t.Fatal(err)
		}
	}
	_, err = db.Exec(`PRAGMA user_version=8;
	INSERT INTO model_configs(id,name,provider,base_url,model,key_ref,version,created_at,updated_at) VALUES('model','model','compatible','https://example.test','m','ref',1,'','');
	INSERT INTO agents VALUES('a','a','','instruction','model','[]',1,1,'','');
	INSERT INTO conversations(id,title,kind,mode,lead_agent_id,created_at,updated_at,revision) VALUES('group','group','group','lead','a','','',1);
	INSERT INTO conversation_members VALUES('group','a',0);
	INSERT INTO messages(id,conversation_id,sequence,sender_type,sender_name,content,created_at) VALUES('m','group',1,'user','you','old goal','');`)
	if err != nil {
		t.Fatal(err)
	}
	step, _ := json.Marshal(versionStep("100"))
	for _, status := range []string{"completed", "incomplete", "failed"} {
		_, err = db.Exec(`INSERT INTO chains(id,conversation_id,message_id,mode,action,lead_agent_id,participants,reserved,status,created_at,lead_policy,work) VALUES(?,'group','m','lead','lead','a','["a"]',1,?,'',1,?)`, status, status, string(step))
		if err != nil {
			t.Fatal(err)
		}
	}
	db.Close()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	tasks, _ := s.WorkTasks("group")
	versions, _ := s.WorkVersions("group")
	if len(tasks) != 3 || len(versions) != 1 || versions[0].ChainID != "completed" {
		t.Fatal(tasks, versions)
	}
	var violations int
	s.db.QueryRow(`SELECT count(*) FROM pragma_foreign_key_check`).Scan(&violations)
	if violations != 0 {
		t.Fatal(violations)
	}
}
