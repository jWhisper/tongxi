package store

import (
	"database/sql"
	"encoding/json"
)

func readRuns(tx *sql.Tx, query string, args ...any) ([]ConversationRun, error) {
	rows, err := tx.Query(`SELECT `+runColumns+` FROM runs WHERE `+query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []ConversationRun{}
	for rows.Next() {
		r, err := scanConversationRun(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, r)
	}
	return list, rows.Err()
}
func restoreCursor(tx *sql.Tx, runID string) error {
	_, err := tx.Exec(`UPDATE member_cursors SET sequence=MIN(sequence,(SELECT read_from FROM runs WHERE id=?)) WHERE EXISTS(SELECT 1 FROM runs r WHERE r.id=? AND r.conversation_id=member_cursors.conversation_id AND r.agent_id=member_cursors.agent_id AND r.read_upper>0)`, runID, runID)
	return err
}
func cancelRun(tx *sql.Tx, r ConversationRun, active *ConversationRun, reason string) (ConversationRun, error) {
	if active != nil && active.ID == r.ID && active.Revision > r.Revision {
		r = *active
	}
	r.Status, r.Error, r.FinishedAt = "cancelled", reason, timestamp()
	r.Revision++
	data, err := json.Marshal(r.Tools)
	if err != nil {
		return r, err
	}
	_, err = tx.Exec(`UPDATE runs SET status='cancelled',error=?,text=?,tools=?,finished_at=?,revision=?,transcript='null' WHERE id=? AND status IN ('queued','running')`, r.Error, r.Text, string(data), r.FinishedAt, r.Revision, r.ID)
	if err == nil {
		err = restoreCursor(tx, r.ID)
	}
	return r, err
}
func stopChain(tx *sql.Tx, id string, active *ConversationRun, reason string) ([]ConversationRun, error) {
	var status string
	err := tx.QueryRow(`SELECT status FROM chains WHERE id=?`, id).Scan(&status)
	if err == sql.ErrNoRows {
		return nil, ValidationError("协作记录不存在")
	}
	if err != nil {
		return nil, err
	}
	if status == "completed" {
		return []ConversationRun{}, nil
	}
	if _, err = tx.Exec(`UPDATE chains SET status='stopped',reason=? WHERE id=?`, reason, id); err != nil {
		return nil, err
	}
	runs, err := readRuns(tx, `chain_id=? AND status IN ('queued','running') ORDER BY rowid`, id)
	if err != nil {
		return nil, err
	}
	for i, r := range runs {
		runs[i], err = cancelRun(tx, r, active, reason)
		if err != nil {
			return nil, err
		}
	}
	return runs, nil
}

// Stop persists the chain barrier and all terminal run snapshots together.
// The service cancels the model context once this transaction has committed.
func (s *Store) StopChain(id string, active *ConversationRun) ([]ConversationRun, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	runs, err := stopChain(tx, id, active, "已停止整次协作；已发布消息保留，后续任务不再执行")
	if err != nil {
		return nil, err
	}
	return runs, tx.Commit()
}

func (s *Store) SetAgentEnabledAndCancel(id string, enabled bool, active *ConversationRun) (Agent, []ConversationRun, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return Agent{}, nil, err
	}
	defer tx.Rollback()
	result, err := tx.Exec(`UPDATE agents SET enabled=?,version=version+1,updated_at=? WHERE id=?`, enabled, timestamp(), id)
	if err != nil {
		return Agent{}, nil, err
	}
	n, _ := result.RowsAffected()
	if n != 1 {
		return Agent{}, nil, ValidationError("角色不存在")
	}
	changed := []ConversationRun{}
	if !enabled {
		pending, err := readRuns(tx, `(agent_id=? OR chain_id IN (SELECT id FROM chains WHERE action='lead' AND lead_agent_id=?)) AND status IN ('queued','running') ORDER BY rowid`, id, id)
		if err != nil {
			return Agent{}, nil, err
		}
		seen := map[string]bool{}
		for _, r := range pending {
			reason := "成员已停用，本次协作已停止；重新启用不会重放旧任务"
			if r.ChainID != "" {
				if seen[r.ChainID] {
					continue
				}
				seen[r.ChainID] = true
				runs, err := stopChain(tx, r.ChainID, active, reason)
				if err != nil {
					return Agent{}, nil, err
				}
				changed = append(changed, runs...)
			} else {
				r, err = cancelRun(tx, r, active, reason)
				if err != nil {
					return Agent{}, nil, err
				}
				changed = append(changed, r)
			}
		}
	}
	a, err := scanAgent(tx.QueryRow(`SELECT * FROM agents WHERE id=?`, id))
	if err != nil {
		return a, nil, err
	}
	return a, changed, tx.Commit()
}

