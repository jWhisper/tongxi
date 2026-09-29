package store

import (
	"database/sql"
	"path/filepath"
	"testing"
	"tongxi/internal/skill"
)

func testSkill(t *testing.T, s *Store, content string, enabled bool) SkillContent {
	t.Helper()
	current, _ := s.SkillContent("budget")
	v, err := s.SaveSkill("budget", current.UpdatedAt, enabled, skill.Bundle{Content: "---\nname: budget-check\ndescription: 检查预算\n---\n" + content, Resources: []skill.Resource{{Path: "references/check.md", Content: content}}})
	if err != nil {
		t.Fatal(err)
	}
	return v
}
func bindSkill(t *testing.T, s *Store, id string, ids ...string) {
	t.Helper()
	a, err := s.Agent(id)
	if err != nil {
		t.Fatal(err)
	}
	a.SkillIDs = ids
	if _, err = s.SaveAgent(a); err != nil {
		t.Fatal(err)
	}
}
func TestSkillCurrentContentBindingsRollbackAndRestart(t *testing.T) {
	s, _, dir := groupFixture(t)
	old := testSkill(t, s, "旧内容", true)
	bindSkill(t, s, "a", "budget")
	a, _ := s.Agent("a")
	changed := a
	changed.Name = "must roll back"
	changed.SkillIDs = []string{"missing"}
	if _, err := s.SaveAgent(changed); err == nil {
		t.Fatal("invalid binding accepted")
	}
	got, _ := s.Agent("a")
	if got.Name != a.Name || got.Version != a.Version || len(got.SkillIDs) != 1 {
		t.Fatal("partial agent update", got)
	}
	current := testSkill(t, s, "最新内容", true)
	if _, err := s.SaveSkill("budget", old.UpdatedAt, true, skill.Bundle{Content: old.Content}); err == nil {
		t.Fatal("stale update accepted")
	}
	if _, err := s.SaveSkill("duplicate", "", true, skill.Bundle{Content: old.Content}); err == nil {
		t.Fatal("duplicate name accepted")
	}
	s.Close()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	content, err := s.SkillContent("budget")
	if err != nil || content.Content != current.Content || content.Resources[0].Content != "最新内容" {
		t.Fatal(content, err)
	}
	list, err := s.Skills()
	if err != nil || len(list) != 1 || len(list[0].AgentNames) != 1 {
		t.Fatal(list, err)
	}
}
func TestSkillLoadsLatestWithinSameRunAndTeam(t *testing.T) {
	s, _, _ := groupFixture(t)
	old := testSkill(t, s, "旧内容", true)
	bindSkill(t, s, "a", "budget")
	bindSkill(t, s, "b", "budget")
	d := scheduleTest(t, s, "skills", "round", "a", "b", "c")
	list, err := s.AvailableSkills(d.Runs[0].ID)
	if err != nil || len(list) != 2 {
		t.Fatal(list, err)
	}
	r, _ := claimTest(t, s, d.Runs[0])
	if _, err = s.LoadSkill(r.ID, "budget", true); err == nil {
		t.Fatal("read resource before load")
	}
	first, err := s.LoadSkill(r.ID, "budget", false)
	if err != nil || first.Content != old.Content {
		t.Fatal(first, err)
	}
	latest := testSkill(t, s, "立即生效", true)
	for _, resource := range []bool{false, true} {
		v, e := s.LoadSkill(r.ID, "budget", resource)
		if e != nil || v.Content != latest.Content || v.Resources[0].Content != "立即生效" {
			t.Fatal("same run reused old content", v, e)
		}
	}
	testSkill(t, s, "停用", false)
	if _, err = s.LoadSkill(r.ID, "budget", false); err == nil {
		t.Fatal("disabled skill loaded")
	}
	list, err = s.AvailableSkills(r.ID)
	if err != nil || len(list) != 0 {
		t.Fatal("disabled catalog", list, err)
	}
	latest = testSkill(t, s, "重新启用", true)
	list, err = s.AvailableSkills(r.ID)
	if err != nil || len(list) != 2 {
		t.Fatal("catalog frozen", list, err)
	}
	finishTest(t, s, r, "completed", "done")
	if _, err = s.LoadSkill(r.ID, "budget", false); err == nil {
		t.Fatal("late load accepted")
	}
	member, _ := claimTest(t, s, d.Runs[1])
	v, err := s.LoadSkill(member.ID, "budget", false)
	if err != nil || v.Content != latest.Content {
		t.Fatal("team reused old content", v, err)
	}
	bindSkill(t, s, "b")
	if _, err = s.LoadSkill(member.ID, "budget", true); err == nil {
		t.Fatal("removed binding still works")
	}
	finishTest(t, s, member, "completed", "done")
	other, _ := claimTest(t, s, d.Runs[2])
	if _, err = s.LoadSkill(other.ID, "budget", false); err == nil {
		t.Fatal("another role borrowed skill")
	}
	if _, err = s.StopChain(d.ChainID, &other); err != nil {
		t.Fatal(err)
	}
	if _, err = s.LoadSkill(other.ID, "budget", false); err == nil {
		t.Fatal("stopped read accepted")
	}
	uses, err := s.SkillUses("group")
	if err != nil || len(uses) != 2 || uses[0].Name != "budget-check" {
		t.Fatal("actual use dedup failed", uses, err)
	}
}
func TestSchema12SkillsMigrateToCurrentContent(t *testing.T) {
	dir := t.TempDir()
	db, err := sql.Open("sqlite", filepath.Join(dir, "tongxi.db"))
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range migrations[:12] {
		if _, err = db.Exec(m); err != nil {
			t.Fatal(err)
		}
	}
	_, err = db.Exec(`PRAGMA user_version=12;
 INSERT INTO model_configs(id,name,provider,base_url,model,key_ref,version,created_at,updated_at) VALUES('model','test','compatible','https://example.test','model','test-key-ref',1,'','');
 INSERT INTO agents VALUES('a','a','','instruction','model','[]',1,1,'','');
 INSERT INTO conversations(id,title,kind,mode,lead_agent_id,created_at,updated_at) VALUES('c','old','private','lead','a','','');
 INSERT INTO messages(id,conversation_id,sequence,sender_type,sender_name,content,created_at) VALUES('m','c',1,'user','你','request','');
 INSERT INTO runs(id,conversation_id,agent_id,message_id,status,error,created_at) VALUES('r','c','a','m','completed','','');
 INSERT INTO skills VALUES('s','new-name',1,2,'created','updated');
 INSERT INTO skill_versions VALUES('s',1,'old-name','old description','old content','[]','','old');
 INSERT INTO skill_versions VALUES('s',2,'new-name','new description','latest content','[{"path":"ref.md","content":"latest reference"}]','note','new');
 INSERT INTO agent_skills VALUES('a','s');
 INSERT INTO skill_scopes VALUES('r');
 INSERT INTO skill_scope_bindings VALUES('r','a','s',1);
 INSERT INTO skill_uses VALUES('r','s',1,'used');`)
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	v, err := s.SkillContent("s")
	if err != nil || v.Name != "new-name" || v.Content != "latest content" || v.Resources[0].Content != "latest reference" {
		t.Fatal(v, err)
	}
	uses, err := s.SkillUses("c")
	if err != nil || len(uses) != 1 || uses[0].Name != "old-name" || uses[0].CreatedAt != "used" {
		t.Fatal("old use lost", uses, err)
	}
	a, err := s.Agent("a")
	if err != nil || len(a.SkillIDs) != 1 || a.SkillIDs[0] != "s" {
		t.Fatal("binding lost", a, err)
	}
	var legacy int
	if err = s.db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE name IN ('skill_versions','skill_scopes','skill_scope_bindings')`).Scan(&legacy); err != nil || legacy != 0 {
		t.Fatal("version tables retained", legacy, err)
	}
	if _, err = s.SaveSkill("s", v.UpdatedAt, true, skill.Bundle{Content: "---\nname: new-name\ndescription: 检查\n---\n编辑后"}); err != nil {
		t.Fatal(err)
	}
}
