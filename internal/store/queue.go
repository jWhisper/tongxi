package store

import (
	"database/sql"
	"encoding/json"
	"unicode/utf8"

	"github.com/cloudwego/eino/schema"
)

const runColumns = `id,conversation_id,agent_id,message_id,status,error,created_at,text,tools,started_at,finished_at,revision,agent_name,COALESCE(chain_id,''),COALESCE(parent_run_id,''),COALESCE(previous_run_id,''),COALESCE(retry_of,''),kind,silent`

func scanConversationRun(row interface{ Scan(...any) error }) (ConversationRun, error) {
	var r ConversationRun
	var tools string
	err := row.Scan(&r.ID, &r.ConversationID, &r.AgentID, &r.MessageID, &r.Status, &r.Error, &r.CreatedAt, &r.Text, &tools, &r.StartedAt, &r.FinishedAt, &r.Revision, &r.AgentName, &r.ChainID, &r.ParentRunID, &r.PreviousRunID, &r.RetryOf, &r.Kind, &r.Silent)
	if err == nil {
		err = json.Unmarshal([]byte(tools), &r.Tools)
	}
	return r, err
}

// Enqueue saves the user's message and initial run atomically. A request ID
// identifies a delivery, not its text: intentionally repeating text is allowed.
func (s *Store) Enqueue(requestID string, m Message, runID string) (ConversationRun, error) {
	d, err := s.Schedule(ScheduleRequest{RequestID: requestID, ConversationID: m.ConversationID, Content: m.Content, Action: "direct"}, m.ID, "chain_"+runID, []string{runID})
	if err != nil {
		return ConversationRun{}, err
	}
	return d.Runs[0], nil
}

func insertMessage(tx *sql.Tx, m *Message) error {
	if err := tx.QueryRow(`SELECT COALESCE(MAX(sequence),0)+1 FROM messages WHERE conversation_id=?`, m.ConversationID).Scan(&m.Sequence); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO messages(id,conversation_id,sequence,sender_type,sender_id,sender_name,content,created_at,target_agent_id,reply_to_message_id,source_run_id) VALUES(?,?,?,?,NULLIF(?,''),?,?,?,NULLIF(?,''),NULLIF(?,''),NULLIF(?,''))`, m.ID, m.ConversationID, m.Sequence, m.SenderType, m.SenderID, m.SenderName, m.Content, m.CreatedAt, m.TargetAgentID, m.ReplyToMessageID, m.SourceRunID); err != nil {
		return err
	}
	_, err := tx.Exec(`UPDATE conversations SET updated_at=? WHERE id=?`, m.CreatedAt, m.ConversationID)
	return err
}

func (s *Store) QueuedRun() (ConversationRun, error) {
	return scanConversationRun(s.db.QueryRow(`SELECT ` + runColumns + ` FROM runs WHERE status='queued' AND (chain_id IS NULL OR EXISTS(SELECT 1 FROM chains c WHERE c.id=runs.chain_id AND c.status='active')) AND (previous_run_id IS NULL OR EXISTS(SELECT 1 FROM runs p WHERE p.id=runs.previous_run_id AND p.status='completed')) ORDER BY rowid LIMIT 1`))
}
func (s *Store) ConversationRun(id string) (ConversationRun, error) {
	return scanConversationRun(s.db.QueryRow(`SELECT `+runColumns+` FROM runs WHERE id=?`, id))
}

// The service holds its execution lock while loading configuration and claiming.
// The serialized snapshot excludes the key reference and actual secret.
func (s *Store) StartConversationRun(r ConversationRun, a Agent) error {
	_, err := s.ClaimConversationRun(r, a)
	return err
}

// History is kept in complete turns so a window never separates tool calls
// from their results. Failed or cancelled turns are retained but not replayed.
func (s *Store) ModelHistory(agentID, conversationID string) ([]*schema.Message, error) {
	rows, err := s.db.Query(`SELECT transcript FROM runs WHERE agent_id=? AND conversation_id=? AND status='completed' AND kind='reply' AND silent=0 ORDER BY rowid DESC LIMIT 12`, agentID, conversationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	turns := [][]*schema.Message{}
	characters := 0
	for rows.Next() {
		var data string
		if err = rows.Scan(&data); err != nil {
			return nil, err
		}
		characters += utf8.RuneCountInString(data)
		if characters > 48000 {
			break
		}
		var turn []*schema.Message
		if err = json.Unmarshal([]byte(data), &turn); err != nil {
			return nil, err
		}
		turns = append(turns, turn)
	}
	history := []*schema.Message{}
	for i := len(turns) - 1; i >= 0; i-- {
		history = append(history, turns[i]...)
	}
	return history, rows.Err()
}

// A completed public reply and terminal run state are committed together.
// Terminal states are immutable, including when a model returns after cancel.
func (s *Store) FinishConversationRun(r ConversationRun, transcript []*schema.Message, replyID string) error {
	data, err := json.Marshal(transcript)
	if err != nil {
		return err
	}
	tools, err := json.Marshal(r.Tools)
	if err != nil {
		return err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.Exec(`UPDATE runs SET status=?,error=?,text=?,tools=?,finished_at=?,revision=?,transcript=?,silent=? WHERE id=? AND status IN ('queued','running')`, r.Status, r.Error, r.Text, string(tools), r.FinishedAt, r.Revision, string(data), r.Silent, r.ID)
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	// A cancelled stream may finish draining later. Archive its private output
	// without changing the terminal status, visible partial text, or revision.
	if n == 0 && (r.Status == "cancelled" || r.Status == "interrupted") && len(transcript) > 0 {
		if _, err = tx.Exec(`UPDATE runs SET transcript=? WHERE id=? AND status IN ('cancelled','interrupted') AND transcript='null'`, string(data), r.ID); err != nil {
			return err
		}
	}
	if n == 1 && r.Status == "completed" && r.Kind != "selector" && !r.Silent {
		m := Message{ID: replyID, ConversationID: r.ConversationID, SenderType: "agent", SenderID: r.AgentID, SenderName: r.AgentName, Content: r.Text, CreatedAt: r.FinishedAt, SourceRunID: r.ID, ReplyToMessageID: r.MessageID}
		if err = insertMessage(tx, &m); err != nil {
			return err
		}
	}
	if n == 1 {
		if r.Status != "completed" {
			if err = restoreCursor(tx, r.ID); err != nil {
				return err
			}
		}
		if err = finishChain(tx, r); err != nil {
			return err
		}
	}
	return tx.Commit()
}
