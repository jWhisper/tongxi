package store

import "database/sql"

// Workspace files have one stable reference per path, without a stored body.
func (s *Store) SaveWorkspaceFile(v Source, runID string) (Source, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return Source{}, err
	}
	defer tx.Rollback()
	if err = activeSourceRun(tx, runID, v.ConversationID); err != nil {
		return Source{}, err
	}
	v.Kind, v.Hash, v.URL = "workspace", "", ""
	old, err := scanSource(tx.QueryRow(`SELECT `+sourceColumns+` FROM sources WHERE conversation_id=? AND kind='workspace' AND name=?`, v.ConversationID, v.Name))
	if err == nil {
		v.ID, v.CreatedAt = old.ID, old.CreatedAt
		_, err = tx.Exec(`UPDATE sources SET format=?,size=?,segments=?,characters=?,note=? WHERE id=?`, v.Format, v.Size, v.Segments, v.Characters, v.Note, v.ID)
	} else if err == sql.ErrNoRows {
		v.CreatedAt = timestamp()
		_, err = tx.Exec(`INSERT INTO sources(`+sourceColumns+`) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, v.ID, v.ConversationID, v.Name, v.Kind, v.Format, v.URL, v.Hash, v.Size, v.Segments, v.Characters, v.Note, v.CreatedAt)
	}
	if err != nil {
		return Source{}, err
	}
	return v, tx.Commit()
}

func (s *Store) Source(conversationID, id string) (Source, error) {
	v, err := scanSource(s.db.QueryRow(`SELECT `+sourceColumns+` FROM sources WHERE conversation_id=? AND id=?`, conversationID, id))
	if err == sql.ErrNoRows {
		return v, ValidationError("当前会话中不存在这份资料")
	}
	return v, err
}

// Only tool-returned passages are receipts. UI previews never become evidence.
func (s *Store) RecordSourceRead(page SourcePage, runID string) error {
	if runID == "" {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = activeSourceRun(tx, runID, page.Source.ConversationID); err != nil {
		return err
	}
	if err = recordSourceRead(tx, page, runID); err != nil {
		return err
	}
	return tx.Commit()
}

func recordSourceRead(tx *sql.Tx, page SourcePage, runID string) error {
	if runID == "" {
		return nil
	}
	for _, part := range page.Segments {
		if _, err := tx.Exec(`INSERT OR IGNORE INTO source_reads VALUES(?,?,?,?,?)`, runID, page.Source.ID, part.Number, part.Location, part.Content); err != nil {
			return err
		}
	}
	return nil
}

func messageAttachments(tx *sql.Tx, conversationID string, ids []string) ([]Source, error) {
	out := []Source{}
	if len(ids) > 10 {
		return nil, ValidationError("一条消息最多添加10个文件")
	}
	seen := map[string]bool{}
	for _, id := range ids {
		if seen[id] {
			return nil, ValidationError("附件重复，请移除后重试")
		}
		seen[id] = true
		v, err := scanSource(tx.QueryRow(`SELECT `+sourceColumns+` FROM sources WHERE id=? AND conversation_id=? AND kind='workspace'`, id, conversationID))
		if err == sql.ErrNoRows {
			return nil, ValidationError("附件不属于当前会话")
		}
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}
