package store

import (
	"database/sql"
	"encoding/json"

	"github.com/cloudwego/eino/schema"
)

// ContextState is the bounded working context, never the authoritative transcript.
// It is committed with the successful run and its public-message coverage.
type ContextState struct {
	Messages     []*schema.Message
	ModelID      string
	ModelVersion int
	TokenRatio   float64
}

func (s *Store) ContextHistory(agentID, conversationID, kind string) ([]*schema.Message, error) {
	var data string
	var through int64
	err := s.db.QueryRow(`SELECT messages,through_run FROM context_states WHERE conversation_id=? AND agent_id=? AND kind=?`, conversationID, agentID, kind).Scan(&data, &through)
	if err != nil && err != sql.ErrNoRows {
		return nil, err
	}
	history := []*schema.Message{}
	if err == nil {
		if err = json.Unmarshal([]byte(data), &history); err != nil {
			return nil, err
		}
	}
	rows, err := s.db.Query(`SELECT transcript FROM runs WHERE agent_id=? AND conversation_id=? AND kind=? AND status='completed' AND silent=0 AND rowid>? ORDER BY rowid`, agentID, conversationID, kind, through)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		if err = rows.Scan(&data); err != nil {
			return nil, err
		}
		var turn []*schema.Message
		if err = json.Unmarshal([]byte(data), &turn); err != nil {
			return nil, err
		}
		history = append(history, turn...)
	}
	return history, rows.Err()
}

func saveContext(tx *sql.Tx, r ConversationRun, state *ContextState) error {
	data, err := json.Marshal(state.Messages)
	if err != nil {
		return err
	}
	_, err = tx.Exec(`INSERT INTO context_states(conversation_id,agent_id,kind,through_run,public_upper,messages,updated_at)
		SELECT conversation_id,agent_id,kind,rowid,read_upper,?,? FROM runs WHERE id=?
		ON CONFLICT(conversation_id,agent_id,kind) DO UPDATE SET through_run=excluded.through_run,public_upper=excluded.public_upper,messages=excluded.messages,updated_at=excluded.updated_at`, string(data), timestamp(), r.ID)
	if err == nil && state.TokenRatio > 0 {
		_, err = tx.Exec(`UPDATE model_configs SET token_ratio=? WHERE id=? AND version=?`, state.TokenRatio, state.ModelID, state.ModelVersion)
	}
	return err
}

func (s *Store) SaveContextToolResult(conversationID, agentID, id, content string) error {
	_, err := s.db.Exec(`INSERT INTO context_tool_results VALUES(?,?,?,?)`, id, conversationID, agentID, content)
	return err
}

func (s *Store) ContextToolResult(conversationID, agentID, id string, offset int, pageSize ...int) (string, int, error) {
	if offset < 0 {
		return "", 0, ValidationError("读取位置不能为负数")
	}
	limit := 4000
	if len(pageSize) > 0 {
		limit = max(128, min(4000, pageSize[0]))
	}
	var content string
	var size int
	err := s.db.QueryRow(`SELECT substr(content,?,?),length(content) FROM context_tool_results WHERE id=? AND conversation_id=? AND agent_id=?`, offset+1, limit, id, conversationID, agentID).Scan(&content, &size)
	if err == sql.ErrNoRows {
		return "", 0, ValidationError("当前角色在本会话中没有这条工具记录")
	}
	next := 0
	if offset+limit < size {
		next = offset + 4000
	}
	return content, next, err
}
