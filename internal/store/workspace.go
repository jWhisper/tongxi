package store

import (
	"database/sql"
	"encoding/json"
	"time"
)

type ValidationError string

func (e ValidationError) Error() string { return string(e) }

type Agent struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Instruction string   `json:"instruction"`
	BaseURL     string   `json:"baseURL"`
	Model       string   `json:"model"`
	KeyRef      string   `json:"-"`
	HasKey      bool     `json:"hasKey"`
	Tools       []string `json:"tools"`
	Enabled     bool     `json:"enabled"`
	Version     int      `json:"version"`
	CreatedAt   string   `json:"createdAt"`
	UpdatedAt   string   `json:"updatedAt"`
}

type Conversation struct {
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	Kind        string   `json:"kind"`
	Mode        string   `json:"mode"`
	LeadAgentID string   `json:"leadAgentID"`
	MemberIDs   []string `json:"memberIDs"`
	CreatedAt   string   `json:"createdAt"`
	UpdatedAt   string   `json:"updatedAt"`
}

type Message struct {
	TargetAgentID    string `json:"targetAgentID"`
	ReplyToMessageID string `json:"replyToMessageID"`
	SourceRunID      string `json:"sourceRunID"`
	ID               string `json:"id"`
	ConversationID   string `json:"conversationID"`
	Sequence         int    `json:"sequence"`
	SenderType       string `json:"senderType"`
	SenderID         string `json:"senderID"`
	SenderName       string `json:"senderName"`
	Content          string `json:"content"`
	CreatedAt        string `json:"createdAt"`
}

type ConversationRun struct {
	Kind           string   `json:"kind"`
	Silent         bool     `json:"silent"`
	RetryOf        string   `json:"retryOf"`
	ChainID        string   `json:"chainID"`
	ParentRunID    string   `json:"parentRunID"`
	PreviousRunID  string   `json:"previousRunID"`
	ID             string   `json:"id"`
	ConversationID string   `json:"conversationID"`
	AgentID        string   `json:"agentID"`
	MessageID      string   `json:"messageID"`
	Status         string   `json:"status"`
	Error          string   `json:"error"`
	CreatedAt      string   `json:"createdAt"`
	Text           string   `json:"text"`
	Tools          []string `json:"tools"`
	StartedAt      string   `json:"startedAt"`
	FinishedAt     string   `json:"finishedAt"`
	Revision       int      `json:"revision"`
	AgentName      string   `json:"agentName"`
}

func timestamp() string { return time.Now().UTC().Format(time.RFC3339Nano) }

func scanAgent(row interface{ Scan(...any) error }) (Agent, error) {
	var a Agent
	var tools string
	err := row.Scan(&a.ID, &a.Name, &a.Description, &a.Instruction, &a.BaseURL, &a.Model, &a.KeyRef, &tools, &a.Enabled, &a.Version, &a.CreatedAt, &a.UpdatedAt)
	if err == sql.ErrNoRows {
		return a, ValidationError("角色不存在")
	}
	if err != nil {
		return a, err
	}
	err = json.Unmarshal([]byte(tools), &a.Tools)
	a.HasKey = a.KeyRef != ""
	return a, err
}