// A failed/interrupted lead (or private) turn may be retried exactly once by
// creating a new run against the original trigger. The next retry targets the
// new failed run. Stopped chains never reopen, and discussion starts anew.
func (s *Store) RetryRun(id, requestID, newID string) (ConversationRun, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return ConversationRun{}, err
	}
	defer tx.Rollback()
	var prior string
	err = tx.QueryRow(`SELECT run_id FROM retry_requests WHERE request_id=?`, requestID).Scan(&prior)
	if err == nil {
		r, err := scanConversationRun(tx.QueryRow(`SELECT `+runColumns+` FROM runs WHERE id=?`, prior))
		if err == nil && r.RetryOf != id {
			return r, ValidationError("重试标识已用于其他任务")
		}
		return r, err
	}
	if err != sql.ErrNoRows {
		return ConversationRun{}, err
	}
	r, err := scanConversationRun(tx.QueryRow(`SELECT `+runColumns+` FROM runs WHERE retry_of=?`, id))
	if err == nil {
		if _, err = tx.Exec(`INSERT INTO retry_requests VALUES(?,?)`, requestID, r.ID); err != nil {
			return r, err
		}
		return r, tx.Commit()
	}
	if err != sql.ErrNoRows {
		return r, err
	}
	original, err := scanConversationRun(tx.QueryRow(`SELECT `+runColumns+` FROM runs WHERE id=?`, id))
	if err != nil {
		return r, err
	}
	if original.Status != "failed" && original.Status != "interrupted" {
		return r, ValidationError("仅失败或中断的任务可以重试")
	}
	if original.ChainID == "" {
		return r, ValidationError("旧版任务请重新发送消息")
	}
	c, err := scanChain(tx.QueryRow(`SELECT `+chainColumns+` FROM chains WHERE id=?`, original.ChainID))
	if err != nil {
		return r, err
	}
	if c.Action != "lead" && c.Action != "direct" {
		return r, ValidationError("讨论任务请重新安排一轮，不续跑旧任务")
	}
	if c.Status == "stopped" {
		return r, ValidationError("已停止的协作不能重试，请重新发起")
	}
	var pending, unchanged bool
	if err = tx.QueryRow(`SELECT EXISTS(SELECT 1 FROM runs WHERE chain_id=? AND status IN ('queued','running'))`, c.ID).Scan(&pending); err != nil {
		return r, err
	}
	if pending {
		return r, ValidationError("本次协作仍有任务在途，请等待结束")
	}
	if err = tx.QueryRow(`SELECT ch.conversation_revision=c.revision FROM chains ch JOIN conversations c ON c.id=ch.conversation_id WHERE ch.id=?`, c.ID).Scan(&unchanged); err != nil {
		return r, err
	}
	if !unchanged {
		return r, ValidationError("会话设置已修改，请重新发起协作")
	}
	var name string
	err = tx.QueryRow(`SELECT a.name FROM agents a JOIN conversation_members m ON m.agent_id=a.id WHERE a.enabled=1 AND a.id=? AND m.conversation_id=?`, original.AgentID, original.ConversationID).Scan(&name)
	if err == sql.ErrNoRows {
		return r, ValidationError("角色已停用或已离开会话")
	}
	if err != nil {
		return r, err
	}
	limit := ChainLimit
	if c.Action == "lead" && original.AgentID != c.LeadAgentID {
		limit-- // A successful member retry still needs a lead conclusion.
	}
	result, err := tx.Exec(`UPDATE chains SET reserved=reserved+1,status='active',reason='' WHERE id=? AND status='failed' AND reserved<?`, c.ID, limit)
	if err != nil {
		return r, err
	}
	n, _ := result.RowsAffected()
	if n != 1 {
		return r, ValidationError("本次协作已结束或已用完 6 次额度，请重新发起")
	}
	r = ConversationRun{ID: newID, ConversationID: original.ConversationID, AgentID: original.AgentID, MessageID: original.MessageID, ChainID: c.ID, ParentRunID: original.ParentRunID, RetryOf: original.ID, AgentName: name, Status: "queued", CreatedAt: timestamp(), Revision: 1, Tools: []string{}}
	if _, err = tx.Exec(`INSERT INTO runs(id,conversation_id,agent_id,message_id,status,error,created_at,agent_name,chain_id,parent_run_id,retry_of) VALUES(?,?,?,?,'queued','',?,?,?,NULLIF(?,''),?)`, r.ID, r.ConversationID, r.AgentID, r.MessageID, r.CreatedAt, r.AgentName, r.ChainID, r.ParentRunID, r.RetryOf); err != nil {
		return r, err
	}
	if _, err = tx.Exec(`INSERT INTO retry_requests VALUES(?,?)`, requestID, r.ID); err != nil {
		return r, err
	}
	return r, tx.Commit()
}

// Recover only work that was running at this startup. Historical failures must
// not cancel a newly queued explicit retry in the same chain.
func (s *Store) recoverChains() error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	running, err := readRuns(tx, `status='running' ORDER BY rowid`)
	if err != nil {
		return err
	}
	for _, r := range running {
		if _, err = tx.Exec(`UPDATE runs SET status='interrupted',error='上次执行被中断，可手动重试或重新安排',finished_at=?,revision=revision+1 WHERE id=?`, timestamp(), r.ID); err != nil {
			return err
		}
		if err = restoreCursor(tx, r.ID); err != nil {
			return err
		}
		r.Status = "interrupted"
		if err = finishChain(tx, r); err != nil {
			return err
		}
	}
	_, err = tx.Exec(`UPDATE runs SET status='cancelled',error='本次协作已结束，待办不再执行',finished_at=?,revision=revision+1 WHERE status='queued' AND chain_id IN (SELECT id FROM chains WHERE status!='active')`, timestamp())
	if err != nil {
		return err
	}
	return tx.Commit()
}
