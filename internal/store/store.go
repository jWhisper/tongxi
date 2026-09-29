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

type Store struct {
	db  *sql.DB
	dir string
}

func (s *Store) Directory() string { return s.dir }

func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", filepath.Join(dir, "tongxi.db"))
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db, dir: dir}
	if err = s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	if _, err = db.Exec(`UPDATE skill_script_runs SET status='interrupted',error='应用意外退出，本次脚本未确认完成',finished_at=? WHERE status='running'`, timestamp()); err != nil {
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
	if version > 0 {
		var current bool
		if err := s.db.QueryRow(`SELECT EXISTS(SELECT 1 FROM pragma_table_info('conversations') WHERE name='token_budget') AND EXISTS(SELECT 1 FROM pragma_table_info('messages') WHERE name='attachments') AND (NOT EXISTS(SELECT 1 FROM sqlite_master WHERE name='model_configs') OR EXISTS(SELECT 1 FROM pragma_table_info('model_configs') WHERE name='context_tokens')) AND NOT EXISTS(SELECT 1 FROM sqlite_master WHERE name='chains' AND sql LIKE '%reserved BETWEEN%')`).Scan(&current); err != nil {
			return err
		}
		if current && version == len(migrations) {
			if err := s.db.QueryRow(`SELECT EXISTS(SELECT 1 FROM sqlite_master WHERE type='table' AND name='image_reads')`).Scan(&current); err != nil {
				return err
			}
		}
		if !current {
			return fmt.Errorf("开发数据库结构已调整，请先备份数据目录，再重建数据库；工作目录中的文件无需删除")
		}
	}
	if version == len(migrations) {
		return nil
	}
	// Table rebuilds keep their original names and references. Validate the
	// complete migrated graph before committing, then restore enforcement.
	if _, err := s.db.Exec(`PRAGMA foreign_keys=OFF`); err != nil {
		return err
	}
	defer s.db.Exec(`PRAGMA foreign_keys=ON`)
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
	var violations int
	if err = tx.QueryRow(`SELECT count(*) FROM pragma_foreign_key_check`).Scan(&violations); err != nil {
		return err
	}
	if violations != 0 {
		return fmt.Errorf("数据迁移发现 %d 处关联错误", violations)
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
