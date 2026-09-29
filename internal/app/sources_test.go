package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"tongxi/internal/material"
	"tongxi/internal/store"
)

func TestEinoReadsActualSourceCitesAndExportsSelectedVersion(t *testing.T) {
	s, c, _ := groupService(t)
	dir := t.TempDir()
	file := filepath.Join(dir, "活动资料.txt")
	if err := os.WriteFile(file, []byte("报名人数12人，预算100元。\n全部环节可自愿跳过。"), 0600); err != nil {
		t.Fatal(err)
	}
	imported, err := s.ImportFiles(c.ID, []string{file, filepath.Join(dir, "missing.txt")})
	if err != nil || len(imported.Sources) != 1 || len(imported.Errors) != 1 {
		t.Fatal(imported, err)
	}
	source := imported.Sources[0]
	if err = os.Remove(file); err != nil {
		t.Fatal(err)
	}
	p, err := s.ReadSource(c.ID, source.ID, 1)
	if err != nil || !strings.Contains(p.Segments[0].Content, "12人") {
		t.Fatal("moving file lost snapshot", err)
	}
	turn := 0
	s.newChatModel = func(_ context.Context, _, _, _ string) (model.ToolCallingChatModel, error) {
		turn++
		current := turn
		return &collaborationModel{next: func(_ context.Context, in []*schema.Message, tools map[string]bool) (*schema.Message, error) {
			for _, name := range []string{"read_source", "search_web", "read_web", "list_sources"} {
				if !tools[name] {
					return nil, fmt.Errorf("missing default tool %s", name)
				}
			}
			if !strings.Contains(in[0].Content, source.ID) {
				return nil, errors.New("source directory not included")
			}
			if current == 1 && in[len(in)-1].Role != schema.Tool {
				return toolMessage("read_source", fmt.Sprintf(`{"source_id":%q,"start":1}`, source.ID)), nil
			}
			if current == 1 && !strings.Contains(in[len(in)-1].Content, "报名人数12人") {
				return nil, errors.New("actual source text not returned")
			}
			step := completedLeadStep("# 第一版\n\n12人，预算100元。[活动资料](source://" + source.ID + "/1)")
			step.Citations = []store.Citation{{SourceID: source.ID, Segment: 1, Quote: "报名人数12人，预算100元。"}}
			if current == 2 {
				step.WorkMode = "continue"
				step.Result = strings.Replace(step.Result, "第一版", "第二版", 1)
			}
			return leadTool(step), nil
		}}, nil
	}
	events := make(chan store.ConversationRun, 200)
	s.StartScheduler(func(r store.ConversationRun) { events <- r })
	for i := 0; i < 2; i++ {
		d, e := s.Schedule(store.ScheduleRequest{RequestID: fmt.Sprintf("sources-integration-%d", i), ConversationID: c.ID, Action: "lead", Content: "依据资料整理方案"})
		if e != nil {
			t.Fatal(e)
		}
		waitRun(t, events, d.Runs[0].ID, "completed")
	}
	detail, err := s.Conversation(c.ID)
	if err != nil || len(detail.Versions) != 2 || len(detail.Sources) != 1 {
		t.Fatal(err, detail.Versions)
	}
	md, err := s.ExportVersion(c.ID, detail.Versions[0].ID, "md")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(md.Data, []byte("第一版")) || bytes.Contains(md.Data, []byte("第二版")) || !bytes.Contains(md.Data, []byte("报名人数12人，预算100元。")) || !bytes.Contains(md.Data, []byte("第1行")) {
		t.Fatal("wrong exported version or lost citations", string(md.Data))
	}
	word, err := s.ExportVersion(c.ID, detail.Versions[0].ID, "docx")
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := material.Parse(context.Background(), word.Name, word.Data)
	if err != nil {
		t.Fatal(err)
	}
	text := fmt.Sprint(parsed.Segments)
	if !strings.Contains(text, "第一版") || !strings.Contains(text, "资料来源") || strings.Contains(text, "第二版") {
		t.Fatal("Word lost content", text)
	}
	if _, err = s.ExportVersion("other", detail.Versions[0].ID, "md"); err == nil {
		t.Fatal("cross-conversation export")
	}
	if _, err = s.ExportVersion(c.ID, detail.Versions[0].ID, "exe"); err == nil {
		t.Fatal("unsupported format")
	}
}

func TestLeadCorrectsInvalidCitationLinkWithoutFailingRun(t *testing.T) {
	s, c, _ := groupService(t)
	path := filepath.Join(c.WorkDir, "input.txt")
	if err := os.WriteFile(path, []byte("预算100元"), 0600); err != nil {
		t.Fatal(err)
	}
	var source store.Source
	corrected := false
	s.newChatModel = func(context.Context, string, string, string) (model.ToolCallingChatModel, error) {
		return &collaborationModel{next: func(_ context.Context, in []*schema.Message, _ map[string]bool) (*schema.Message, error) {
			last := in[len(in)-1]
			if last.Role != schema.Tool {
				return toolMessage("read_workspace_file", `{"path":"input.txt"}`), nil
			}
			if last.ToolName == "read_workspace_file" {
				var read sourceToolResult
				if err := json.Unmarshal([]byte(last.Content), &read); err != nil {
					return nil, err
				}
				if read.Page == nil || read.Page.Segments[0].Link != "source://"+read.Page.Source.ID+"/1" {
					return nil, errors.New("missing exact citation link")
				}
				source = read.Page.Source
			} else {
				if !strings.Contains(last.Content, "未提交") {
					return nil, errors.New("unexpected continuation after successful decision")
				}
				corrected = true
			}
			url := "source://" + source.ID + "/片段1"
			if corrected {
				url = "source://" + source.ID + "/1"
			}
			step := completedLeadStep("预算100元。[来源](" + url + ")")
			step.Citations = []store.Citation{{SourceID: source.ID, Segment: 1, Quote: "预算100元"}}
			return leadTool(step), nil
		}}, nil
	}
	events := make(chan store.ConversationRun, 100)
	s.StartScheduler(func(r store.ConversationRun) { events <- r })
	d, err := s.Schedule(store.ScheduleRequest{RequestID: "correct-citation-link", ConversationID: c.ID, Content: "根据文件给出预算", Action: "lead"})
	if err != nil {
		t.Fatal(err)
	}
	waitRun(t, events, d.Runs[0].ID, "completed")
	detail, err := s.Conversation(c.ID)
	if err != nil || !corrected || len(detail.Versions) != 1 || len(detail.Runs) != 1 {
		t.Fatal(detail, err)
	}
}
