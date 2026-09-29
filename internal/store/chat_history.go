package store

import (
	"database/sql"
	"strings"
)

type HistoryMessage struct {
	ID         string `json:"id"`
	Sequence   int    `json:"sequence"`
	Sender     string `json:"sender"`
	SenderType string `json:"senderType"`
	ReplyTo    string `json:"replyTo,omitempty"`
	CreatedAt  string `json:"createdAt"`
	Content    string `json:"content"`
	Next       int    `json:"nextOffset,omitempty"`
}

// Literal keyword search supports Chinese and never interprets % or _ as wildcards.
func (s *Store) SearchChatHistory(conversationID, query string, before int, pageSize ...int) ([]HistoryMessage, error) {
	limit := 400
	if len(pageSize) > 0 {
		limit = max(40, min(400, pageSize[0]))
	}
	query = strings.TrimSpace(query)
	if query == "" || len([]rune(query)) > 200 {
		return nil, ValidationError("搜索词请输入 1–200 个字符")
	}
	rows, err := s.db.Query(`SELECT id,sequence,sender_name,sender_type,COALESCE(reply_to_message_id,''),created_at,
		substr(content,MAX(1,instr(lower(content),lower(?))-20),?)
		FROM messages WHERE conversation_id=? AND instr(lower(content),lower(?))>0 AND (?=0 OR sequence<?)
		ORDER BY sequence DESC LIMIT 20`, query, limit, conversationID, query, before, before)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []HistoryMessage{}
	for rows.Next() {
		var m HistoryMessage
		if err = rows.Scan(&m.ID, &m.Sequence, &m.Sender, &m.SenderType, &m.ReplyTo, &m.CreatedAt, &m.Content); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// Anchor lookup includes conversation scope; a foreign message ID cannot reveal its neighbours.
func (s *Store) ReadChatHistory(conversationID, id string, sequence, radius, offset int, pageSize ...int) ([]HistoryMessage, error) {
	limit := 4000
	if len(pageSize) > 0 {
		limit = max(128, min(4000, pageSize[0]))
	}
	if offset < 0 || radius < 0 || radius > 3 {
		return nil, ValidationError("offset 不能为负数，前后文范围为 0–3 条")
	}
	if id != "" {
		if err := s.db.QueryRow(`SELECT sequence FROM messages WHERE conversation_id=? AND id=?`, conversationID, id).Scan(&sequence); err != nil {
			if err == sql.ErrNoRows {
				return nil, ValidationError("本会话没有这条消息")
			}
			return nil, err
		}
	}
	if sequence < 1 {
		return nil, ValidationError("请提供本会话的消息 ID 或序号")
	}
	rows, err := s.db.Query(`SELECT id,sequence,sender_name,sender_type,COALESCE(reply_to_message_id,''),created_at,
		CASE WHEN sequence=? THEN substr(content,?,?) ELSE substr(content,1,?) END,length(content)
		FROM messages WHERE conversation_id=? AND sequence BETWEEN ? AND ? ORDER BY sequence`, sequence, offset+1, limit, min(400, limit), conversationID, sequence-radius, sequence+radius)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []HistoryMessage{}
	for rows.Next() {
		var m HistoryMessage
		var size int
		if err = rows.Scan(&m.ID, &m.Sequence, &m.Sender, &m.SenderType, &m.ReplyTo, &m.CreatedAt, &m.Content, &size); err != nil {
			return nil, err
		}
		end := min(400, limit)
		if m.Sequence == sequence {
			end = offset + limit
		}
		if size > end {
			m.Next = end
		}
		out = append(out, m)
	}
	return out, rows.Err()
}
