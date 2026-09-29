package store

import (
	"testing"
	"tongxi/internal/scriptrun"
)

func TestScriptExecutionBudgetAndRestartRecords(t *testing.T) {
	s, _, dir := groupFixture(t)
	testSkill(t, s, "test", true)
	bindSkill(t, s, "a", "budget")
	d := scheduleTest(t, s, "script", "round", "a")
	r, _ := claimTest(t, s, d.Runs[0])
	for _, id := range []string{"one", "two", "three"} {
		if err := s.StartScriptRun(ScriptRun{ID: id, RunID: r.ID, SkillID: "budget", Name: "budget-check", Path: "scripts/run.sh", Args: []string{}}, "group"); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.StartScriptRun(ScriptRun{ID: "four", RunID: r.ID, SkillID: "budget"}, "group"); err == nil {
		t.Fatal("execution budget bypassed")
	}
	if err := s.FinishScriptRun("one", scriptrun.Result{Status: "completed", Stdout: "done", ExitCode: 0, Files: []string{"成果/result.csv"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.StopChain(d.ChainID, &r); err != nil {
		t.Fatal(err)
	}
	if err := s.StartScriptRun(ScriptRun{ID: "late", RunID: r.ID, SkillID: "budget"}, "group"); err == nil {
		t.Fatal("stopped execution accepted")
	}
	s.Close()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	rows, err := s.ScriptRuns("group")
	if err != nil || len(rows) != 3 || rows[0].Status != "completed" || rows[0].Stdout != "done" || rows[1].Status != "interrupted" || rows[2].Status != "interrupted" {
		t.Fatal(rows, err)
	}
	other, err := s.ScriptRuns("other")
	if err != nil || len(other) != 0 {
		t.Fatal("execution history crossed conversation", other, err)
	}
}
