package store

import (
	"database/sql"
	"strings"
	"testing"
)

func TestWorkDirectoryIsImmutableAtStoreBoundary(t *testing.T) {
	s, c, dir := groupFixture(t)
	c.WorkDir = "/chosen/directory"
	c, err := s.SaveConversation(c, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"", "/another/directory"} {
		changed := c
		changed.WorkDir = path
		if _, err = s.SaveConversation(changed, false); err == nil {
			t.Fatal("directory changed")
		}
	}
	if _, err = s.db.Exec(`UPDATE conversations SET work_dir='' WHERE id=?`, c.ID); err == nil {
		t.Fatal("database bypass")
	}
	c.Title = "renamed"
	if _, err = s.SaveConversation(c, false); err != nil {
		t.Fatal(err)
	}
	got, err := s.Conversation(c.ID)
	if err != nil || got.WorkDir != c.WorkDir {
		t.Fatal(got, err)
	}
	s.Close()
	reopened, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	got, err = reopened.Conversation(c.ID)
	if err != nil || got.WorkDir != c.WorkDir {
		t.Fatal("directory lost after restart", got, err)
	}
}

func TestWorkspaceReferencesStoreOnlyMetadataAndReadPassages(t *testing.T) {
	s, _, _ := groupFixture(t)
	d := scheduleTest(t, s, "files", "lead")
	r, _ := claimTest(t, s, d.Runs[0])
	v := Source{ID: "file-reference", ConversationID: "group", Name: "预算.txt", Format: "txt", Segments: 1, Size: 20}
	first, err := s.SaveWorkspaceFile(v, r.ID)
	if err != nil {
		t.Fatal(err)
	}
	v.ID, v.Size = "another-id", 30
	latest, err := s.SaveWorkspaceFile(v, r.ID)
	if err != nil || latest.ID != first.ID || latest.Size != 30 {
		t.Fatal(latest, err)
	}
	var bodies int
	if err = s.db.QueryRow(`SELECT count(*) FROM source_segments`).Scan(&bodies); err != nil || bodies != 0 {
		t.Fatal("stored file snapshot", bodies, err)
	}
	page := SourcePage{Source: latest, Segments: []SourceSegment{{Number: 1, Location: "第1行", Content: "预算100元"}}}
	if err = s.RecordSourceRead(page, ""); err != nil {
		t.Fatal(err)
	}
	step := versionStep("100")
	step.Citations = []Citation{{SourceID: first.ID, Segment: 1, Quote: "预算100元", Name: "forged", Location: "forged"}}
	if _, err = s.AdvanceLead(r.ID, step, "unread", "child"); err == nil {
		t.Fatal("UI preview counted as evidence")
	}
	if err = s.RecordSourceRead(page, r.ID); err != nil {
		t.Fatal(err)
	}
	page.Segments[0].Content = "预算200元"
	if err = s.RecordSourceRead(page, r.ID); err != nil {
		t.Fatal(err)
	}
	out, err := s.AdvanceLead(r.ID, step, "old-quote", "child")
	if err != nil || out.Step.Citations[0].Location != "第1行" || out.Step.Citations[0].Name != "预算.txt" {
		t.Fatal(out, err)
	}
	finishTest(t, s, r, "completed", out.Step.PublicText())
	if err = s.RecordSourceRead(page, r.ID); err == nil {
		t.Fatal("stopped run saved a read")
	}
	if _, err = s.Source("one", latest.ID); err == nil {
		t.Fatal("cross-conversation source")
	}
}

