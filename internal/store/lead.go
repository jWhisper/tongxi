package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"time"
	"unicode/utf8"
)

const LeadLimit = 13
const LeadDuration = 10 * time.Minute

type AcceptanceCheck struct {
	Criterion string `json:"criterion" jsonschema:"description=具体且可检查的验收条件，首次确定后保持原文和顺序"`
	Status    string `json:"status" jsonschema:"description=pending 待检查、unmet 未满足、met 已满足"`
	Evidence  string `json:"evidence" jsonschema:"description=与当前成果对应的检查依据或具体缺口，不得虚构验证"`
}

type LeadStep struct {
	Files         []ArtifactDelivery  `json:"files,omitempty" jsonschema:"description=本次成果要交付的真实文件及核对依据；选择最新文件替换旧文件，未变文件可保留基础版本ID。先read_artifact完整读取再核对，不能仅凭脚本退出码交付"`
	Citations     []Citation          `json:"citations,omitempty" jsonschema:"description=成果采用的真实资料依据。引用需先实际读取，填写 source_id、segment、连续原文 quote；正文使用 [来源名称](source://source_id/segment)，每个片段登记一次"`
	WorkMode      string              `json:"work_mode,omitempty" jsonschema:"description=本次用户消息首次提交时选择 continue 续改当前任务、new 明确新任务、clarify 指向不清先提问；后续轮次沿用"`
	Title         string              `json:"title,omitempty" jsonschema:"description=new 时给出简短任务名称，continue 不改标题"`
	Brief         string              `json:"brief,omitempty" jsonschema:"description=当前任务目标、有效约束与用户已确认信息的完整简要记录，续改时合并新信息而不丢失旧要求，最多8000字"`
	ChangeSummary string              `json:"change_summary,omitempty" jsonschema:"description=本次相对基础版本的具体修改摘要，不是空泛总结"`
	Changes       []RequirementChange `json:"changes,omitempty" jsonschema:"description=首次续改时逐项声明用户授权的验收条件变动；未变化的条件保持原文和顺序"`
	Questions     []string            `json:"questions,omitempty" jsonschema:"description=暂停等待用户时尚缺的具体信息，资料齐全则留空，不重复询问已确认信息"`
	Action        string              `json:"action" jsonschema:"description=delegate 委托一位成员、complete 交付已达标成果、pause 保留成果并说明未完成"`
	Checks        []AcceptanceCheck   `json:"checks" jsonschema:"description=1至6条验收条件及检查结果，每次保留所有条件"`
	Result        string              `json:"result" jsonschema:"description=当前完整可用成果，持续更新同一份内容；首次分工可为空，交付时不得为空"`
	Reason        string              `json:"reason" jsonschema:"description=本次检查结论、下一步要解决的缺口或暂停原因"`
	NextAgentID   string              `json:"next_agent_id" jsonschema:"description=delegate 时必须填一位启用成员的准确 ID，其他动作留空"`
	Task          string              `json:"task" jsonschema:"description=给下一位的具体任务和检查重点，delegate 时必填，其他动作留空"`
}

type LeadAdvance struct {
	Step LeadStep
	Run  *ConversationRun
}

func (step LeadStep) PublicText() string {
	var b strings.Builder
	if step.Result != "" {
		b.WriteString(step.Result + "\n\n")
	}
	b.WriteString("验收检查（主要助手自检）：\n")
	for _, check := range step.Checks {
		label := map[string]string{"met": "已满足", "unmet": "未满足", "pending": "待检查"}[check.Status]
		fmt.Fprintf(&b, "- %s：%s", check.Criterion, label)
		if check.Evidence != "" {
			b.WriteString("；" + check.Evidence)
		}
		b.WriteString("\n")
	}
	b.WriteString("\n" + step.Reason)
	if len(step.Questions) > 0 {
		b.WriteString("\n\n待补充信息：\n")
		for _, q := range step.Questions {
			b.WriteString("- " + q + "\n")
		}
	}
	if step.Action == "pause" {
		b.WriteString("\n\n本次尚未完成，以上为当前成果。")
	}
	return b.String()
}

