package store

import (
	"encoding/json"
	"tongxi/internal/scriptrun"
)

type ScriptRun struct {
	ID         string   `json:"id"`
	RunID      string   `json:"runID"`
	SkillID    string   `json:"skillID"`
	Name       string   `json:"name"`
	Path       string   `json:"path"`
	Args       []string `json:"args"`
	Status     string   `json:"status"`
	ExitCode   int      `json:"exitCode"`
	Stdout     string   `json:"stdout"`
	Stderr     string   `json:"stderr"`
	Error      string   `json:"error"`
	Files      []string `json:"files"`
	StartedAt  string   `json:"startedAt"`
	FinishedAt string   `json:"finishedAt"`
}

func (s *Store) StartScriptRun(v ScriptRun, conversationID string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = activeSourceRun(tx, v.RunID, conversationID); err != nil {
		return err
	}
	var count int
	if err = tx.QueryRow(`SELECT count(*) FROM skill_script_runs WHERE run_id=?`, v.RunID).Scan(&count); err != nil {
		return err
	}
	if count >= 3 {
		return ValidationError("本次回复最多执行3次脚本，请根据已有结果继续或报告缺失能力")
	}
	args, err := json.Marshal(v.Args)
	if err != nil {
		return err
	}
	_, err = tx.Exec(`INSERT INTO skill_script_runs(id,run_id,skill_id,name,path,args,status,started_at) VALUES(?,?,?,?,?,?,'running',?)`, v.ID, v.RunID, v.SkillID, v.Name, v.Path, string(args), timestamp())
	if err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Store) FinishScriptRun(id string, r scriptrun.Result) error {
	if r.Files == nil {
		r.Files = []string{}
	}
	files, err := json.Marshal(r.Files)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`UPDATE skill_script_runs SET status=?,exit_code=?,stdout=?,stderr=?,error=?,files=?,finished_at=? WHERE id=? AND status='running'`, r.Status, r.ExitCode, r.Stdout, r.Stderr, r.Error, string(files), timestamp(), id)
	return err
}
func (s *Store) ScriptRuns(conversationID string) ([]ScriptRun, error) {
	rows, err := s.db.Query(`SELECT s.id,s.run_id,s.skill_id,s.name,s.path,s.args,s.status,s.exit_code,s.stdout,s.stderr,s.error,s.files,s.started_at,s.finished_at FROM skill_script_runs s JOIN runs r ON r.id=s.run_id WHERE r.conversation_id=? ORDER BY s.started_at,s.id`, conversationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ScriptRun{}
	for rows.Next() {
		var v ScriptRun
		var args, files string
		if err = rows.Scan(&v.ID, &v.RunID, &v.SkillID, &v.Name, &v.Path, &args, &v.Status, &v.ExitCode, &v.Stdout, &v.Stderr, &v.Error, &files, &v.StartedAt, &v.FinishedAt); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(args), &v.Args); err != nil {
			return nil, err
		}
		if v.Args == nil {
			v.Args = []string{}
		}
		if err = json.Unmarshal([]byte(files), &v.Files); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *Store) ScriptFileNotice(id, stderr string) error {
	_, err := s.db.Exec(`UPDATE skill_script_runs SET stderr=? WHERE id=?`, stderr, id)
	return err
}
