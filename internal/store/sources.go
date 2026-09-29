package store

import (
	"database/sql"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

type Source struct {
	ID             string `json:"id"`
	ConversationID string `json:"conversationID"`
	Name           string `json:"name"`
	Kind           string `json:"kind"`
	Format         string `json:"format"`
	URL            string `json:"url"`
	Hash           string `json:"hash"`
	Size           int    `json:"size"`
	Segments       int    `json:"segments"`
	Characters     int    `json:"characters"`
	Note           string `json:"note"`
	CreatedAt      string `json:"createdAt"`
}
type SourceSegment struct {
	Link     string `json:"link,omitempty"`
	Number   int    `json:"number"`
	Location string `json:"location"`
	Content  string `json:"content"`
}
type SourcePage struct {
	Source   Source          `json:"source"`
	Segments []SourceSegment `json:"segments"`
	Next     int             `json:"next"`
}
type Citation struct {
	Name     string `json:"name,omitempty" jsonschema:"description=由系统记录，不需填写"`
	Location string `json:"location,omitempty" jsonschema:"description=由系统记录，不需填写"`
	SourceID string `json:"source_id" jsonschema:"description=已实际读取资料的完整 source.id"`
	Segment  int    `json:"segment" jsonschema:"description=read_source 或 read_web 返回的片段 number"`
	Quote    string `json:"quote" jsonschema:"description=支持结论的连续原文，必须确实存在于该片段，最多1000字；不能用自己的概括代替"`
}

const sourceColumns = `id,conversation_id,name,kind,format,url,hash,size,segments,characters,note,created_at`

func scanSource(row interface{ Scan(...any) error }) (Source, error) {
	var s Source
	err := row.Scan(&s.ID, &s.ConversationID, &s.Name, &s.Kind, &s.Format, &s.URL, &s.Hash, &s.Size, &s.Segments, &s.Characters, &s.Note, &s.CreatedAt)
	return s, err
}
func (s *Store) Sources(conversationID string) ([]Source, error) {
	rows, err := s.db.Query(`SELECT `+sourceColumns+` FROM sources WHERE conversation_id=? ORDER BY rowid`, conversationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Source{}
	for rows.Next() {
		v, e := scanSource(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func activeSourceRun(tx *sql.Tx, runID, conversationID string) error {
	if runID == "" {
		return nil
	}
	var valid bool
	err := tx.QueryRow(`SELECT EXISTS(SELECT 1 FROM runs r LEFT JOIN chains c ON c.id=r.chain_id WHERE r.id=? AND r.conversation_id=? AND r.status='running' AND (r.chain_id IS NULL OR c.status='active'))`, runID, conversationID).Scan(&valid)
	if err != nil {
		return err
	}
	if !valid {
		return ValidationError("本次执行已停止，资料操作未保存")
	}
	return nil
}

// Web sources and delivered artifacts retain their saved contents.
func (s *Store) SaveSource(v Source, segments []SourceSegment, runID string) (Source, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return Source{}, err
	}
	defer tx.Rollback()
	if err = activeSourceRun(tx, runID, v.ConversationID); err != nil {
		return Source{}, err
	}
	v, err = saveSource(tx, v, segments)
	if err != nil {
		return Source{}, err
	}
	return v, tx.Commit()
}

func saveSource(tx *sql.Tx, v Source, segments []SourceSegment) (Source, error) {
	old, err := scanSource(tx.QueryRow(`SELECT `+sourceColumns+` FROM sources WHERE conversation_id=? AND kind=? AND url=? AND name=? AND hash=?`, v.ConversationID, v.Kind, v.URL, v.Name, v.Hash))
	if err == nil {
		return old, nil
	}
	if err != sql.ErrNoRows {
		return Source{}, err
	}
	var count int
	if err = tx.QueryRow(`SELECT count(*) FROM sources WHERE conversation_id=?`, v.ConversationID).Scan(&count); err != nil {
		return Source{}, err
	}
	if count >= 100 {
		return Source{}, ValidationError("本会话已保存100份资料，请新建会话继续")
	}
	v.Segments = len(segments)
	v.Characters = 0
	if v.Segments == 0 {
		return Source{}, ValidationError("未找到可读取的文字")
	}
	for _, part := range segments {
		v.Characters += utf8.RuneCountInString(part.Content)
	}
	if v.Characters > 500000 {
		return Source{}, ValidationError("解析文字超过50万字，请拆分资料")
	}
	v.CreatedAt = timestamp()
	_, err = tx.Exec(`INSERT INTO sources(`+sourceColumns+`) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, v.ID, v.ConversationID, v.Name, v.Kind, v.Format, v.URL, v.Hash, v.Size, v.Segments, v.Characters, v.Note, v.CreatedAt)
	if err != nil {
		return Source{}, err
	}
	for i, part := range segments {
		if _, err = tx.Exec(`INSERT INTO source_segments VALUES(?,?,?,?)`, v.ID, i+1, part.Location, part.Content); err != nil {
			return Source{}, err
		}
	}
	return v, nil
}

// UI reads do not constitute model access. Tool reads record exactly the
// returned segments, scoped to the running conversation/chain.
func (s *Store) ReadSource(conversationID, sourceID string, start int, runID string) (SourcePage, error) {
	out := SourcePage{Segments: []SourceSegment{}}
	tx, err := s.db.Begin()
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	if err = activeSourceRun(tx, runID, conversationID); err != nil {
		return out, err
	}
	out.Source, err = scanSource(tx.QueryRow(`SELECT `+sourceColumns+` FROM sources WHERE id=? AND conversation_id=?`, sourceID, conversationID))
	if err == sql.ErrNoRows {
		return out, ValidationError("当前会话中不存在这份资料")
	}
	if err != nil {
		return out, err
	}
	if start == 0 {
		start = 1
	}
	if start < 1 || start > out.Source.Segments {
		return out, ValidationError("资料片段序号超出范围")
	}
	rows, err := tx.Query(`SELECT number,location,content FROM source_segments WHERE source_id=? AND number>=? ORDER BY number LIMIT 10`, sourceID, start)
	if err != nil {
		return out, err
	}
	total := 0
	for rows.Next() {
		var part SourceSegment
		if err = rows.Scan(&part.Number, &part.Location, &part.Content); err != nil {
			rows.Close()
			return out, err
		}
		part.Link = fmt.Sprintf("source://%s/%d", sourceID, part.Number)
		n := utf8.RuneCountInString(part.Content)
		if total+n > 12000 && len(out.Segments) > 0 {
			break
		}
		total += n
		out.Segments = append(out.Segments, part)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	if err = recordSourceRead(tx, out, runID); err != nil {
		return out, err
	}
	last := out.Segments[len(out.Segments)-1].Number
	if last < out.Source.Segments {
		out.Next = last + 1
	}
	return out, tx.Commit()
}

var citationURL = regexp.MustCompile(`source://([a-f0-9]{32})/([1-9][0-9]*)`)
var anyCitationURL = regexp.MustCompile(`source://[^\s)<>\]]+`)

func normalizeQuote(s string) string { return strings.Join(strings.Fields(s), " ") }
func validateCitations(tx *sql.Tx, c Chain, step LeadStep) error {
	if len(step.Citations) > 40 {
		return ValidationError("单次成果最多引用40个原文片段")
	}
	declared := map[string]bool{}
	for i := range step.Citations {
		cite := &step.Citations[i]
		key := fmt.Sprintf("source://%s/%d", cite.SourceID, cite.Segment)
		if declared[key] {
			return ValidationError("同一资料片段无需重复登记引用")
		}
		declared[key] = true
		quote := normalizeQuote(cite.Quote)
		if quote == "" || utf8.RuneCountInString(quote) > 1000 {
			return ValidationError("引用摘录应为1–1000字的连续原文")
		}
		// Use what this chain actually read, even if the file changed afterwards.
		rows, err := tx.Query(`SELECT s.name,sr.location,sr.content FROM source_reads sr JOIN sources s ON s.id=sr.source_id JOIN runs r ON r.id=sr.run_id WHERE r.chain_id=? AND s.conversation_id=? AND sr.source_id=? AND sr.segment=? ORDER BY sr.rowid DESC`, c.ID, c.ConversationID, cite.SourceID, cite.Segment)
		if err != nil {
			return err
		}
		found := false
		for rows.Next() {
			var name, location, content string
			if err = rows.Scan(&name, &location, &content); err != nil {
				rows.Close()
				return err
			}
			if strings.Contains(normalizeQuote(content), quote) {
				cite.Name, cite.Location = name, location
				found = true
				break
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		if !found && c.Basis != nil && c.Basis.Work != nil {
			for _, old := range c.Basis.Work.Citations {
				if old.SourceID == cite.SourceID && old.Segment == cite.Segment && old.Quote == cite.Quote {
					*cite, found = old, true
					break
				}
			}
		}
		if !found {
			return ValidationError("引用必须来自本轮实际读取的连续原文，或基础版本已验证的摘录；请先读取对应文件或资料")
		}
	}
	for _, raw := range anyCitationURL.FindAllString(step.Result, -1) {
		m := citationURL.FindStringSubmatch(raw)
		if len(m) != 3 || m[0] != raw || !declared[raw] {
			return ValidationError("正文引用需使用 source://资料ID/片段号，并在 citations 登记对应原文")
		}
		if _, err := strconv.Atoi(m[2]); err != nil {
			return ValidationError("引用片段序号无效")
		}
	}
	return nil
}