// One atomic decision per lead run binds assessment, public delegation and the
// next member. A repeated call can never broadcast or replace its decision.
func (s *Store) AdvanceLead(runID string, step LeadStep, messageID, nextID string) (LeadAdvance, error) {
	var out LeadAdvance
	payload, err := json.Marshal(step)
	if err != nil {
		return out, err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	var old, saved, childID string
	err = tx.QueryRow(`SELECT payload,step,COALESCE(next_run_id,'') FROM lead_steps WHERE run_id=?`, runID).Scan(&old, &saved, &childID)
	if err == nil {
		if old != string(payload) {
			return out, ValidationError("本次已提交安排，不能重复选人或更改验收决定")
		}
		if err = json.Unmarshal([]byte(saved), &out.Step); err != nil {
			return out, err
		}
		if childID != "" {
			r, e := scanConversationRun(tx.QueryRow(`SELECT `+runColumns+` FROM runs WHERE id=?`, childID))
			out.Run = &r
			return out, e
		}
		return out, nil
	}
	if err != sql.ErrNoRows {
		return out, err
	}
	r, err := scanConversationRun(tx.QueryRow(`SELECT `+runColumns+` FROM runs WHERE id=?`, runID))
	if err != nil {
		return out, err
	}
	c, err := scanChain(tx.QueryRow(`SELECT `+chainColumns+` FROM chains WHERE id=?`, r.ChainID))
	if err != nil {
		return out, err
	}
	if c.LeadPolicy != 1 || c.Status != "active" || r.Status != "running" || r.AgentID != c.LeadAgentID {
		return out, ValidationError("仅当前执行中的主要助手可以提交验收和下一步安排")
	}
	if err = routeWorkRevision(tx, &c, &step); err != nil {
		return out, err
	}
	if err = validateLeadStep(step, c.Work); err != nil {
		return out, err
	}
	if err = validateCitations(tx, c, step); err != nil {
		return out, err
	}
	if err = validateArtifactDelivery(tx, c, step); err != nil {
		return out, err
	}
	stalled := 0
	if c.Work != nil && strings.TrimSpace(step.Result) == strings.TrimSpace(c.Work.Result) && reflect.DeepEqual(step.Files, c.Work.Files) {
		unchanged := true
		for i, check := range step.Checks {
			unchanged = unchanged && check.Status == c.Work.Checks[i].Status
		}
		if unchanged {
			stalled = c.Stalled + 1
		}
	}
	if step.Action == "delegate" {
		reason := ""
		started, err := time.Parse(time.RFC3339Nano, c.CreatedAt)
		if err != nil {
			return out, err
		}
		switch {
		case c.Reserved+2 > LeadLimit:
			reason = "已达本次执行预算，尚未完成验收"
		case time.Since(started) >= LeadDuration:
			reason = "已达本次协作时间预算，尚未完成验收"
		case stalled >= 2:
			reason = "连续两次检查未更新成果或验收状态，暂停等待补充"
		}
		if reason != "" {
			step.Action, step.NextAgentID, step.Task = "pause", "", ""
			step.Reason = reason + "。" + step.Reason
		}
	}
	if step.Action == "delegate" {
		if step.NextAgentID == r.AgentID {
			return out, ValidationError("请直接完善自己的成果，或选择另一位成员")
		}
		var name string
		err = tx.QueryRow(`SELECT a.name FROM agents a JOIN conversation_members m ON m.agent_id=a.id WHERE m.conversation_id=? AND a.id=? AND a.enabled=1`, r.ConversationID, step.NextAgentID).Scan(&name)
		if err == sql.ErrNoRows {
			return out, ValidationError("接收伙伴不存在、已停用或不属于本会话")
		}
		if err != nil {
			return out, err
		}
		m := Message{ID: messageID, ConversationID: r.ConversationID, SenderType: "agent", SenderID: r.AgentID, SenderName: r.AgentName, Content: step.Task, TargetAgentID: step.NextAgentID, SourceRunID: r.ID, ReplyToMessageID: c.MessageID, CreatedAt: timestamp()}
		if err = insertMessage(tx, &m); err != nil {
			return out, err
		}
		_, err = tx.Exec(`INSERT INTO runs(id,conversation_id,agent_id,message_id,status,error,created_at,agent_name,chain_id,parent_run_id,previous_run_id) VALUES(?,?,?,?,'queued','',?,?,?,?,?)`, nextID, r.ConversationID, step.NextAgentID, m.ID, timestamp(), name, c.ID, r.ID, r.ID)
		if err != nil {
			return out, err
		}
		child, err := scanConversationRun(tx.QueryRow(`SELECT `+runColumns+` FROM runs WHERE id=?`, nextID))
		if err != nil {
			return out, err
		}
		out.Run, childID = &child, child.ID
		if _, err = tx.Exec(`UPDATE chains SET reserved=reserved+1 WHERE id=?`, c.ID); err != nil {
			return out, err
		}
	}
	data, _ := json.Marshal(step)
	if _, err = tx.Exec(`INSERT INTO lead_steps VALUES(?,?,?,NULLIF(?,''))`, r.ID, string(payload), string(data), childID); err != nil {
		return out, err
	}
	if _, err = tx.Exec(`UPDATE chains SET work=?,stalled=? WHERE id=?`, string(data), stalled, c.ID); err != nil {
		return out, err
	}
	out.Step = step
	return out, tx.Commit()
}

func validateLeadStep(step LeadStep, prior *LeadStep) error {
	if len(step.Checks) == 0 || len(step.Checks) > 6 || strings.TrimSpace(step.Reason) == "" {
		return ValidationError("请给出 1–6 条验收条件和本次检查结论")
	}
	allMet := true
	for _, check := range step.Checks {
		if strings.TrimSpace(check.Criterion) == "" || utf8.RuneCountInString(check.Criterion) > 300 || utf8.RuneCountInString(check.Evidence) > 1000 {
			return ValidationError("验收条件不能为空或过长")
		}
		if check.Status != "pending" && check.Status != "unmet" && check.Status != "met" {
			return ValidationError("验收状态必须为 pending、unmet 或 met")
		}
		if check.Status == "met" && strings.TrimSpace(check.Evidence) == "" {
			return ValidationError("已满足的条件必须提供与当前成果对应的检查依据")
		}
		allMet = allMet && check.Status == "met"
	}
	if prior != nil {
		before, after := []string{}, []string{}
		for _, check := range prior.Checks {
			before = append(before, check.Criterion)
		}
		for _, check := range step.Checks {
			after = append(after, check.Criterion)
		}
		if !reflect.DeepEqual(before, after) {
			return ValidationError("请保持最初验收条件的原文、数量和顺序，不能降低或替换标准")
		}
	}
	if utf8.RuneCountInString(step.Result) > 32000 || utf8.RuneCountInString(step.Reason) > 2000 || utf8.RuneCountInString(step.Task) > 8000 {
		return ValidationError("成果或委托内容过长，请保留完整可用内容并精简重复表述")
	}
	switch step.Action {
	case "delegate":
		if step.NextAgentID == "" || strings.TrimSpace(step.Task) == "" {
			return ValidationError("请指定一位成员及其具体任务")
		}
	case "complete", "pause":
		if step.NextAgentID != "" || step.Task != "" {
			return ValidationError("交付或暂停时不能同时安排成员")
		}
		if step.Action == "complete" && (!allMet || strings.TrimSpace(step.Result) == "" || len(step.Questions) > 0) {
			return ValidationError("尚未满足全部验收条件或缺少完整成果，不能标为已交付")
		}
	default:
		return ValidationError("请选择 delegate、complete 或 pause")
	}
	return nil
}

func (s *Store) LeadStep(runID string) (*LeadStep, error) {
	var data string
	err := s.db.QueryRow(`SELECT step FROM lead_steps WHERE run_id=?`, runID).Scan(&data)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var step LeadStep
	err = json.Unmarshal([]byte(data), &step)
	return &step, err
}

// Only published decisions belong in the transcript. Delegation messages share
// a source run with its reply, but remain ordinary messages in the UI.
func (s *Store) PublishedLeadSteps(conversationID string) (map[string]LeadStep, error) {
	rows, err := s.db.Query(`SELECT l.run_id,l.step FROM lead_steps l JOIN runs r ON r.id=l.run_id
		WHERE r.conversation_id=? AND r.status='completed'`, conversationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	steps := make(map[string]LeadStep)
	for rows.Next() {
		var id, data string
		if err := rows.Scan(&id, &data); err != nil {
			return nil, err
		}
		var step LeadStep
		if err := json.Unmarshal([]byte(data), &step); err != nil {
			return nil, err
		}
		steps[id] = step
	}
	return steps, rows.Err()
}

func continueLead(tx *sql.Tx, c Chain, r ConversationRun, pending bool) error {
	if pending {
		return nil
	}
	if r.AgentID == c.LeadAgentID {
		var data string
		err := tx.QueryRow(`SELECT step FROM lead_steps WHERE run_id=?`, r.ID).Scan(&data)
		if err == sql.ErrNoRows {
			_, err = tx.Exec(`UPDATE chains SET status='incomplete',reason='主要助手未提交有效验收，当前内容尚未完成' WHERE id=?`, c.ID)
			return err
		}
		if err != nil {
			return err
		}
		var step LeadStep
		if err = json.Unmarshal([]byte(data), &step); err != nil {
			return err
		}
		status := "incomplete"
		if step.Action == "complete" {
			if err = publishWorkVersion(tx, c, step, r.FinishedAt); err != nil {
				return err
			}
			status = "completed"
		}
		_, err = tx.Exec(`UPDATE chains SET status=?,reason=? WHERE id=?`, status, step.Reason, c.ID)
		return err
	}
	if c.Reserved >= LeadLimit {
		_, err := tx.Exec(`UPDATE chains SET status='incomplete',reason='执行额度已用完，尚未完成验收' WHERE id=?`, c.ID)
		return err
	}
	_, err := tx.Exec(`INSERT INTO runs(id,conversation_id,agent_id,message_id,status,error,created_at,agent_name,chain_id,parent_run_id,previous_run_id) SELECT ?,?,?,?,'queued','',?,name,?,?,? FROM agents WHERE id=?`, "review_"+r.ID, r.ConversationID, c.LeadAgentID, c.MessageID, timestamp(), c.ID, r.ID, r.ID, c.LeadAgentID)
	if err != nil {
		return err
	}
	_, err = tx.Exec(`UPDATE chains SET reserved=reserved+1 WHERE id=?`, c.ID)
	return err
}
