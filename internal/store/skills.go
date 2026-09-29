package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"tongxi/internal/skill"
)

type Skill struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Enabled     bool     `json:"enabled"`
	UpdatedAt   string   `json:"updatedAt"`
	AgentNames  []string `json:"agentNames"`
}
type SkillContent struct {
	ID          string           `json:"id"`
	Name        string           `json:"name"`
	Description string           `json:"description"`
	Enabled     bool             `json:"enabled"`
	Content     string           `json:"content"`
	Resources   []skill.Resource `json:"resources"`
	Scripts     []skill.Resource `json:"scripts"`
	Note        string           `json:"note"`
	UpdatedAt   string           `json:"updatedAt"`
}
type SkillBinding struct {
	AgentID     string `json:"agentID"`
	SkillID     string `json:"skillID"`
	Name        string `json:"name"`
	Description string `json:"description"`
}
type SkillUse struct {
	RunID     string `json:"runID"`
	SkillID   string `json:"skillID"`
	Name      string `json:"name"`
	CreatedAt string `json:"createdAt"`
}

func (s *Store) Skills() ([]Skill, error) {
	rows, err := s.db.Query(`SELECT s.id,s.name,s.description,s.enabled,s.updated_at,
  (SELECT json_group_array(a.name) FROM agent_skills b JOIN agents a ON a.id=b.agent_id WHERE b.skill_id=s.id)
  FROM skills s ORDER BY s.created_at,s.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Skill{}
	for rows.Next() {
		var v Skill
		var names string
		if err = rows.Scan(&v.ID, &v.Name, &v.Description, &v.Enabled, &v.UpdatedAt, &names); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(names), &v.AgentNames); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

const skillColumns = `id,name,description,enabled,content,resources,scripts,note,updated_at`

func scanSkill(row interface{ Scan(...any) error }) (SkillContent, error) {
	var v SkillContent
	var resources, scripts string
	err := row.Scan(&v.ID, &v.Name, &v.Description, &v.Enabled, &v.Content, &resources, &scripts, &v.Note, &v.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return v, ValidationError("技能不存在")
	}
	if err == nil {
		err = json.Unmarshal([]byte(resources), &v.Resources)
		if err == nil {
			err = json.Unmarshal([]byte(scripts), &v.Scripts)
		}
	}
	return v, err
}
func (s *Store) SkillContent(id string) (SkillContent, error) {
	return scanSkill(s.db.QueryRow(`SELECT `+skillColumns+` FROM skills WHERE id=?`, id))
}

// Saves replace current content. updated_at only guards against stale editor saves.
func (s *Store) SaveSkill(id, expected string, enabled bool, p skill.Bundle) (SkillContent, error) {
	m, err := skill.Validate(p)
	if err != nil {
		return SkillContent{}, ValidationError(err.Error())
	}
	if p.Resources == nil {
		p.Resources = []skill.Resource{}
	}
	if p.Scripts == nil {
		p.Scripts = []skill.Resource{}
	}
	scripts, err := json.Marshal(p.Scripts)
	if err != nil {
		return SkillContent{}, err
	}
	resources, err := json.Marshal(p.Resources)
	if err != nil {
		return SkillContent{}, err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return SkillContent{}, err
	}
	defer tx.Rollback()
	var duplicate bool
	if err = tx.QueryRow(`SELECT EXISTS(SELECT 1 FROM skills WHERE name=? AND id<>?)`, m.Name, id).Scan(&duplicate); err != nil {
		return SkillContent{}, err
	}
	if duplicate {
		return SkillContent{}, ValidationError("已有同名技能，请选择它后编辑")
	}
	now := timestamp()
	if expected == "" {
		_, err = tx.Exec(`INSERT INTO skills(id,name,description,enabled,content,resources,scripts,note,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?)`, id, m.Name, m.Description, enabled, p.Content, string(resources), string(scripts), p.Note, now, now)
	} else {
		var result sql.Result
		result, err = tx.Exec(`UPDATE skills SET name=?,description=?,enabled=?,content=?,resources=?,scripts=?,note=?,updated_at=? WHERE id=? AND updated_at=?`, m.Name, m.Description, enabled, p.Content, string(resources), string(scripts), p.Note, now, id, expected)
		if err == nil {
			n, _ := result.RowsAffected()
			if n != 1 {
				return SkillContent{}, ValidationError("技能已被修改，请重新打开后编辑")
			}
		}
	}
	if err != nil {
		return SkillContent{}, err
	}
	if err = tx.Commit(); err != nil {
		return SkillContent{}, err
	}
	return s.SkillContent(id)
}

// Each Run receives the team's current enabled skill descriptions.
func (s *Store) AvailableSkills(runID string) ([]SkillBinding, error) {
	rows, err := s.db.Query(`SELECT b.agent_id,s.id,s.name,s.description FROM runs r
  JOIN conversation_members m ON m.conversation_id=r.conversation_id
  JOIN agent_skills b ON b.agent_id=m.agent_id JOIN skills s ON s.id=b.skill_id
  WHERE r.id=? AND r.kind<>'selector' AND r.status IN ('queued','running') AND s.enabled=1
  AND (r.chain_id IS NULL OR EXISTS(SELECT 1 FROM chains c WHERE c.id=r.chain_id AND c.status='active'))
  ORDER BY b.agent_id,s.name`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SkillBinding{}
	for rows.Next() {
		var v SkillBinding
		if err = rows.Scan(&v.AgentID, &v.SkillID, &v.Name, &v.Description); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// Every tool call reads current content and checks the Run's current role binding.
func (s *Store) LoadSkill(runID, skillID string, resource bool) (SkillContent, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return SkillContent{}, err
	}
	defer tx.Rollback()
	var conversationID string
	if err = tx.QueryRow(`SELECT conversation_id FROM runs WHERE id=?`, runID).Scan(&conversationID); err != nil {
		return SkillContent{}, err
	}
	if err = activeSourceRun(tx, runID, conversationID); err != nil {
		return SkillContent{}, err
	}
	var allowed bool
	err = tx.QueryRow(`SELECT EXISTS(SELECT 1 FROM runs r JOIN agent_skills b ON b.agent_id=r.agent_id
  JOIN skills s ON s.id=b.skill_id WHERE r.id=? AND r.kind<>'selector' AND s.id=? AND s.enabled=1)`, runID, skillID).Scan(&allowed)
	if err != nil {
		return SkillContent{}, err
	}
	if !allowed {
		return SkillContent{}, ValidationError("此角色未绑定该技能，或技能已停用")
	}
	if resource {
		var loaded bool
		if err = tx.QueryRow(`SELECT EXISTS(SELECT 1 FROM skill_uses WHERE run_id=? AND skill_id=?)`, runID, skillID).Scan(&loaded); err != nil {
			return SkillContent{}, err
		}
		if !loaded {
			return SkillContent{}, ValidationError("请先调用 load_skill 阅读技能说明")
		}
	}
	v, err := scanSkill(tx.QueryRow(`SELECT `+skillColumns+` FROM skills WHERE id=?`, skillID))
	if err != nil {
		return v, err
	}
	if !resource {
		if _, err = tx.Exec(`INSERT OR IGNORE INTO skill_uses VALUES(?,?,?,?)`, runID, skillID, v.Name, timestamp()); err != nil {
			return v, err
		}
	}
	return v, tx.Commit()
}
func (s *Store) SkillUses(conversationID string) ([]SkillUse, error) {
	rows, err := s.db.Query(`SELECT u.run_id,u.skill_id,u.name,u.created_at FROM skill_uses u
  JOIN runs r ON r.id=u.run_id WHERE r.conversation_id=? ORDER BY u.created_at,u.skill_id`, conversationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SkillUse{}
	for rows.Next() {
		var v SkillUse
		if err = rows.Scan(&v.RunID, &v.SkillID, &v.Name, &v.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
