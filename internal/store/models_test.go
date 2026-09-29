package store

import (
	"database/sql"
	"path/filepath"
	"testing"
)

func TestModelMigrationPreservesDistinctConnectionsAndSharedReferences(t *testing.T) {
	dir := t.TempDir()
	old, err := sql.Open("sqlite", filepath.Join(dir, "tongxi.db"))
	if err != nil {
		t.Fatal(err)
	}
	for _, migration := range migrations[:7] {
		if _, err = old.Exec(migration); err != nil {
			t.Fatal(err)
		}
	}
	_, err = old.Exec(`PRAGMA user_version=7;
 INSERT INTO model_settings VALUES(1,'https://api.kimi.com/coding/v1','kimi-for-coding','shared-ref');
 INSERT INTO agents VALUES('a','writer','','write','https://api.kimi.com/coding/v1','kimi-for-coding','shared-ref','[]',1,3,'old','old');
 INSERT INTO agents VALUES('b','reviewer','','review','https://api.kimi.com/coding/v1','kimi-for-coding','shared-ref','[]',0,2,'old','old');
 INSERT INTO agents VALUES('c','other','','other','https://api.kimi.com/coding/v1','kimi-for-coding','different-ref','[]',1,1,'old','old');
 INSERT INTO conversations VALUES('room','saved','private','lead','a','old','old',1);
 INSERT INTO conversation_members VALUES('room','a',0);
 INSERT INTO messages(id,conversation_id,sequence,sender_type,sender_id,sender_name,content,created_at) VALUES('message','room',1,'agent','a','writer','saved reply','old');`)
	if err != nil {
		t.Fatal(err)
	}
	old.Close()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	models, err := s.Models()
	if err != nil || len(models) != 2 {
		t.Fatal(models, err)
	}
	a, _ := s.Agent("a")
	b, _ := s.Agent("b")
	c, _ := s.Agent("c")
	if a.ModelID != b.ModelID || a.ModelID == c.ModelID || a.KeyRef != "shared-ref" || c.KeyRef != "different-ref" || a.Version != 3 || b.Enabled {
		t.Fatal("migration changed model identity or role", a, b, c)
	}
	m, _ := s.Model(a.ModelID)
	if m.AgentCount != 2 || m.Provider != "kimi-code" {
		t.Fatal(m)
	}
	messages, _ := s.Messages("room")
	if len(messages) != 1 || messages[0].Content != "saved reply" {
		t.Fatal("lost history", messages)
	}
	var n int
	if err = s.db.QueryRow(`SELECT count(*) FROM pragma_foreign_key_check`).Scan(&n); err != nil || n != 0 {
		t.Fatal(n, err)
	}
	if _, err = s.db.Exec(`UPDATE agents SET model_id='missing' WHERE id='a'`); err == nil {
		t.Fatal("foreign key not enforced")
	}
}

func TestModelUpdatesResolveAtRunTimeAndReferencedDeletionIsBlocked(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	a := addAgent(t, s, "a")
	m, _ := s.Model(a.ModelID)
	if err = s.DeleteModel(m.ID, m.Version); err == nil {
		t.Fatal("in-use model deleted")
	}
	m.Model = "changed"
	m.KeyRef = "rotated"
	m.Name = "Shared model"
	updated, err := s.SaveModel(m)
	if err != nil {
		t.Fatal(err)
	}
	a, err = s.Agent(a.ID)
	if err != nil || a.Model != "changed" || a.KeyRef != "rotated" || a.ModelName != "Shared model" {
		t.Fatal(a, err)
	}
	if _, err = s.SaveModel(m); err == nil {
		t.Fatal("stale model update accepted")
	}
	spare, err := s.SaveModel(ModelConfig{ID: "spare", Name: "spare", Provider: "compatible", BaseURL: "https://example.test/v1", Model: "test", KeyRef: "spare-key"})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.DeleteModel(spare.ID, spare.Version); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Model(updated.ID); err != nil {
		t.Fatal("unrelated model lost", err)
	}
}
