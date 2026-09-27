package store

import "database/sql"

const DiscussionSelectionLimit = 6

// A user interruption and the replacement request commit in one transaction.
func supersedeDiscussion(tx *sql.Tx, conversationID string, active *ConversationRun) ([]ConversationRun, error) {
	rows, err := tx.Query(`SELECT id FROM chains WHERE conversation_id=? AND action='discussion' AND status='active'`, conversationID)
	if err != nil {
		return nil, err
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	changed := []ConversationRun{}
	for _, id := range ids {
		runs, err := stopChain(tx, id, active, "收到你的新消息，已转向新话题")
		if err != nil {
			return nil, err
		}
		changed = append(changed, runs...)
	}
	return changed, nil
}

// The previous public speaker yields the floor. A member who passed on this
// exact input is not asked again until another public message arrives.
const candidateFilter = `a.enabled=1
 AND a.id!=COALESCE((SELECT sender_id FROM messages WHERE id=r.message_id),'')
 AND NOT EXISTS(SELECT 1 FROM runs p WHERE p.chain_id=r.chain_id AND p.message_id=r.message_id AND p.agent_id=a.id AND p.silent=1 AND p.status='completed')`

func (s *Store) DiscussionCandidates(runID string) ([]Member, error) {
	rows, err := s.db.Query(`SELECT a.id,a.name,a.description,a.enabled FROM runs r JOIN conversation_members m ON m.conversation_id=r.conversation_id JOIN agents a ON a.id=m.agent_id WHERE r.id=? AND `+candidateFilter+` ORDER BY m.position`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	members := []Member{}
	for rows.Next() {
		var m Member
		if err = rows.Scan(&m.ID, &m.Name, &m.Description, &m.Enabled); err != nil {
			return nil, err
		}
		members = append(members, m)
	}
	return members, rows.Err()
}

func (s *Store) SelectDiscussionSpeaker(runID, agentID, newID string) (ConversationRun, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return ConversationRun{}, err
	}
	defer tx.Rollback()
	r, err := scanConversationRun(tx.QueryRow(`SELECT `+runColumns+` FROM runs WHERE id=?`, runID))
	if err != nil {
		return r, err
	}
	child, err := scanConversationRun(tx.QueryRow(`SELECT `+runColumns+` FROM runs WHERE parent_run_id=?`, runID))
	if err == nil {
		if child.AgentID != agentID {
			return child, ValidationError("本次已选择发言人，不能重复安排")
		}
		return child, nil
	}
	if err != sql.ErrNoRows {
		return child, err
	}
	var name string
	err = tx.QueryRow(`SELECT a.name FROM runs r JOIN chains c ON c.id=r.chain_id JOIN conversation_members m ON m.conversation_id=r.conversation_id JOIN agents a ON a.id=m.agent_id WHERE r.id=? AND r.kind='selector' AND r.status='running' AND r.silent=0 AND c.action='discussion' AND c.status='active' AND a.id=? AND `+candidateFilter, runID, agentID).Scan(&name)
	if err == sql.ErrNoRows {
		return child, ValidationError("发言候选或讨论状态已改变，请重新发送消息")
	}
	if err != nil {
		return child, err
	}
	result, err := tx.Exec(`UPDATE chains SET reserved=reserved+1 WHERE id=? AND status='active' AND reserved<?`, r.ChainID, ChainLimit)
	if err != nil {
		return child, err
	}
	if n, _ := result.RowsAffected(); n != 1 {
		return child, ValidationError("本轮讨论已达上限")
	}
	_, err = tx.Exec(`INSERT INTO runs(id,conversation_id,agent_id,message_id,status,error,created_at,agent_name,chain_id,parent_run_id,previous_run_id) VALUES(?,?,?,?,'queued','',?,?,?,?,?)`, newID, r.ConversationID, agentID, r.MessageID, timestamp(), name, r.ChainID, r.ID, r.ID)
	if err != nil {
		return child, err
	}
	child, err = scanConversationRun(tx.QueryRow(`SELECT `+runColumns+` FROM runs WHERE id=?`, newID))
	if err != nil {
		return child, err
	}
	return child, tx.Commit()
}

func (s *Store) PassDiscussion(runID string) (bool, error) {
	result, err := s.db.Exec(`UPDATE runs SET silent=1 WHERE id=? AND status='running' AND chain_id IN (SELECT id FROM chains WHERE status='active' AND action='discussion') AND NOT EXISTS(SELECT 1 FROM runs p WHERE p.parent_run_id=runs.id)`, runID)
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	return n == 1, err
}

func continueDiscussion(tx *sql.Tx, c Chain, r ConversationRun) error {
	reason := ""
	var selections int
	if err := tx.QueryRow(`SELECT count(*) FROM runs WHERE chain_id=? AND kind='selector'`, c.ID).Scan(&selections); err != nil {
		return err
	}
	if r.Kind == "selector" {
		reason = "暂时没有新的补充，等你继续"
	} else if c.Reserved >= ChainLimit {
		reason = "本轮发言已达上限，等你继续"
	} else if selections >= DiscussionSelectionLimit {
		reason = "本轮讨论暂歇，等你继续"
	}
	if reason != "" {
		_, err := tx.Exec(`UPDATE chains SET status='completed',reason=? WHERE id=? AND status='active'`, reason, c.ID)
		return err
	}
	var connectionID, name, triggerID string
	err := tx.QueryRow(`SELECT a.id,a.name FROM conversation_members m JOIN agents a ON a.id=m.agent_id WHERE m.conversation_id=? AND a.enabled=1 ORDER BY m.position LIMIT 1`, c.ConversationID).Scan(&connectionID, &name)
	if err == sql.ErrNoRows {
		_, err = tx.Exec(`UPDATE chains SET status='completed',reason='当前没有启用成员，讨论暂歇' WHERE id=?`, c.ID)
		return err
	}
	if err != nil {
		return err
	}
	if err = tx.QueryRow(`SELECT id FROM messages WHERE conversation_id=? ORDER BY sequence DESC LIMIT 1`, c.ConversationID).Scan(&triggerID); err != nil {
		return err
	}
	_, err = tx.Exec(`INSERT INTO runs(id,conversation_id,agent_id,message_id,status,error,created_at,agent_name,chain_id,parent_run_id,previous_run_id,kind) VALUES(?,?,?,?,'queued','',?,?,?,?,?,'selector')`, "select_"+r.ID, c.ConversationID, connectionID, triggerID, timestamp(), name, c.ID, r.ID, r.ID)
	return err
}