func TestMessageAttachmentsPersistScopeAndIdempotency(t *testing.T) {
	s, _, dir := groupFixture(t)
	v, err := s.SaveWorkspaceFile(Source{ID: "file", ConversationID: "group", Name: "预算.csv", Format: "csv"}, "")
	if err != nil {
		t.Fatal(err)
	}
	in := ScheduleRequest{RequestID: "attach", ConversationID: "group", Action: "mention", AgentIDs: []string{"b"}, AttachmentIDs: []string{v.ID}}
	d, err := s.Schedule(in, "m", "chain", []string{"r"})
	if err != nil || len(d.Runs) != 1 || d.Runs[0].AgentID != "b" {
		t.Fatal(d, err)
	}
	replay, err := s.Schedule(in, "unused", "unused", []string{"unused"})
	if err != nil || replay.MessageID != "m" {
		t.Fatal(replay, err)
	}
	in.AttachmentIDs = nil
	in.Content = "different"
	if _, err = s.Schedule(in, "unused", "unused", nil); err == nil {
		t.Fatal("idempotency ignored attachments")
	}
	for _, ids := range [][]string{{v.ID, v.ID}, {"missing"}} {
		in.RequestID, in.AttachmentIDs = "invalid", ids
		if _, err = s.Schedule(in, "invalid", "invalid", []string{"invalid"}); err == nil {
			t.Fatal("invalid attachments accepted")
		}
	}
	in.RequestID, in.ConversationID, in.Action, in.AgentIDs, in.AttachmentIDs = "cross", "one", "direct", nil, []string{v.ID}
	if _, err = s.Schedule(in, "invalid", "invalid", []string{"invalid"}); err == nil {
		t.Fatal("cross-conversation attachment")
	}
	r, input := claimTest(t, s, d.Runs[0])
	if !strings.Contains(input, "预算.csv") || !strings.Contains(input, "attachments") {
		t.Fatal("attachment absent from model context", input)
	}
	finishTest(t, s, r, "completed", "收到附件")
	s.Close()
	s, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	messages, err := s.Messages("group")
	if err != nil || len(messages) != 2 || len(messages[0].Attachments) != 1 || messages[0].Attachments[0].Name != "预算.csv" {
		t.Fatal(messages, err)
	}
}

func TestMentionInterruptsAutomaticWorkAndDoesNotChangeMode(t *testing.T) {
	for _, mode := range []string{"lead", "discussion"} {
		t.Run(mode, func(t *testing.T) {
			s, c, _ := groupFixture(t)
			c.Mode = mode
			if mode == "discussion" {
				c.LeadAgentID = ""
			}
			if _, err := s.SaveConversation(c, false); err != nil {
				t.Fatal(err)
			}
			old := scheduleTest(t, s, "auto", mode)
			mentioned := scheduleTest(t, s, "point", "mention", "b")
			prior, _ := s.ConversationRun(old.Runs[0].ID)
			if prior.Status != "cancelled" {
				t.Fatal("automatic work not stopped", prior)
			}
			r, _ := claimTest(t, s, mentioned.Runs[0])
			finishTest(t, s, r, "completed", "one reply")
			if _, err := s.QueuedRun(); err != sql.ErrNoRows {
				t.Fatal("unexpected automatic continuation", err)
			}
			after, _ := s.Conversation(c.ID)
			if after.Mode != mode {
				t.Fatal("mention changed conversation mode")
			}
			next := scheduleTest(t, s, "next", mode)
			chain, _ := s.RunChain(next.Runs[0].ID)
			if chain.Action != mode {
				t.Fatal(chain)
			}
		})
	}
}

func TestMentionRetryKeepsRecipientAndAttachments(t *testing.T) {
	s, _, _ := groupFixture(t)
	v, err := s.SaveWorkspaceFile(Source{ID: "file", ConversationID: "group", Name: "input.txt", Format: "txt"}, "")
	if err != nil {
		t.Fatal(err)
	}
	d, err := s.Schedule(ScheduleRequest{RequestID: "mention-retry", ConversationID: "group", Action: "mention", AgentIDs: []string{"b"}, Content: "read", AttachmentIDs: []string{v.ID}}, "m", "ch", []string{"r"})
	if err != nil {
		t.Fatal(err)
	}
	r, _ := claimTest(t, s, d.Runs[0])
	finishTest(t, s, r, "failed", "")
	retry, err := s.RetryRun(r.ID, "retry-request", "retry-run")
	if err != nil || retry.AgentID != "b" {
		t.Fatal(retry, err)
	}
	retry, input := claimTest(t, s, retry)
	if !strings.Contains(input, "input.txt") {
		t.Fatal("retry lost attachment")
	}
	finishTest(t, s, retry, "completed", "done")
	if _, err = s.QueuedRun(); err != sql.ErrNoRows {
		t.Fatal("retry started another agent", err)
	}
}

func TestPrivateAttachmentOnlyMessageIncludesFileInInput(t *testing.T) {
	s, _, _ := groupFixture(t)
	v, err := s.SaveWorkspaceFile(Source{ID: "private-file", ConversationID: "one", Name: "input.txt", Format: "txt"}, "")
	if err != nil {
		t.Fatal(err)
	}
	d, err := s.Schedule(ScheduleRequest{RequestID: "private-attachment", ConversationID: "one", Action: "direct", AttachmentIDs: []string{v.ID}}, "m", "ch", []string{"r"})
	if err != nil {
		t.Fatal(err)
	}
	_, input := claimTest(t, s, d.Runs[0])
	if !strings.Contains(input, "input.txt") || !strings.Contains(input, "private-file") {
		t.Fatal("private input lost attachment", input)
	}
}
