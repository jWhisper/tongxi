package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"tongxi/internal/store"
)

func TestWorkspaceDefaultBindingUnavailableAndCopy(t *testing.T) {
	s, c, _ := groupService(t)
	if !filepath.IsAbs(c.WorkDir) {
		t.Fatal("missing default work directory", c)
	}
	other := c
	other.WorkDir = t.TempDir()
	if _, err := s.SaveConversation(other); err == nil {
		t.Fatal("changed directory")
	}
	other.WorkDir = ""
	if _, err := s.SaveConversation(other); err == nil {
		t.Fatal("cleared directory")
	}
	external := filepath.Join(t.TempDir(), "资料.txt")
	if err := os.WriteFile(external, []byte("报名12人"), 0600); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		result, err := s.ImportFiles(c.ID, []string{external})
		if err != nil || len(result.Sources) != 1 {
			t.Fatal(result, err)
		}
	}
	p, err := s.ListWorkspaceFiles(c.ID, ".", 0)
	if err != nil || len(p.Files) != 1 {
		t.Fatal(p, err)
	}
	dir, err := s.ExportDirectory(c.ID)
	if err != nil || dir != filepath.Join(c.WorkDir, "成果") {
		t.Fatal(dir, err)
	}
	if err = os.Rename(c.WorkDir, c.WorkDir+"-moved"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ListWorkspaceFiles(c.ID, ".", 0); err == nil {
		t.Fatal("silently replaced missing directory")
	}
	c.Title = "renamed while unavailable"
	if _, err = s.SaveConversation(c); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(c.WorkDir); !os.IsNotExist(err) {
		t.Fatal("recreated missing directory")
	}
}

func TestLegacyConversationCanChooseDirectoryOnce(t *testing.T) {
	s, c, _ := groupService(t)
	c.ID = "legacy"
	c.WorkDir = ""
	c, err := s.db.SaveConversation(c, true)
	if err != nil {
		t.Fatal(err)
	}
	c.WorkDir = t.TempDir()
	expected, err := ValidateWorkspaceDirectory(c.WorkDir)
	if err != nil {
		t.Fatal(err)
	}
	c, err = s.SaveConversation(c)
	if err != nil || c.WorkDir != expected {
		t.Fatal(c, err)
	}
	c.WorkDir = t.TempDir()
	if _, err = s.SaveConversation(c); err == nil {
		t.Fatal("rebound legacy conversation")
	}
}

func TestEinoBrowsesWorkspaceReadsNewRevisionAndRetainsOldCitation(t *testing.T) {
	s, c, _ := groupService(t)
	file := filepath.Join(c.WorkDir, "预算.txt")
	write := func(value string) {
		t.Helper()
		if err := os.WriteFile(file, []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("预算100元")
	iteration := 0
	s.newChatModel = func(context.Context, string, string, string) (model.ToolCallingChatModel, error) {
		iteration++
		current := iteration
		return &collaborationModel{next: func(_ context.Context, in []*schema.Message, tools map[string]bool) (*schema.Message, error) {
			if !tools["list_workspace_files"] || !tools["read_workspace_file"] {
				return nil, fmt.Errorf("missing workspace tools")
			}
			last := in[len(in)-1]
			if last.Role != schema.Tool {
				return toolMessage("list_workspace_files", `{"path":".","offset":0}`), nil
			}
			if strings.Contains(last.Content, `"files"`) {
				if !strings.Contains(last.Content, "预算.txt") {
					return nil, fmt.Errorf("directory not listed")
				}
				return toolMessage("read_workspace_file", `{"path":"预算.txt"}`), nil
			}
			var result sourceToolResult
			if err := json.Unmarshal([]byte(last.Content), &result); err != nil {
				return nil, err
			}
			if result.Page == nil {
				return nil, fmt.Errorf("missing source page %s", last.Content)
			}
			want := "预算100元"
			if current == 2 {
				want = "预算200元"
			}
			if result.Page.Segments[0].Content != want {
				return nil, fmt.Errorf("wrong file revision")
			}
			step := completedLeadStep("依据 " + want + "[预算](source://" + result.Page.Source.ID + "/1)")
			if current == 2 {
				step.WorkMode = "continue"
			}
			step.Citations = []store.Citation{{SourceID: result.Page.Source.ID, Segment: 1, Quote: want}}
			return leadTool(step), nil
		}}, nil
	}
	events := make(chan store.ConversationRun, 200)
	s.StartScheduler(func(r store.ConversationRun) { events <- r })
	for i := 0; i < 2; i++ {
		if i == 1 {
			write("预算200元")
		}
		d, err := s.Schedule(store.ScheduleRequest{RequestID: fmt.Sprintf("workspace-file-%d", i), ConversationID: c.ID, Action: "lead", Content: "根据目录文件更新结果"})
		if err != nil {
			t.Fatal(err)
		}
		waitRun(t, events, d.Runs[0].ID, "completed")
	}
	detail, err := s.Conversation(c.ID)
	if err != nil || len(detail.Versions) != 2 || len(detail.Sources) != 1 {
		t.Fatal(detail, err)
	}
	if detail.Versions[0].Step.Citations[0].SourceID != detail.Versions[1].Step.Citations[0].SourceID {
		t.Fatal("file reference changed")
	}
	if err = os.Remove(file); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ReadSource(c.ID, detail.Sources[0].ID, 1); err == nil {
		t.Fatal("deleted file should not return stale content")
	}
	old, err := s.ExportVersion(c.ID, detail.Versions[0].ID, "md")
	if err != nil || !strings.Contains(string(old.Data), "预算100元") || !strings.Contains(string(old.Data), "第1行") {
		t.Fatal("lost historical citation", string(old.Data), err)
	}
}

func TestWorkspaceReadsLatestWithinSameRunAndKeepsReference(t *testing.T) {
	s, c, _ := groupService(t)
	path := filepath.Join(c.WorkDir, "live.txt")
	if err := os.WriteFile(path, []byte("before"), 0600); err != nil {
		t.Fatal(err)
	}
	p, err := s.ReadWorkspaceFile(c.ID, "live.txt")
	if err != nil {
		t.Fatal(err)
	}
	d, err := s.Schedule(store.ScheduleRequest{RequestID: "latest-file-same-run", ConversationID: c.ID, Content: "read", Action: "lead"})
	if err != nil {
		t.Fatal(err)
	}
	r := d.Runs[0]
	a, err := s.db.Agent(r.AgentID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.ClaimConversationRun(r, a); err != nil {
		t.Fatal(err)
	}
	old, err := s.readWorkspaceFile(s.ctx, c.ID, "live.txt", r.ID)
	if err != nil || old.Segments[0].Content != "before" {
		t.Fatal(old, err)
	}
	if err = os.WriteFile(path, []byte("after"), 0600); err != nil {
		t.Fatal(err)
	}
	latest, err := s.readSource(s.ctx, c.ID, p.Source.ID, 1, r.ID)
	if err != nil || latest.Source.ID != p.Source.ID || latest.Segments[0].Content != "after" {
		t.Fatal(latest, err)
	}
	imported, err := s.ImportFiles(c.ID, []string{path})
	if err != nil || len(imported.Sources) != 1 || imported.Sources[0].ID != p.Source.ID {
		t.Fatal(imported, err)
	}
	files, err := s.ListWorkspaceFiles(c.ID, ".", 0)
	if err != nil || len(files.Files) != 1 {
		t.Fatal("copied an internal file", files, err)
	}
}
