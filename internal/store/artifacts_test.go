package store

import (
	"fmt"
	"testing"
	"tongxi/internal/scriptrun"
)

func artifactFixture(t *testing.T, s *Store, r ConversationRun, id string, n int) Artifact {
	t.Helper()
	path := "成果/脚本/" + id + "/report.csv"
	if err := s.StartScriptRun(ScriptRun{ID: id, RunID: r.ID, SkillID: "budget", Path: "scripts/run.py"}, r.ConversationID); err != nil {
		t.Fatal(err)
	}
	if err := s.FinishScriptRun(id, scriptrun.Result{Status: "completed", ExitCode: 0, Files: []string{path}}); err != nil {
		t.Fatal(err)
	}
	segments := []SourceSegment{}
	for i := 0; i < n; i++ {
		segments = append(segments, SourceSegment{Number: i + 1, Location: fmt.Sprintf("行%d", i+1), Content: fmt.Sprintf("row%d", i+1)})
	}
	a, err := s.SaveArtifact(Artifact{ID: id, RunID: r.ID, ScriptRunID: id, ConversationID: r.ConversationID, Path: path, Format: "csv"}, []byte("data-"+id), segments, "")
	if err != nil {
		t.Fatal(err)
	}
	return a
}
func TestArtifactDeliveryRequiresActualCompleteReadAndPreservesVersions(t *testing.T) {
	s, _, dir := groupFixture(t)
	testSkill(t, s, "test", true)
	d := scheduleTest(t, s, "artifact-v1", "lead")
	r, _ := claimTest(t, s, d.Runs[0])
	a := artifactFixture(t, s, r, "file-one", 11)
	step := versionStep("100")
	if _, err := s.AdvanceLead(r.ID, step, "m", "next"); err == nil {
		t.Fatal("generated files silently omitted")
	}
	step.Files = []ArtifactDelivery{{ArtifactID: "invented", Evidence: "done"}}
	if _, err := s.AdvanceLead(r.ID, step, "m", "next"); err == nil {
		t.Fatal("invented file accepted")
	}
	step.Files[0].ArtifactID = a.ID
	if _, err := s.AdvanceLead(r.ID, step, "m", "next"); err == nil {
		t.Fatal("unread file accepted")
	}
	// UI preview does not count as model verification, including all pages.
	s.ReadSource("group", a.SourceID, 1, "")
	s.ReadSource("group", a.SourceID, 11, "")
	if _, err := s.AdvanceLead(r.ID, step, "m", "next"); err == nil {
		t.Fatal("UI read counted as verification")
	}
	p, err := s.ReadSource("group", a.SourceID, 1, r.ID)
	if err != nil || p.Next != 11 {
		t.Fatal(p, err)
	}
	if _, err = s.AdvanceLead(r.ID, step, "m", "next"); err == nil {
		t.Fatal("partial file read accepted")
	}
	if _, err = s.ReadSource("group", a.SourceID, p.Next, r.ID); err != nil {
		t.Fatal(err)
	}
	step.Files[0].Evidence = ""
	if _, err = s.AdvanceLead(r.ID, step, "m", "next"); err == nil {
		t.Fatal("missing evidence accepted")
	}
	step.Files[0].Evidence = "全部11行已核对"
	out, err := s.AdvanceLead(r.ID, step, "m", "next")
	if err != nil {
		t.Fatal(err)
	}
	finishTest(t, s, r, "completed", out.Step.PublicText())
	versions, _ := s.WorkVersions("group")
	if len(versions) != 1 || versions[0].Step.Files[0].ArtifactID != a.ID {
		t.Fatal(versions)
	}
	d2 := scheduleTest(t, s, "artifact-v2", "lead")
	r2, _ := claimTest(t, s, d2.Runs[0])
	step.WorkMode = "continue"
	if _, err = s.AdvanceLead(r2.ID, step, "m2", "next2"); err == nil {
		t.Fatal("old verification silently reused")
	}
	a2 := artifactFixture(t, s, r2, "file-two", 1)
	s.ReadSource("group", a2.SourceID, 1, r2.ID)
	step.Files = []ArtifactDelivery{{ArtifactID: a2.ID, Evidence: "新表已核对"}}
	out, err = s.AdvanceLead(r2.ID, step, "m2", "next2")
	if err != nil {
		t.Fatal(err)
	}
	finishTest(t, s, r2, "completed", out.Step.PublicText())
	versions, _ = s.WorkVersions("group")
	if len(versions) != 2 || versions[0].Step.Files[0].ArtifactID != a.ID || versions[1].Step.Files[0].ArtifactID != a2.ID {
		t.Fatal(versions)
	}
	// A new topic cannot present another task's files as its own.
	d3 := scheduleTest(t, s, "another-task", "lead")
	r3, _ := claimTest(t, s, d3.Runs[0])
	step.WorkMode = "new"
	if _, err = s.AdvanceLead(r3.ID, step, "m3", "next3"); err == nil {
		t.Fatal("unrelated task file accepted")
	}
	if _, err = s.Artifact("private", a.ID); err == nil {
		t.Fatal("cross conversation metadata")
	}
	if _, err = s.ArtifactData("private", a.ID); err == nil {
		t.Fatal("cross conversation bytes")
	}
	if _, err = s.SaveArtifact(Artifact{ID: "late", RunID: r.ID, ScriptRunID: a.ScriptRunID, ConversationID: "group", Path: a.Path}, []byte("late"), nil, ""); err == nil {
		t.Fatal("late capture accepted")
	}
	s.Close()
	s, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	bytes, err := s.ArtifactData("group", a.ID)
	if err != nil || string(bytes) != "data-file-one" {
		t.Fatal(string(bytes), err)
	}
	versions, _ = s.WorkVersions("group")
	if len(versions) != 2 || versions[0].Step.Files[0].ArtifactID != a.ID {
		t.Fatal("history lost", versions)
	}
}
func TestArtifactRegistrationRollsBackAndRejectsUnlistedFiles(t *testing.T) {
	s, _, _ := groupFixture(t)
	testSkill(t, s, "test", true)
	d := scheduleTest(t, s, "save-artifact", "lead")
	r, _ := claimTest(t, s, d.Runs[0])
	a := artifactFixture(t, s, r, "existing", 1)
	before, _ := s.Sources("group")
	_, err := s.SaveArtifact(Artifact{ID: "bad", RunID: r.ID, ScriptRunID: a.ScriptRunID, ConversationID: "group", Path: "other.csv"}, []byte("x"), []SourceSegment{{Content: "x"}}, "")
	if err == nil {
		t.Fatal("unlisted output accepted")
	}
	_, err = s.SaveArtifact(Artifact{ID: "duplicate", RunID: r.ID, ScriptRunID: a.ScriptRunID, ConversationID: "group", Path: a.Path}, []byte("different"), []SourceSegment{{Content: "different"}}, "")
	if err == nil {
		t.Fatal("duplicate capture rewrote artifact")
	}
	after, _ := s.Sources("group")
	if len(before) != len(after) {
		t.Fatal("orphan source leaked from rollback")
	}
}
