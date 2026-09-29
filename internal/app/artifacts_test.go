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
	"tongxi/internal/scriptrun"
	"tongxi/internal/skill"
	"tongxi/internal/store"
)

func TestEinoFileGenerationReviewDeliveryAndRevision(t *testing.T) {
	s, c, agents := groupService(t)
	saved, err := s.SaveSkill(SkillInput{Enabled: true, Content: "---\nname: csv-report\ndescription: 汇总CSV并生成统计文件\n---\n运行scripts/report.py并核对输出。", Scripts: []skill.Resource{{Path: "scripts/report.py", Content: "# fixture"}}})
	if err != nil {
		t.Fatal(err)
	}
	a := agents[2]
	if _, err = s.SaveAgent(AgentInput{ID: a.ID, Name: a.Name, Instruction: a.Instruction, ModelID: a.ModelID, Version: a.Version, SkillIDs: []string{saved.ID}}); err != nil {
		t.Fatal(err)
	}
	var current store.Artifact
	created, executed := 0, 0
	s.executeScript = func(ctx context.Context, req scriptrun.Request) scriptrun.Result {
		executed++
		path := filepath.Join("成果", "脚本", req.ID, "report.csv")
		if err := os.MkdirAll(filepath.Join(req.Workspace, filepath.Dir(path)), 0700); err != nil {
			t.Fatal(err)
		}
		text := "category,total\nall,350\n"
		if executed == 2 {
			text = "department,total\n设计,200\n研发,150\n"
		}
		if err := os.WriteFile(filepath.Join(req.Workspace, path), []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
		return scriptrun.Result{Status: "completed", ExitCode: 0, Files: []string{path}}
	}
	s.newChatModel = func(_ context.Context, _ string, name string, _ string) (model.ToolCallingChatModel, error) {
		turn := created
		created++
		iteration := 0
		return &collaborationModel{next: func(ctx context.Context, in []*schema.Message, tools map[string]bool) (*schema.Message, error) {
			iteration++
			if !tools["list_artifacts"] || !tools["read_artifact"] {
				return nil, fmt.Errorf("file tools absent")
			}
			step := completedLeadStep("费用合计350元，附统计表")
			switch turn % 5 {
			case 0:
				step.Action = "delegate"
				step.Checks[0].Status = "pending"
				step.NextAgentID = agents[2].ID
				step.Task = "根据本次要求生成CSV，保留旧文件"
				return leadTool(step), nil
			case 1:
				switch iteration {
				case 1:
					return toolMessage("load_skill", fmt.Sprintf(`{"skill_id":%q}`, saved.ID)), nil
				case 2:
					return toolMessage("run_skill_script", fmt.Sprintf(`{"skill_id":%q,"path":"scripts/report.py","args":[]}`, saved.ID)), nil
				default:
					var result scriptToolResult
					if e := json.Unmarshal([]byte(in[len(in)-1].Content), &result); e != nil {
						return nil, e
					}
					if len(result.Artifacts) != 1 {
						return nil, fmt.Errorf("snapshot missing: %+v", result)
					}
					current = result.Artifacts[0]
					return schema.AssistantMessage("已生成文件 "+current.ID+"，请核对实际内容", nil), nil
				}
			case 2:
				if !strings.Contains(in[0].Content, current.ID) {
					return nil, fmt.Errorf("lead cannot discover member file")
				}
				step.Action = "delegate"
				step.Checks[0].Status = "pending"
				step.NextAgentID = agents[1].ID
				step.Task = "核对文件 " + current.ID + " 是否合计350"
				step.Files = []store.ArtifactDelivery{{ArtifactID: current.ID}}
				return leadTool(step), nil
			case 3:
				if iteration == 1 {
					return toolMessage("read_artifact", fmt.Sprintf(`{"artifact_id":%q,"start":1}`, current.ID)), nil
				}
				if !strings.Contains(in[len(in)-1].Content, "350") && !strings.Contains(in[len(in)-1].Content, "150") {
					return nil, fmt.Errorf("reviewer did not see actual data")
				}
				return schema.AssistantMessage("已读取文件 "+current.ID+" 的全部内容，合计350正确", nil), nil
			default:
				step.Files = []store.ArtifactDelivery{{ArtifactID: current.ID, Evidence: "评审已读取整张表，核对合计350元"}}
				return leadTool(step), nil
			}
		}}, nil
	}
	events := make(chan store.ConversationRun, 200)
	s.StartScheduler(func(r store.ConversationRun) { events <- r })
	for _, request := range []string{"根据CSV汇总费用并交付统计表", "改成按部门统计"} {
		if _, err = s.Schedule(store.ScheduleRequest{RequestID: newID(), ConversationID: c.ID, Action: "lead", Content: request}); err != nil {
			t.Fatal(err)
		}
		awaitGroup(t, events, 5)
	}
	detail, err := s.Conversation(c.ID)
	if err != nil || len(detail.Versions) != 2 || len(detail.Artifacts) != 2 || executed != 2 {
		t.Fatal(detail.Versions, detail.Artifacts, err)
	}
	oldID, newID := detail.Versions[0].Step.Files[0].ArtifactID, detail.Versions[1].Step.Files[0].ArtifactID
	if oldID == newID || newID != current.ID {
		t.Fatal("wrong version attachment")
	}
	old, err := s.db.Artifact(c.ID, oldID)
	if err != nil {
		t.Fatal(err)
	}
	// Workspace edits/deletion cannot change the delivered bytes or preview.
	if err = os.Remove(filepath.Join(c.WorkDir, old.Path)); err != nil {
		t.Fatal(err)
	}
	page, err := s.ReadSource(c.ID, old.SourceID, 1)
	if err != nil || !strings.Contains(page.Segments[1].Content, "all") {
		t.Fatal(page, err)
	}
	path, err := s.ArtifactFile(c.ID, oldID)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if string(data) != "category,total\nall,350\n" {
		t.Fatal(string(data))
	}
	if _, err = s.ArtifactFile("other", oldID); err == nil {
		t.Fatal("cross-conversation open")
	}
	if err = os.WriteFile(path, []byte("user edits"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ArtifactFile(c.ID, oldID); err == nil {
		t.Fatal("modified copy overwritten")
	}
	outside := t.TempDir()
	if err = os.Symlink(outside, filepath.Join(c.WorkDir, "成果", "交付", newID)); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ArtifactFile(c.ID, newID); err == nil {
		t.Fatal("symlink output directory accepted")
	}
	entries, _ := os.ReadDir(outside)
	if len(entries) != 0 {
		t.Fatal("wrote outside workspace")
	}
	exported, err := s.ExportVersion(c.ID, detail.Versions[0].ID, "md")
	if err != nil || !strings.Contains(string(exported.Data), "report.csv") || !strings.Contains(string(exported.Data), "评审已读取") {
		t.Fatal(string(exported.Data), err)
	}
	// The unchanged source and both stored binaries remain independently retrievable.
	oldBytes, _ := s.db.ArtifactData(c.ID, oldID)
	newBytes, _ := s.db.ArtifactData(c.ID, newID)
	if !strings.Contains(string(oldBytes), "all,350") || !strings.Contains(string(newBytes), "研发,150") {
		t.Fatal("snapshots were replaced")
	}
}
