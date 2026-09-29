package store

import (
	"database/sql"
	"encoding/json"
	"strings"
	"unicode/utf8"
)

type WorkTask struct {
	ID              string `json:"id"`
	ConversationID  string `json:"conversationID"`
	Title           string `json:"title"`
	Goal            string `json:"goal"`
	CurrentChainID  string `json:"currentChainID"`
	LatestVersionID string `json:"latestVersionID"`
	Revision        int    `json:"revision"`
	CreatedAt       string `json:"createdAt"`
	UpdatedAt       string `json:"updatedAt"`
}

type WorkVersion struct {
	ID            string   `json:"id"`
	TaskID        string   `json:"taskID"`
	Number        int      `json:"number"`
	ChainID       string   `json:"chainID"`
	BaseVersionID string   `json:"baseVersionID"`
	Step          LeadStep `json:"step"`
	Request       string   `json:"request"`
	Summary       string   `json:"summary"`
	CreatedAt     string   `json:"createdAt"`
}

// A scheduled revision carries its own immutable input even after the current
// task advances. Explicit older-version selection uses that version's checks.
type WorkBasis struct {
	Task            WorkTask  `json:"task"`
	VersionID       string    `json:"versionID"`
	VersionNumber   int       `json:"versionNumber"`
	Work            *LeadStep `json:"work"`
	PreviousRequest string    `json:"previousRequest"`
	Explicit        bool      `json:"explicit"`
}

type RequirementChange struct {
	Index     int    `json:"index" jsonschema:"description=原验收条件的序号，从1开始；新增条件填0"`
	Criterion string `json:"criterion" jsonschema:"description=修改或新增后的条件全文；删除旧条件时留空"`
	Quote     string `json:"quote" jsonschema:"description=本次用户消息中明确授权此变动的连续原文，不得引用其他消息或编造"`
}

const taskColumns = `id,conversation_id,title,goal,current_chain_id,COALESCE(latest_version_id,''),revision,created_at,updated_at`
const versionColumns = `id,task_id,number,chain_id,COALESCE(base_version_id,''),step,request,summary,created_at`

func scanWorkTask(row interface{ Scan(...any) error }) (WorkTask, error) {
	var t WorkTask
	err := row.Scan(&t.ID, &t.ConversationID, &t.Title, &t.Goal, &t.CurrentChainID, &t.LatestVersionID, &t.Revision, &t.CreatedAt, &t.UpdatedAt)
	return t, err
}

func scanWorkVersion(row interface{ Scan(...any) error }) (WorkVersion, error) {
	var v WorkVersion
	var step string
	err := row.Scan(&v.ID, &v.TaskID, &v.Number, &v.ChainID, &v.BaseVersionID, &step, &v.Request, &v.Summary, &v.CreatedAt)
	if err == nil {
		err = json.Unmarshal([]byte(step), &v.Step)
	}
	return v, err
}

