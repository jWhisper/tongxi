package app

import (
	"encoding/json"
	"strings"
	"testing"
	"tongxi/internal/store"
)

func saveTestModel(t *testing.T, s *Service, name, key string) store.ModelConfig {
	t.Helper()
	m, err := s.SaveModel(ModelInput{Name: name, Provider: "compatible", BaseURL: "https://example.test/v1", Model: name, APIKey: key})
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestAgentsSelectSharedModelsWithoutCredentials(t *testing.T) {
	s := newService(t)
	m := saveTestModel(t, s, "shared", "shared-secret")
	other := saveTestModel(t, s, "other", "other-secret")
	create := AgentInput{Name: "助手", Instruction: "帮助写作", ModelID: m.ID, Tools: []string{"count_characters"}}
	a, err := s.SaveAgent(create)
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.SaveAgent(create)
	if err != nil {
		t.Fatal(err)
	}
	if a.ID == b.ID || a.KeyRef != b.KeyRef {
		t.Fatal("shared model not resolved")
	}
	edit := create
	edit.ID, edit.Version, edit.ModelID = a.ID, a.Version, other.ID
	updated, err := s.SaveAgent(edit)
	if err != nil {
		t.Fatal(err)
	}
	unchanged, _ := s.db.Agent(b.ID)
	if updated.ModelID != other.ID || unchanged.ModelID != m.ID {
		t.Fatal("role model selection leaked")
	}
	workspace, err := s.Workspace()
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(workspace)
	for _, secret := range []string{"shared-secret", "other-secret", a.KeyRef, updated.KeyRef} {
		if strings.Contains(string(data), secret) {
			t.Fatal("workspace returned credential or reference")
		}
	}
	if _, err = s.SaveAgent(edit); err == nil {
		t.Fatal("stale role overwritten")
	}
	edit.Version = updated.Version
	edit.ModelID = "missing"
	if _, err = s.SaveAgent(edit); err == nil {
		t.Fatal("unknown model accepted")
	}
}

func TestWorkspaceFailurePreservesSavedData(t *testing.T) {
	s := newService(t)
	_, err := s.SaveAgent(AgentInput{Name: "助手", Instruction: "指令", ModelID: "missing"})
	if err == nil || !strings.Contains(err.Error(), "模型配置不存在") {
		t.Fatal("missing key not explained")
	}
	config := saveTestModel(t, s, "test", "secret")
	a, err := s.SaveAgent(AgentInput{Name: "助手", Instruction: "指令", ModelID: config.ID})
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
	_, err = s.SaveAgent(AgentInput{Name: "未保存", Instruction: "指令", ModelID: "missing"})
	if err == nil || !strings.Contains(err.Error(), "本地数据读写失败") {
		t.Fatalf("storage failure not distinguished: %v", err)
	}
}
