package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

type Settings struct {
	BaseURL string `json:"baseURL"`
	Model   string `json:"model"`
	KeyRef  string `json:"-"`
}

// ProbeRun is P0 verification history, independent of the future conversation schema.
type ProbeRun struct {
	ID         string   `json:"id"`
	Source     string   `json:"source"`
	Prompt     string   `json:"prompt"`
	Status     string   `json:"status"`
	Text       string   `json:"text"`
	Error      string   `json:"error"`
	Tools      []string `json:"tools"`
	StartedAt  string   `json:"startedAt"`
	FinishedAt string   `json:"finishedAt"`
	Revision   int      `json:"revision"`
}

type Store struct{ db *sql.DB }

func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", filepath.Join(dir, "tongxi.db"))
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	if err = s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	// P0 never replays an uncertain model invocation after a crash.
	if _, err = db.Exec(`UPDATE probe_runs SET status='interrupted', error='上次执行被中断，请重新发起验证', revision=revision+1 WHERE status='running'`); err != nil {
		db.Close()
		return nil, err
	}
	if err = s.recoverChains(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) migrate() error {
	if _, err := s.db.Exec(`PRAGMA journal_mode=WAL; PRAGMA busy_timeout=5000; PRAGMA foreign_keys=ON;`); err != nil {
		return err
	}
	var version int
	if err := s.db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		return err
	}
	if version > len(migrations) {
		return fmt.Errorf("数据库版本 %d 高于当前程序支持版本", version)
	}
	if version == len(migrations) {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for i := version; i < len(migrations); i++ {
		if _, err := tx.Exec(migrations[i]); err != nil {
			return err
		}
		if _, err := tx.Exec(fmt.Sprintf("PRAGMA user_version=%d", i+1)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) Settings() (Settings, error) {
	var v Settings
	err := s.db.QueryRow(`SELECT base_url,model,key_ref FROM model_settings WHERE id=1`).Scan(&v.BaseURL, &v.Model, &v.KeyRef)
	if err == sql.ErrNoRows {
		return v, nil
	}
	return v, err
}

func (s *Store) SaveSettings(v Settings) error {
	_, err := s.db.Exec(`INSERT INTO model_settings VALUES(1,?,?,?) ON CONFLICT(id) DO UPDATE SET base_url=excluded.base_url, model=excluded.model, key_ref=excluded.key_ref`, v.BaseURL, v.Model, v.KeyRef)
	return err
}

func (s *Store) InsertRun(r ProbeRun) error {
	b, err := json.Marshal(r.Tools)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`INSERT INTO probe_runs VALUES(?,?,?,?,?,?,?,?,?,?)`, r.ID, r.Source, r.Prompt, r.Status, r.Text, r.Error, string(b), r.StartedAt, r.FinishedAt, r.Revision)
	return err
}

func (s *Store) FinishRun(r ProbeRun) error {
	b, err := json.Marshal(r.Tools)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`UPDATE probe_runs SET status=?,text=?,error=?,tools=?,finished_at=?,revision=? WHERE id=? AND status='running'`, r.Status, r.Text, r.Error, string(b), r.FinishedAt, r.Revision, r.ID)
	return err
}

func (s *Store) Runs() ([]ProbeRun, error) {
	rows, err := s.db.Query(`SELECT * FROM (SELECT * FROM probe_runs ORDER BY started_at DESC LIMIT 30) ORDER BY started_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	runs := make([]ProbeRun, 0)
	for rows.Next() {
		var r ProbeRun
		var raw string
		if err := rows.Scan(&r.ID, &r.Source, &r.Prompt, &r.Status, &r.Text, &r.Error, &raw, &r.StartedAt, &r.FinishedAt, &r.Revision); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(raw), &r.Tools); err != nil {
			return nil, err
		}
		runs = append(runs, r)
	}
	return runs, rows.Err()
}

func (s *Store) Close() error { return s.db.Close() }