func (s *Store) WorkTasks(conversationID string) ([]WorkTask, error) {
	rows, err := s.db.Query(`SELECT `+taskColumns+` FROM work_tasks WHERE conversation_id=? ORDER BY updated_at,rowid`, conversationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []WorkTask{}
	for rows.Next() {
		t, err := scanWorkTask(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, t)
	}
	return items, rows.Err()
}

func (s *Store) WorkVersions(conversationID string) ([]WorkVersion, error) {
	rows, err := s.db.Query(`SELECT `+versionColumns+` FROM work_versions WHERE task_id IN (SELECT id FROM work_tasks WHERE conversation_id=?) ORDER BY rowid`, conversationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []WorkVersion{}
	for rows.Next() {
		v, err := scanWorkVersion(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, v)
	}
	return items, rows.Err()
}

func supersedeLead(tx *sql.Tx, conversationID string, active *ConversationRun) ([]ConversationRun, error) {
	rows, err := tx.Query(`SELECT id FROM chains WHERE conversation_id=? AND action='lead' AND status='active'`, conversationID)
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
		runs, err := stopChain(tx, id, active, "收到新的用户要求，停止旧安排并保留草稿")
		if err != nil {
			return nil, err
		}
		changed = append(changed, runs...)
	}
	return changed, nil
}

func shortTitle(text string) string {
	runes := []rune(strings.TrimSpace(text))
	if len(runes) > 60 {
		runes = runes[:60]
	}
	return string(runes)
}

func attachWorkTask(tx *sql.Tx, chainID string, in ScheduleRequest, now string) error {
	var task WorkTask
	var err error
	var basis *WorkBasis
	if in.BaseVersionID != "" {
		version, e := scanWorkVersion(tx.QueryRow(`SELECT `+versionColumns+` FROM work_versions WHERE id=?`, in.BaseVersionID))
		if e == sql.ErrNoRows {
			return ValidationError("成果版本不存在，请重新打开版本记录")
		}
		if e != nil {
			return e
		}
		task, err = scanWorkTask(tx.QueryRow(`SELECT `+taskColumns+` FROM work_tasks WHERE id=?`, version.TaskID))
		if err != nil {
			return err
		}
		if task.ConversationID != in.ConversationID {
			return ValidationError("不能使用其他会话的成果版本")
		}
		basis = &WorkBasis{Task: task, VersionID: version.ID, VersionNumber: version.Number, Work: &version.Step, PreviousRequest: version.Request, Explicit: true}
	} else {
		task, err = scanWorkTask(tx.QueryRow(`SELECT `+taskColumns+` FROM work_tasks WHERE conversation_id=? ORDER BY updated_at DESC,rowid DESC LIMIT 1`, in.ConversationID))
		if err != nil && err != sql.ErrNoRows {
			return err
		}
		if err == nil {
			previous, e := scanChain(tx.QueryRow(`SELECT `+chainColumns+` FROM chains WHERE id=?`, task.CurrentChainID))
			if e != nil {
				return e
			}
			basis = &WorkBasis{Task: task, VersionID: task.LatestVersionID, Work: previous.Work}
			if basis.Work == nil && previous.Basis != nil {
				basis.Work = previous.Basis.Work
			}
			if e = tx.QueryRow(`SELECT content FROM messages WHERE id=?`, previous.MessageID).Scan(&basis.PreviousRequest); e != nil {
				return e
			}
			if task.LatestVersionID != "" {
				if e = tx.QueryRow(`SELECT number FROM work_versions WHERE id=?`, task.LatestVersionID).Scan(&basis.VersionNumber); e != nil {
					return e
				}
			}
		}
	}
	if basis == nil {
		task = WorkTask{ID: chainID, ConversationID: in.ConversationID, Title: shortTitle(in.Content), Goal: in.Content, CurrentChainID: chainID, Revision: 1, CreatedAt: now, UpdatedAt: now}
		_, err = tx.Exec(`INSERT INTO work_tasks(id,conversation_id,title,goal,current_chain_id,revision,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?)`, task.ID, task.ConversationID, task.Title, task.Goal, chainID, task.Revision, now, now)
	} else {
		task.Revision++
		_, err = tx.Exec(`UPDATE work_tasks SET current_chain_id=?,revision=?,updated_at=? WHERE id=?`, chainID, task.Revision, now, task.ID)
	}
	if err != nil {
		return err
	}
	data, _ := json.Marshal(basis)
	base := ""
	if basis != nil {
		base = basis.VersionID
	}
	_, err = tx.Exec(`UPDATE chains SET task_id=?,base_version_id=NULLIF(?,''),task_revision=?,basis=?,revision_policy=1 WHERE id=?`, task.ID, base, task.Revision, string(data), chainID)
	return err
}

func checkTaskCurrent(tx *sql.Tx, c Chain) error {
	if c.TaskID == "" {
		return nil
	}
	var current bool
	if err := tx.QueryRow(`SELECT EXISTS(SELECT 1 FROM work_tasks WHERE id=? AND current_chain_id=? AND revision=?)`, c.TaskID, c.ID, c.TaskRevision).Scan(&current); err != nil {
		return err
	}
	if !current {
		return ValidationError("这份成果已有新的修改，请基于当前版本继续")
	}
	return nil
}

func routeWorkRevision(tx *sql.Tx, c *Chain, step *LeadStep) error {
	if err := checkTaskCurrent(tx, *c); err != nil {
		return err
	}
	if c.RevisionPolicy == 0 {
		return nil
	}
	if utf8.RuneCountInString(step.Brief) > 8000 || utf8.RuneCountInString(step.ChangeSummary) > 1000 || utf8.RuneCountInString(step.Title) > 100 || len(step.Questions) > 6 {
		return ValidationError("任务说明、修改摘要或待补充问题过长")
	}
	for _, q := range step.Questions {
		if strings.TrimSpace(q) == "" || utf8.RuneCountInString(q) > 500 {
			return ValidationError("请简洁说明待补充的问题")
		}
	}
	if c.Work != nil {
		step.WorkMode = c.Work.WorkMode
		if step.ChangeSummary == "" {
			step.ChangeSummary = c.Work.ChangeSummary
		}
		if step.Brief == "" {
			step.Brief = c.Work.Brief
		}
		return nil
	}
	var request string
	if err := tx.QueryRow(`SELECT content FROM messages WHERE id=?`, c.MessageID).Scan(&request); err != nil {
		return err
	}
	if step.WorkMode == "" {
		step.WorkMode = "new"
		if c.Basis != nil {
			step.WorkMode = "continue"
		}
	}
	if c.Basis != nil && c.Basis.Explicit && step.WorkMode != "continue" {
		return ValidationError("用户已选定历史版本，请在该版本基础上继续修改")
	}
	switch step.WorkMode {
	case "continue":
		if c.Basis == nil {
			return ValidationError("没有可继续的任务，请创建新任务")
		}
		if c.Basis.Work != nil {
			if err := validateRequirementChanges(c.Basis.Work.Checks, step.Checks, step.Changes, request); err != nil {
				return err
			}
		}
	case "new", "clarify":
		if step.WorkMode == "clarify" && (step.Action != "pause" || strings.TrimSpace(step.Result) != "") {
			return ValidationError("指向不清时请暂停并提问，不要修改已有成果")
		}
		if c.Basis != nil {
			previous := c.Basis.Task
			if _, err := tx.Exec(`UPDATE work_tasks SET current_chain_id=?,revision=?,updated_at=? WHERE id=?`, previous.CurrentChainID, previous.Revision, previous.UpdatedAt, previous.ID); err != nil {
				return err
			}
		}
		if step.WorkMode == "clarify" {
			// No artifact is attached to an unresolved question.
			if c.Basis == nil {
				return ValidationError("当前没有候选成果，请按新任务说明缺少的资料")
			}
			c.TaskID, c.BaseVersionID = "", ""
			_, err := tx.Exec(`UPDATE chains SET task_id=NULL,base_version_id=NULL WHERE id=?`, c.ID)
			return err
		}
		title := strings.TrimSpace(step.Title)
		if title == "" {
			title = shortTitle(request)
		}
		if c.Basis != nil {
			c.TaskID, c.BaseVersionID, c.TaskRevision = c.ID, "", 1
			if _, err := tx.Exec(`INSERT INTO work_tasks(id,conversation_id,title,goal,current_chain_id,revision,created_at,updated_at) VALUES(?,?,?,?,?,1,?,?)`, c.ID, c.ConversationID, title, request, c.ID, c.CreatedAt, c.CreatedAt); err != nil {
				return err
			}
		} else {
			if _, err := tx.Exec(`UPDATE work_tasks SET title=? WHERE id=?`, title, c.TaskID); err != nil {
				return err
			}
		}
		c.Basis = nil
		if _, err := tx.Exec(`UPDATE chains SET task_id=?,base_version_id=NULL,task_revision=?,basis='null' WHERE id=?`, c.TaskID, c.TaskRevision, c.ID); err != nil {
			return err
		}
	default:
		return ValidationError("请选择 continue 续改、new 新任务或 clarify 澄清指向")
	}
	if step.ChangeSummary == "" {
		step.ChangeSummary = shortTitle(request)
	}
	return nil
}

func validateRequirementChanges(before, after []AcceptanceCheck, changes []RequirementChange, request string) error {
	edits := map[int]string{}
	additions := []string{}
	for _, change := range changes {
		quote := strings.TrimSpace(change.Quote)
		if quote == "" || !strings.Contains(request, quote) {
			return ValidationError("修改验收条件必须引用本次用户消息中的连续原文")
		}
		if change.Index == 0 {
			if strings.TrimSpace(change.Criterion) == "" {
				return ValidationError("新增条件不能为空")
			}
			additions = append(additions, change.Criterion)
			continue
		}
		if change.Index < 1 || change.Index > len(before) {
			return ValidationError("原验收条件序号不存在")
		}
		if _, ok := edits[change.Index]; ok {
			return ValidationError("同一验收条件不能重复修改")
		}
		edits[change.Index] = change.Criterion
	}
	expected := []string{}
	for i, check := range before {
		value, ok := edits[i+1]
		if !ok {
			value = check.Criterion
		}
		if value != "" {
			expected = append(expected, value)
		}
	}
	expected = append(expected, additions...)
	if len(expected) != len(after) {
		return ValidationError("请保留未被用户改变的验收条件，并逐项声明新增、修改或删除的依据")
	}
	for i, value := range expected {
		if after[i].Criterion != value {
			return ValidationError("未变的条件必须保持原文和顺序；修改条件请填写 changes 及用户原文依据")
		}
	}
	return nil
}

func publishWorkVersion(tx *sql.Tx, c Chain, step LeadStep, now string) error {
	if c.TaskID == "" || step.Action != "complete" {
		return nil
	}
	if err := checkTaskCurrent(tx, c); err != nil {
		return err
	}
	var request string
	if err := tx.QueryRow(`SELECT content FROM messages WHERE id=?`, c.MessageID).Scan(&request); err != nil {
		return err
	}
	data, _ := json.Marshal(step)
	id := "version_" + c.ID
	_, err := tx.Exec(`INSERT INTO work_versions(id,task_id,number,chain_id,base_version_id,step,request,summary,created_at)
		SELECT ?,?,COALESCE(MAX(number),0)+1,?,NULLIF(?,''),?,?,?,? FROM work_versions WHERE task_id=?`, id, c.TaskID, c.ID, c.BaseVersionID, string(data), request, step.ChangeSummary, now, c.TaskID)
	if err != nil {
		return err
	}
	_, err = tx.Exec(`UPDATE work_tasks SET latest_version_id=? WHERE id=?`, id, c.TaskID)
	return err
}