func (s *Store) Agents() ([]Agent, error) {
	rows, err := s.db.Query(`SELECT * FROM agents ORDER BY created_at,id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []Agent{}
	for rows.Next() {
		a, err := scanAgent(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, a)
	}
	return list, rows.Err()
}
func (s *Store) Agent(id string) (Agent, error) {
	return scanAgent(s.db.QueryRow(`SELECT * FROM agents WHERE id=?`, id))
}

func (s *Store) SaveAgent(a Agent) (Agent, error) {
	tools, err := json.Marshal(a.Tools)
	if err != nil {
		return a, err
	}
	now := timestamp()
	if a.Version == 0 {
		_, err = s.db.Exec(`INSERT INTO agents VALUES(?,?,?,?,?,?,?,?,?,1,?,?)`, a.ID, a.Name, a.Description, a.Instruction, a.BaseURL, a.Model, a.KeyRef, string(tools), a.Enabled, now, now)
	} else {
		var result sql.Result
		result, err = s.db.Exec(`UPDATE agents SET name=?,description=?,instruction=?,base_url=?,model=?,key_ref=?,tools=?,enabled=?,version=version+1,updated_at=? WHERE id=? AND version=?`, a.Name, a.Description, a.Instruction, a.BaseURL, a.Model, a.KeyRef, string(tools), a.Enabled, now, a.ID, a.Version)
		if err == nil {
			n, _ := result.RowsAffected()
			if n != 1 {
				return a, ValidationError("角色已被修改，请重新打开后编辑")
			}
		}
	}
	if err != nil {
		return a, err
	}
	return s.Agent(a.ID)
}

func (s *Store) SetAgentEnabled(id string, enabled bool) (Agent, error) {
	a, _, err := s.SetAgentEnabledAndCancel(id, enabled, nil)
	return a, err
}

func (s *Store) KeyReferenced(ref string) (bool, error) {
	var count int
	err := s.db.QueryRow(`SELECT (SELECT count(*) FROM model_settings WHERE key_ref=?)+(SELECT count(*) FROM agents WHERE key_ref=?)`, ref, ref).Scan(&count)
	return count > 0, err
}

func (s *Store) Conversations() ([]Conversation, error) {
	rows, err := s.db.Query(`SELECT id,title,kind,mode,COALESCE(lead_agent_id,''),created_at,updated_at FROM conversations ORDER BY updated_at DESC,id`)
	if err != nil {
		return nil, err
	}
	list := []Conversation{}
	for rows.Next() {
		var c Conversation
		if err = rows.Scan(&c.ID, &c.Title, &c.Kind, &c.Mode, &c.LeadAgentID, &c.CreatedAt, &c.UpdatedAt); err != nil {
			rows.Close()
			return nil, err
		}
		list = append(list, c)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	for i := range list {
		list[i].MemberIDs, err = s.members(list[i].ID)
		if err != nil {
			return nil, err
		}
	}
	return list, nil
}
func (s *Store) members(id string) ([]string, error) {
	rows, err := s.db.Query(`SELECT agent_id FROM conversation_members WHERE conversation_id=? ORDER BY position`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
func (s *Store) Conversation(id string) (Conversation, error) {
	var c Conversation
	err := s.db.QueryRow(`SELECT id,title,kind,mode,COALESCE(lead_agent_id,''),created_at,updated_at FROM conversations WHERE id=?`, id).Scan(&c.ID, &c.Title, &c.Kind, &c.Mode, &c.LeadAgentID, &c.CreatedAt, &c.UpdatedAt)
	if err == sql.ErrNoRows {
		return c, ValidationError("会话不存在")
	}
	if err != nil {
		return c, err
	}
	c.MemberIDs, err = s.members(id)
	return c, err
}

func (s *Store) SaveConversation(c Conversation, create bool) (Conversation, error) {
	if (c.Kind != "private" && c.Kind != "group") || (c.Mode != "lead" && c.Mode != "discussion") {
		return c, ValidationError("请选择有效的会话类型与协作方式")
	}
	if len(c.MemberIDs) == 0 || (c.Kind == "private" && (len(c.MemberIDs) != 1 || c.Mode != "lead")) || (c.Kind == "group" && len(c.MemberIDs) < 2) {
		return c, ValidationError("私聊请选择一位角色，多人会话请至少选择两位")
	}
	seen := map[string]bool{}
	for _, id := range c.MemberIDs {
		if seen[id] {
			return c, ValidationError("不能重复选择同一角色")
		}
		seen[id] = true
	}
	if (c.Mode == "lead" && !seen[c.LeadAgentID]) || (c.Mode == "discussion" && c.LeadAgentID != "") {
		return c, ValidationError("主要助手必须是会话成员；讨论模式不设置主要助手")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return c, err
	}
	defer tx.Rollback()
	if !create {
		var count int
		if err = tx.QueryRow(`SELECT count(*) FROM conversations WHERE id=?`, c.ID).Scan(&count); err != nil {
			return c, err
		}
		if count == 0 {
			return c, ValidationError("会话不存在")
		}
		if err = tx.QueryRow(`SELECT count(*) FROM runs WHERE conversation_id=? AND status IN ('queued','running')`, c.ID).Scan(&count); err != nil {
			return c, err
		}
		if count > 0 {
			return c, ValidationError("请先结束会话中的待执行或运行中任务")
		}
	}
	for _, id := range c.MemberIDs {
		var enabled bool
		err = tx.QueryRow(`SELECT enabled FROM agents WHERE id=?`, id).Scan(&enabled)
		if err == sql.ErrNoRows {
			return c, ValidationError("所选角色不存在")
		}
		if err != nil {
			return c, err
		}
		if !enabled {
			var existing int
			if err = tx.QueryRow(`SELECT count(*) FROM conversation_members WHERE conversation_id=? AND agent_id=?`, c.ID, id).Scan(&existing); err != nil {
				return c, err
			}
			if create || existing == 0 {
				return c, ValidationError("不能将已停用的角色加入会话")
			}
			if c.LeadAgentID == id {
				return c, ValidationError("请选择启用中的角色作为主要助手")
			}
		}
	}
	now := timestamp()
	if create {
		_, err = tx.Exec(`INSERT INTO conversations(id,title,kind,mode,lead_agent_id,created_at,updated_at) VALUES(?,?,?,?,NULLIF(?,''),?,?)`, c.ID, c.Title, c.Kind, c.Mode, c.LeadAgentID, now, now)
	} else {
		_, err = tx.Exec(`UPDATE conversations SET title=?,kind=?,mode=?,lead_agent_id=NULLIF(?,''),updated_at=?,revision=revision+1 WHERE id=?`, c.Title, c.Kind, c.Mode, c.LeadAgentID, now, c.ID)
	}
	if err != nil {
		return c, err
	}
	if _, err = tx.Exec(`DELETE FROM conversation_members WHERE conversation_id=?`, c.ID); err != nil {
		return c, err
	}
	for i, id := range c.MemberIDs {
		if _, err = tx.Exec(`INSERT INTO conversation_members VALUES(?,?,?)`, c.ID, id, i); err != nil {
			return c, err
		}
	}
	if err = tx.Commit(); err != nil {
		return c, err
	}
	return s.Conversation(c.ID)
}

func (s *Store) AppendMessage(m Message) (Message, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return m, err
	}
	defer tx.Rollback()
	var exists int
	if err = tx.QueryRow(`SELECT count(*) FROM conversations WHERE id=?`, m.ConversationID).Scan(&exists); err != nil {
		return m, err
	}
	if exists == 0 {
		return m, ValidationError("会话不存在")
	}
	if m.SenderType == "agent" {
		err = tx.QueryRow(`SELECT a.name FROM agents a JOIN conversation_members cm ON cm.agent_id=a.id WHERE cm.conversation_id=? AND a.id=? AND a.enabled=1`, m.ConversationID, m.SenderID).Scan(&m.SenderName)
		if err == sql.ErrNoRows {
			return m, ValidationError("发言角色不存在、已停用或不属于本会话")
		}
		if err != nil {
			return m, err
		}
	} else if m.SenderType == "user" {
		m.SenderID = ""
		m.SenderName = "你"
	} else {
		return m, ValidationError("无效的发送者")
	}
	if err = tx.QueryRow(`SELECT COALESCE(MAX(sequence),0)+1 FROM messages WHERE conversation_id=?`, m.ConversationID).Scan(&m.Sequence); err != nil {
		return m, err
	}
	m.CreatedAt = timestamp()
	_, err = tx.Exec(`INSERT INTO messages(id,conversation_id,sequence,sender_type,sender_id,sender_name,content,created_at) VALUES(?,?,?,?,NULLIF(?,''),?,?,?)`, m.ID, m.ConversationID, m.Sequence, m.SenderType, m.SenderID, m.SenderName, m.Content, m.CreatedAt)
	if err != nil {
		return m, err
	}
	if _, err = tx.Exec(`UPDATE conversations SET updated_at=? WHERE id=?`, m.CreatedAt, m.ConversationID); err != nil {
		return m, err
	}
	return m, tx.Commit()
}
func (s *Store) Messages(id string) ([]Message, error) {
	rows, err := s.db.Query(`SELECT `+messageColumns+` FROM messages WHERE conversation_id=? ORDER BY sequence`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []Message{}
	for rows.Next() {
		m, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, m)
	}
	return list, rows.Err()
}

// Additional runs may share a trigger message (used by later group orchestration).
func (s *Store) CreateConversationRun(r ConversationRun) error {
	result, err := s.db.Exec(`INSERT INTO runs(id,conversation_id,agent_id,message_id,status,error,created_at) SELECT ?,?,?,?,'queued','',? WHERE EXISTS (
		SELECT 1 FROM agents a JOIN conversation_members cm ON a.id=cm.agent_id JOIN messages m ON m.conversation_id=cm.conversation_id
		WHERE a.id=? AND a.enabled=1 AND cm.conversation_id=? AND m.id=?)`, r.ID, r.ConversationID, r.AgentID, r.MessageID, timestamp(), r.AgentID, r.ConversationID, r.MessageID)
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n != 1 {
		return ValidationError("角色已停用，或角色、触发消息不属于该会话")
	}
	return nil
}
func (s *Store) ConversationRuns(id string) ([]ConversationRun, error) {
	rows, err := s.db.Query(`SELECT `+runColumns+` FROM runs WHERE conversation_id=? ORDER BY rowid`, id)
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
