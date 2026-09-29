package store

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"
)

// Artifact is a saved file, independent of changes to its original workspace path.
type Artifact struct {
	ID             string `json:"id"`
	ConversationID string `json:"conversationID"`
	RunID          string `json:"runID"`
	ScriptRunID    string `json:"scriptRunID"`
	Name           string `json:"name"`
	Path           string `json:"path"`
	Format         string `json:"format"`
	Hash           string `json:"hash"`
	Size           int    `json:"size"`
	SourceID       string `json:"sourceID"`
	CreatedAt      string `json:"createdAt"`
}
type ArtifactDelivery struct {
	ArtifactID string `json:"artifact_id" jsonschema:"description=本轮生成或基础版本继承的真实文件ID，从 list_artifacts 或脚本结果获取"`
	Evidence   string `json:"evidence" jsonschema:"description=读取实际文件后逐项核对的具体依据；不能只说执行成功。交付时必填"`
}

const artifactColumns = `id,conversation_id,run_id,script_run_id,name,path,format,hash,size,source_id,created_at`

func scanArtifact(row interface{ Scan(...any) error }) (Artifact, error) {
	var a Artifact
	err := row.Scan(&a.ID, &a.ConversationID, &a.RunID, &a.ScriptRunID, &a.Name, &a.Path, &a.Format, &a.Hash, &a.Size, &a.SourceID, &a.CreatedAt)
	return a, err
}
func (s *Store) Artifacts(conversationID string) ([]Artifact, error) {
	rows, err := s.db.Query(`SELECT `+artifactColumns+` FROM artifacts WHERE conversation_id=? ORDER BY created_at,id`, conversationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Artifact{}
	for rows.Next() {
		a, e := scanArtifact(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
func (s *Store) Artifact(conversationID, id string) (Artifact, error) {
	a, err := scanArtifact(s.db.QueryRow(`SELECT `+artifactColumns+` FROM artifacts WHERE id=? AND conversation_id=?`, id, conversationID))
	if err == sql.ErrNoRows {
		return a, ValidationError("当前会话中不存在这份成果文件")
	}
	return a, err
}
func (s *Store) ArtifactData(conversationID, id string) ([]byte, error) {
	var data []byte
	err := s.db.QueryRow(`SELECT data FROM artifacts WHERE id=? AND conversation_id=?`, id, conversationID).Scan(&data)
	if err == sql.ErrNoRows {
		return nil, ValidationError("当前会话中不存在这份成果文件")
	}
	return data, err
}
func (s *Store) SaveArtifact(a Artifact, data []byte, segments []SourceSegment, note string) (Artifact, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return a, err
	}
	defer tx.Rollback()
	if err = activeSourceRun(tx, a.RunID, a.ConversationID); err != nil {
		return a, err
	}
	var files string
	err = tx.QueryRow(`SELECT files FROM skill_script_runs WHERE id=? AND run_id=? AND status='completed' AND exit_code=0`, a.ScriptRunID, a.RunID).Scan(&files)
	if err == sql.ErrNoRows {
		return a, ValidationError("只能登记成功执行实际生成的文件")
	}
	if err != nil {
		return a, err
	}
	var paths []string
	if err = json.Unmarshal([]byte(files), &paths); err != nil {
		return a, err
	}
	if !slices.Contains(paths, a.Path) || !filepath.IsLocal(a.Path) {
		return a, ValidationError("文件不在本次脚本生成清单中")
	}
	if len(data) == 0 || len(data) > 20*1024*1024 {
		return a, ValidationError("成果文件需非空且不超过20MB")
	}
	sum := sha256.Sum256(data)
	a.Hash = hex.EncodeToString(sum[:])
	a.Size = len(data)
	a.Name = filepath.Base(a.Path)
	source, err := saveSource(tx, Source{ID: a.ID, ConversationID: a.ConversationID, Name: a.Path, Kind: "artifact", Format: a.Format, Hash: a.Hash, Size: a.Size, Note: note}, segments)
	if err != nil {
		return a, err
	}
	a.SourceID = source.ID
	a.CreatedAt = timestamp()
	_, err = tx.Exec(`INSERT INTO artifacts (`+artifactColumns+`,data) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, a.ID, a.ConversationID, a.RunID, a.ScriptRunID, a.Name, a.Path, a.Format, a.Hash, a.Size, a.SourceID, a.CreatedAt, data)
	if err != nil {
		return a, err
	}
	return a, tx.Commit()
}
func validateArtifactDelivery(tx *sql.Tx, c Chain, step LeadStep) error {
	if len(step.Files) > 32 {
		return ValidationError("每份成果最多交付32个文件")
	}
	seen := map[string]bool{}
	for _, file := range step.Files {
		if seen[file.ArtifactID] {
			return ValidationError("不要重复交付同一个文件")
		}
		seen[file.ArtifactID] = true
		var sourceID, chainID string
		var segments int
		err := tx.QueryRow(`SELECT a.source_id,COALESCE(r.chain_id,''),s.segments FROM artifacts a JOIN runs r ON r.id=a.run_id JOIN sources s ON s.id=a.source_id WHERE a.id=? AND a.conversation_id=?`, file.ArtifactID, c.ConversationID).Scan(&sourceID, &chainID, &segments)
		if err == sql.ErrNoRows {
			return ValidationError("交付文件不存在或不属于本会话")
		}
		if err != nil {
			return err
		}
		allowed := chainID == c.ID
		if !allowed && step.WorkMode == "continue" && c.Basis != nil && c.Basis.Work != nil {
			for _, old := range c.Basis.Work.Files {
				allowed = allowed || old.ArtifactID == file.ArtifactID
			}
		}
		if !allowed {
			return ValidationError("请选择本轮生成或基础成果版本中的文件")
		}
		if step.Action != "complete" {
			continue
		}
		if strings.TrimSpace(file.Evidence) == "" || utf8.RuneCountInString(file.Evidence) > 1000 {
			return ValidationError("请填写每个文件的具体核对依据")
		}
		var read int
		err = tx.QueryRow(`SELECT count(DISTINCT sr.segment) FROM source_reads sr JOIN runs r ON r.id=sr.run_id WHERE r.chain_id=? AND sr.source_id=? AND r.status IN ('running','completed')`, c.ID, sourceID).Scan(&read)
		if err != nil {
			return err
		}
		if read < segments {
			return ValidationError("交付前请用 read_artifact 实际读完文件文字片段（按 next 续页），再核对内容；历史文件续用也须在本轮重新读取")
		}
	}
	if step.Action == "complete" && len(step.Files) == 0 {
		var generated bool
		if err := tx.QueryRow(`SELECT EXISTS(SELECT 1 FROM artifacts a JOIN runs r ON r.id=a.run_id WHERE r.chain_id=?)`, c.ID).Scan(&generated); err != nil {
			return err
		}
		if generated {
			return ValidationError("本轮已生成成果文件，请在 files 中选择需要交付的文件并填写核对依据")
		}
	}
	return nil
}
