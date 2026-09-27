package app

import (
	"encoding/json"
	"strings"
	"testing"
	"tongxi/internal/store"
)

func TestAgentCredentialsRemainIndependentAndPrivate(t *testing.T) {
	s := newService(t)
	if _, err := s.SaveSettings(SettingsInput{BaseURL: "https://example.test/v1", Model: "test", APIKey: "shared-secret"}); err != nil {
		t.Fatal(err)
	}
	create := AgentInput{Name: "助手", Instruction: "帮助写作", BaseURL: "https://example.test/v1", Model: "test", Tools: []string{"count_characters"}}
	a, err := s.SaveAgent(create)
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.SaveAgent(create)
	if err != nil {
		t.Fatal(err)
	}
	if a.ID == b.ID || a.KeyRef != b.KeyRef {
		t.Fatal("same-name identity or shared credential wrong")
	}
	if _, err = s.SaveSettings(SettingsInput{BaseURL: "https://example.test/v1", Model: "test", APIKey: "new-default-secret"}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.vault.Get(a.KeyRef); err != nil {
		t.Fatal("rotating default deleted role credential")
	}
	edit := create
	edit.ID = a.ID
	edit.Version = a.Version
	edit.Name = "编辑后"
	edit.APIKey = "agent-a-secret"
	updated, err := s.SaveAgent(edit)
	if err != nil {
		t.Fatal(err)
	}
	if updated.KeyRef == b.KeyRef {
		t.Fatal("role credentials were not isolated")
	}
	if _, err = s.vault.Get(b.KeyRef); err != nil {
		t.Fatal("editing one role deleted another role credential")
	}
	workspace, err := s.Workspace()
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(workspace)
	for _, secret := range []string{"shared-secret", "new-default-secret", "agent-a-secret", a.KeyRef, updated.KeyRef} {
		if strings.Contains(string(data), secret) {
			t.Fatal("workspace returned credential or reference")
		}
	}
	edit.APIKey = ""
	edit.Version = updated.Version
	edit.BaseURL = "https://other.test/v1"
	if _, err = s.SaveAgent(edit); err == nil {
		t.Fatal("endpoint change reused credential")
	}
	edit.BaseURL = create.BaseURL
	edit.Version = a.Version
	if _, err = s.SaveAgent(edit); err == nil {
		t.Fatal("stale edit overwritten")
	}
}

func TestWorkspaceFailurePreservesSavedData(t *testing.T) {
	s := newService(t)
	_, err := s.SaveAgent(AgentInput{Name: "助手", Instruction: "指令", BaseURL: "https://example.test/v1", Model: "test"})
	if err == nil || !strings.Contains(err.Error(), "API Key") {
		t.Fatal("missing key not explained")
	}
	if _, err = s.SaveSettings(SettingsInput{BaseURL: "https://example.test/v1", Model: "test", APIKey: "secret"}); err != nil {
		t.Fatal(err)
	}
	a, err := s.SaveAgent(AgentInput{Name: "助手", Instruction: "指令", BaseURL: "https://example.test/v1", Model: "test"})
	if err != nil {
		t.Fatal(err)
	}
	c, err := s.SaveConversation(store.Conversation{Title: "私聊", Kind: "private", Mode: "lead", LeadAgentID: a.ID, MemberIDs: []string{a.ID}})
	if err != nil {
		t.Fatal(err)
	}
	m, err := s.PostMessage(c.ID, "已保存的消息")
	if err != nil {
		t.Fatal(err)
	}
	if m.SenderType != "user" || m.SenderName != "你" {
		t.Fatal("public user identity wrong")
	}
	detail, err := s.Conversation(c.ID)
	if err != nil || len(detail.Messages) != 1 || len(detail.Runs) != 0 {
		t.Fatal("P1 publish unexpectedly scheduled execution")
	}
	// A closed database must produce a storage error instead of a configuration error.
	if _, err = s.db.SetAgentEnabled(a.ID, false); err != nil {
		t.Fatal(err)
	}
	s.db.Close()
	_, err = s.SaveAgent(AgentInput{Name: "未保存", Instruction: "指令", BaseURL: "https://example.test/v1", Model: "test"})
	if err == nil || !strings.Contains(err.Error(), "本地数据读写失败") {
		t.Fatalf("storage failure not distinguished: %v", err)
	}
}
