package store

import (
	"database/sql"
	"path/filepath"
	"testing"
)

func addAgent(t *testing.T, s *Store, id string) Agent {
	t.Helper()
	a, err := s.SaveAgent(Agent{ID: id, Name: "同名助手", Instruction: "协助写作", BaseURL: "https://example.test/v1", Model: "test", KeyRef: "key-ref", Enabled: true, Tools: []string{"count_characters"}})
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestMigrateP0AndPersistWorkspace(t *testing.T) {
	dir := t.TempDir()
	old, err := sql.Open("sqlite", filepath.Join(dir, "tongxi.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = old.Exec(migrations[0] + `PRAGMA user_version=1; INSERT INTO model_settings VALUES(1,'https://example.test/v1','test','existing-ref'); INSERT INTO probe_runs VALUES('old','local','hello','completed','ok','','[]','','',1);`); err != nil {
		t.Fatal(err)
	}
	old.Close()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	a := addAgent(t, s, "a")
	b := addAgent(t, s, "b")
	c, err := s.SaveConversation(Conversation{ID: "group", Title: "写作讨论", Kind: "group", Mode: "lead", LeadAgentID: a.ID, MemberIDs: []string{a.ID, b.ID}}, true)
	if err != nil {
		t.Fatal(err)
	}
	m, err := s.AppendMessage(Message{ID: "m", ConversationID: c.ID, SenderType: "agent", SenderID: a.ID, Content: "初稿"})
	if err != nil {
		t.Fatal(err)
	}
	a.Name = "新的名称"
	a.Instruction = "新的指令"
	if _, err = s.SaveAgent(a); err != nil {
		t.Fatal(err)
	}
	if _, err = s.SetAgentEnabled(a.ID, false); err != nil {
		t.Fatal(err)
	}
	if err = s.CreateConversationRun(ConversationRun{ID: "disabled", ConversationID: c.ID, AgentID: a.ID, MessageID: m.ID}); err == nil {
		t.Fatal("disabled agent accepted a new run")
	}
	if err = s.CreateConversationRun(ConversationRun{ID: "queued", ConversationID: c.ID, AgentID: b.ID, MessageID: m.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.SaveConversation(c, false); err == nil {
		t.Fatal("in-flight conversation allowed changes")
	}
	s.Close()
	s, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	settings, _ := s.Settings()
	if settings.KeyRef != "existing-ref" {
		t.Fatal("P0 configuration lost")
	}
	probes, _ := s.Runs()
	if len(probes) != 1 || probes[0].Text != "ok" {
		t.Fatal("P0 history lost")
	}
	gotA, _ := s.Agent(a.ID)
	gotB, _ := s.Agent(b.ID)
	if gotA.Enabled || gotA.Name != "新的名称" || gotB.Name != b.Name || gotB.Instruction != b.Instruction {
		t.Fatal("agent edit leaked or disable lost")
	}
	gotC, err := s.Conversation(c.ID)
	if err != nil || len(gotC.MemberIDs) != 2 || gotC.MemberIDs[0] != a.ID || gotC.LeadAgentID != a.ID {
		t.Fatalf("membership not preserved: %+v %v", gotC, err)
	}
	messages, err := s.Messages(c.ID)
	if err != nil || len(messages) != 1 || messages[0].SenderName != "同名助手" || messages[0].SenderID != a.ID {
		t.Fatalf("historical identity changed: %+v %v", messages, err)
	}
	runs, err := s.ConversationRuns(c.ID)
	if err != nil || len(runs) != 1 || runs[0].AgentID != b.ID || runs[0].Status != "queued" {
		t.Fatalf("run not preserved: %+v %v", runs, err)
	}
}

func TestConversationValidationAndMessageIsolation(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	addAgent(t, s, "a")
	addAgent(t, s, "b")
	addAgent(t, s, "outsider")
	c := Conversation{ID: "group", Title: "讨论", Kind: "group", Mode: "lead", LeadAgentID: "outsider", MemberIDs: []string{"a", "b"}}
	if _, err = s.SaveConversation(c, true); err == nil {
		t.Fatal("non-member lead accepted")
	}
	c.LeadAgentID = "a"
	c.MemberIDs = []string{"a", "missing"}
	if _, err = s.SaveConversation(c, true); err == nil {
		t.Fatal("missing member accepted")
	}
	list, _ := s.Conversations()
	if len(list) != 0 {
		t.Fatal("invalid conversation partially committed")
	}
	c.MemberIDs = []string{"b", "a"}
	c.Mode = "discussion"
	c.LeadAgentID = ""
	if _, err = s.SaveConversation(c, true); err != nil {
		t.Fatal(err)
	}
	private := Conversation{ID: "private", Title: "私聊", Kind: "private", Mode: "lead", LeadAgentID: "a", MemberIDs: []string{"a"}}
	if _, err = s.SaveConversation(private, true); err != nil {
		t.Fatal(err)
	}
	for _, m := range []Message{{ID: "m1", ConversationID: c.ID, SenderType: "user", SenderID: "outsider", Content: "群内消息"}, {ID: "m2", ConversationID: private.ID, SenderType: "user", Content: "私聊消息"}} {
		if _, err = s.AppendMessage(m); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = s.AppendMessage(Message{ID: "spoof", ConversationID: c.ID, SenderType: "agent", SenderID: "outsider", Content: "不能投递"}); err == nil {
		t.Fatal("non-member posted")
	}
	if err = s.CreateConversationRun(ConversationRun{ID: "cross", ConversationID: c.ID, AgentID: "a", MessageID: "m2"}); err == nil {
		t.Fatal("cross-conversation trigger accepted")
	}
	messages, _ := s.Messages(c.ID)
	if len(messages) != 1 || messages[0].Content != "群内消息" || messages[0].SenderID != "" || messages[0].Sequence != 1 {
		t.Fatal("message isolation or sender binding failed")
	}
	c.Title = "改名"
	c.MemberIDs = []string{"a", "missing"}
	if _, err = s.SaveConversation(c, false); err == nil {
		t.Fatal("invalid edit succeeded")
	}
	got, _ := s.Conversation(c.ID)
	if got.Title != "讨论" || got.MemberIDs[0] != "b" {
		t.Fatal("invalid edit changed conversation")
	}
}
