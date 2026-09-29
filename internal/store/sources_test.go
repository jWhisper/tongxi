package store

import (
	"fmt"
	"strings"
	"testing"
)

func sourceFixture(t *testing.T, s *Store, id, conversation string) Source {
	t.Helper()
	v, err := s.SaveSource(Source{ID: id, ConversationID: conversation, Name: "活动.txt", Kind: "file", Format: "txt", Hash: id}, []SourceSegment{{Location: "第1行", Content: "报名人数12人，预算100元。"}, {Location: "第2行", Content: "参与者可不发言。"}}, "")
	if err != nil {
		t.Fatal(err)
	}
	return v
}
func TestCitationRequiresReadCorrectQuoteAndConversation(t *testing.T) {
	s, _, _ := groupFixture(t)
	v := sourceFixture(t, s, strings.Repeat("a", 32), "group")
	other := sourceFixture(t, s, strings.Repeat("b", 32), "one")
	d := scheduleTest(t, s, "根据资料写方案", "lead")
	r, _ := claimTest(t, s, d.Runs[0])
	step := versionStep("100")
	step.Result = "12人，预算100元。[活动资料](source://" + v.ID + "/1)"
	step.Citations = []Citation{{SourceID: v.ID, Segment: 1, Quote: "报名人数12人，预算100元。"}}
	if _, err := s.AdvanceLead(r.ID, step, "before", "child"); err == nil {
		t.Fatal("unread citation accepted")
	}
	if _, err := s.ReadSource("group", other.ID, 1, r.ID); err == nil {
		t.Fatal("cross-conversation read")
	}
	page, err := s.ReadSource("group", v.ID, 1, r.ID)
	if err != nil || len(page.Segments) != 2 {
		t.Fatal(page, err)
	}
	step.Citations[0].Quote = "预算200元"
	if _, err = s.AdvanceLead(r.ID, step, "bad", "child"); err == nil {
		t.Fatal("fabricated quotation")
	}
	step.Citations[0].Quote = "报名人数12人，预算100元。"
	step.Result += "[不存在](source://" + other.ID + "/1)"
	if _, err = s.AdvanceLead(r.ID, step, "bad", "child"); err == nil {
		t.Fatal("undeclared reference")
	}
	step.Result = "12人，预算100元。[活动资料](source://" + v.ID + "/1)"
	out, err := s.AdvanceLead(r.ID, step, "done", "child")
	if err != nil {
		t.Fatal(err)
	}
	finishTest(t, s, r, "completed", out.Step.PublicText())
	versions, _ := s.WorkVersions("group")
	if len(versions) != 1 || len(versions[0].Step.Citations) != 1 {
		t.Fatal(versions)
	}
	step.WorkMode = "continue"
	reviseTest(t, s, "continue-sources", "沿用已核对来源完善表述", versions[0].ID, step)
	versions, _ = s.WorkVersions("group")
	if len(versions) != 2 || versions[1].Step.Citations[0] != versions[0].Step.Citations[0] {
		t.Fatal("lost historical citation")
	}
}
func TestSourceSnapshotsDedupPaginationAndCancelledRun(t *testing.T) {
	s, _, _ := groupFixture(t)
	v := sourceFixture(t, s, strings.Repeat("a", 32), "group")
	copy := sourceFixture(t, s, v.ID, "group")
	if copy.ID != v.ID {
		t.Fatal("deduplication failed")
	}
	sources, _ := s.Sources("group")
	if len(sources) != 1 {
		t.Fatal(sources)
	}
	parts := []SourceSegment{}
	for i := 0; i < 14; i++ {
		parts = append(parts, SourceSegment{Location: fmt.Sprintf("第%d行", i+1), Content: strings.Repeat("字", 1600)})
	}
	large, err := s.SaveSource(Source{ID: strings.Repeat("c", 32), ConversationID: "group", Name: "多页", Hash: "new", Kind: "file"}, parts, "")
	if err != nil {
		t.Fatal(err)
	}
	p, err := s.ReadSource("group", large.ID, 1, "")
	if err != nil || len(p.Segments) != 7 || p.Next != 8 {
		t.Fatal(p.Next, err)
	}
	d := scheduleTest(t, s, "read", "lead")
	r, _ := claimTest(t, s, d.Runs[0])
	finishTest(t, s, r, "cancelled", "")
	if _, err = s.ReadSource("group", v.ID, 1, r.ID); err == nil {
		t.Fatal("cancelled read saved")
	}
	if _, err = s.SaveSource(v, parts, r.ID); err == nil {
		t.Fatal("cancelled duplicate save accepted")
	}
}
func TestTeamReadsCanSupportLeadCitation(t *testing.T) {
	s, _, _ := groupFixture(t)
	v := sourceFixture(t, s, strings.Repeat("d", 32), "group")
	d := scheduleTest(t, s, "依据资料评审", "lead")
	r, _ := claimTest(t, s, d.Runs[0])
	step := pendingStep("b", "")
	out, err := s.AdvanceLead(r.ID, step, "invite", "member")
	if err != nil {
		t.Fatal(err)
	}
	finishTest(t, s, r, "completed", out.Step.PublicText())
	member, _ := s.QueuedRun()
	member, _ = claimTest(t, s, member)
	if _, err = s.ReadSource("group", v.ID, 1, member.ID); err != nil {
		t.Fatal(err)
	}
	finishTest(t, s, member, "completed", "资料确认12人100元")
	lead, _ := s.QueuedRun()
	lead, _ = claimTest(t, s, lead)
	step.Action = "complete"
	step.NextAgentID = ""
	step.Task = ""
	step.Result = "30分钟，各环节可跳过。[依据](source://" + v.ID + "/1)"
	step.Citations = []Citation{{SourceID: v.ID, Segment: 1, Quote: "预算100元"}}
	for i := range step.Checks {
		step.Checks[i].Status = "met"
		step.Checks[i].Evidence = "已检查"
	}
	if _, err = s.AdvanceLead(lead.ID, step, "final", "child"); err != nil {
		t.Fatal("lead could not cite team-read source", err)
	}
}
