package store

type ImageRead struct {
	RunID     string `json:"runID"`
	SourceID  string `json:"sourceID"`
	Name      string `json:"name"`
	Hash      string `json:"hash"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
	CreatedAt string `json:"createdAt"`
}

func (s *Store) RecordImageRead(conversationID string, read ImageRead) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = activeSourceRun(tx, read.RunID, conversationID); err != nil {
		return err
	}
	var valid bool
	if err = tx.QueryRow(`SELECT EXISTS(SELECT 1 FROM sources WHERE id=? AND conversation_id=? AND kind='workspace')`, read.SourceID, conversationID).Scan(&valid); err != nil {
		return err
	}
	if !valid {
		return ValidationError("图片不属于当前会话")
	}
	_, err = tx.Exec(`INSERT OR IGNORE INTO image_reads VALUES(?,?,?,?,?,?)`, read.RunID, read.SourceID, read.Hash, read.Width, read.Height, timestamp())
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) ImageReads(conversationID string) ([]ImageRead, error) {
	rows, err := s.db.Query(`SELECT i.run_id,i.source_id,s.name,i.hash,i.width,i.height,i.created_at FROM image_reads i JOIN sources s ON s.id=i.source_id WHERE s.conversation_id=? ORDER BY i.rowid`, conversationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ImageRead{}
	for rows.Next() {
		var r ImageRead
		if err = rows.Scan(&r.RunID, &r.SourceID, &r.Name, &r.Hash, &r.Width, &r.Height, &r.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
