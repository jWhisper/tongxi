package store

import (
	"testing"
)

func TestReopenPreservesDataAndInterruptsUnfinishedRun(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	v := Settings{BaseURL: "https://example.test/v1", Model: "test", KeyRef: "reference-only"}
	if err = s.SaveSettings(v); err != nil {
		t.Fatal(err)
	}
	if err = s.InsertRun(ProbeRun{ID: "run", Source: "local", Prompt: "hello", Status: "running", Tools: []string{}, Revision: 1}); err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	got, err := s.Settings()
	if err != nil || got != v {
		t.Fatalf("settings=%+v err=%v", got, err)
	}
	runs, err := s.Runs()
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 || runs[0].Status != "interrupted" || runs[0].Revision != 2 {
		t.Fatalf("unexpected recovered runs: %+v", runs)
	}
	late := runs[0]
	late.Status = "completed"
	if err = s.FinishRun(late); err != nil {
		t.Fatal(err)
	}
	runs, _ = s.Runs()
	if runs[0].Status != "interrupted" {
		t.Fatal("late completion overwrote interrupted status")
	}
}
