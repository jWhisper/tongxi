package app

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"strings"
	"testing"
	"tongxi/internal/scriptrun"
	"tongxi/internal/skill"
	"tongxi/internal/store"
)

func TestEinoLoadsCurrentSkillOnDemandAndRecordsUse(t *testing.T) {
	s, c, agents := groupService(t)
	input := SkillInput{Enabled: true, Content: "---\nname: budget-check\ndescription: 核对预算时使用\n---\n专属步骤甲：必须读取 references/check.md。", Resources: []skill.Resource{{Path: "references/check.md", Content: "预算上限100元"}}}
	v, err := s.SaveSkill(input)
	if err != nil {
		t.Fatal(err)
	}
	input.ID, input.UpdatedAt = v.ID, v.UpdatedAt
	a := agents[0]
	if _, err = s.SaveAgent(AgentInput{ID: a.ID, Name: a.Name, Description: a.Description, Instruction: a.Instruction, ModelID: a.ModelID, Tools: a.Tools, SkillIDs: []string{v.ID}, Version: a.Version}); err != nil {
		t.Fatal(err)
	}
	round := 0
	s.newChatModel = func(context.Context, string, string, string) (model.ToolCallingChatModel, error) {
		round++
		current := round
		call := 0
		return &collaborationModel{next: func(_ context.Context, in []*schema.Message, tools map[string]bool) (*schema.Message, error) {
			call++
			if current > 1 && call == 1 {
				for _, message := range in {
					if message.Role == schema.Tool && strings.Contains(message.Content, "专属步骤甲") {
						return nil, fmt.Errorf("old skill instructions leaked into new run")
					}
				}
			}
			if current == 3 {
				if tools["load_skill"] || strings.Contains(in[0].Content, "budget-check") {
					return nil, fmt.Errorf("disabled catalog exposed")
				}
				return leadTool(completedLeadStep("普通任务完成")), nil
			}
			if !tools["load_skill"] || !tools["read_skill_resource"] {
				return nil, fmt.Errorf("skill tools missing")
			}
			if !strings.Contains(in[0].Content, v.ID) || strings.Contains(in[0].Content, "专属步骤甲") {
				return nil, fmt.Errorf("catalog missing or eager body injection")
			}
			if call == 1 {
				hasReminder := false
				for _, reminder := range in {
					if reminder.Role == schema.System && strings.Contains(reminder.Content, "本次实际尚未读取任何技能") && strings.Contains(reminder.Content, v.ID) {
						hasReminder = true
					}
				}
				if !hasReminder {
					return nil, fmt.Errorf("current task skill reminder missing")
				}
				return toolMessage("load_skill", fmt.Sprintf(`{"skill_id":%q}`, v.ID)), nil
			}
			var got skillResult
			if err := json.Unmarshal([]byte(in[len(in)-1].Content), &got); err != nil {
				return nil, err
			}
			if got.Error != "" {
				return nil, fmt.Errorf("tool error %s", got.Error)
			}
			if call == 2 {
				if current == 1 {
					input.Resources[0].Content = "预算上限200元"
					updated, e := s.SaveSkill(input)
					if e != nil {
						return nil, e
					}
					input.UpdatedAt = updated.UpdatedAt
				}
				if !strings.Contains(got.Content, "专属步骤甲") || len(got.Resources) != 1 {
					return nil, fmt.Errorf("instructions or resource list missing")
				}
				return toolMessage("read_skill_resource", fmt.Sprintf(`{"skill_id":%q,"path":"references/check.md","offset":0}`, v.ID)), nil
			}
			want := "预算上限200元"
			if got.Content != want {
				return nil, fmt.Errorf("wrong reference: %s", got.Content)
			}
			step := completedLeadStep(want)
			if current == 2 {
				step.WorkMode = "continue"
			}
			return leadTool(step), nil
		}}, nil
	}
	events := make(chan store.ConversationRun, 200)
	s.StartScheduler(func(r store.ConversationRun) { events <- r })
	for i := 0; i < 3; i++ {
		if i > 0 {
			input.Resources[0].Content = "预算上限200元"
			input.Enabled = i != 2
			v, err = s.SaveSkill(input)
			if err != nil {
				t.Fatal(err)
			}
			input.UpdatedAt = v.UpdatedAt
		}
		d, err := s.Schedule(store.ScheduleRequest{RequestID: newID(), ConversationID: c.ID, Action: "lead", Content: "检查预算"})
		if err != nil {
			t.Fatal(err)
		}
		waitRun(t, events, d.Runs[0].ID, "completed")
	}
	detail, err := s.Conversation(c.ID)
	if err != nil || len(detail.SkillUses) != 2 {
		t.Fatal(detail.SkillUses, err)
	}
	current, err := s.SkillContent(v.ID)
	if err != nil || current.Resources[0].Content != "预算上限200元" {
		t.Fatal(current, err)
	}
}

func TestFreshSkillHistoryRemovesOnlySkillToolPairs(t *testing.T) {
	call := func(id, name string) schema.ToolCall {
		return schema.ToolCall{ID: id, Type: "function", Function: schema.FunctionCall{Name: name}}
	}
	mixed := schema.AssistantMessage("正在处理", []schema.ToolCall{call("one", "load_skill"), call("two", "read_source")})
	mixed.ReasoningContent = "obsolete skill reasoning"
	history := []*schema.Message{mixed, schema.ToolMessage("old skill body", "one"), schema.ToolMessage("user source", "two"), schema.AssistantMessage("old result", nil), schema.AssistantMessage("", []schema.ToolCall{call("three", "read_skill_resource")}), schema.ToolMessage("old reference", "three")}
	history[3].ReasoningContent = "old workflow carried into final reasoning"
	got := freshSkillHistory(history)
	if len(got) != 3 || len(got[0].ToolCalls) != 1 || got[0].ToolCalls[0].ID != "two" || got[1].Content != "user source" || got[2].Content != "old result" {
		t.Fatal("wrong retained history", got)
	}
	for _, m := range got {
		if m.ReasoningContent != "" {
			t.Fatal("past reasoning retained")
		}
	}
	if len(history[0].ToolCalls) != 2 || history[1].Content != "old skill body" || history[3].ReasoningContent == "" {
		t.Fatal("saved transcript mutated")
	}
}

func TestEinoExecutesLatestImportedScriptAndRecordsResult(t *testing.T) {
	s, c, agents := groupService(t)
	in := SkillInput{Enabled: true, Content: "---\nname: budget-script\ndescription: 执行预算计算\n---\n执行 scripts/budget.py。", Scripts: []skill.Resource{{Path: "scripts/budget.py", Content: "old script"}}}
	saved, err := s.SaveSkill(in)
	if err != nil {
		t.Fatal(err)
	}
	in.ID, in.UpdatedAt = saved.ID, saved.UpdatedAt
	a := agents[0]
	if _, err = s.SaveAgent(AgentInput{ID: a.ID, Name: a.Name, Description: a.Description, Instruction: a.Instruction, ModelID: a.ModelID, Tools: a.Tools, SkillIDs: []string{saved.ID}, Version: a.Version}); err != nil {
		t.Fatal(err)
	}
	executed := 0
	s.executeScript = func(ctx context.Context, req scriptrun.Request) scriptrun.Result {
		executed++
		if req.Bundle.Scripts[0].Content != "latest script" || len(req.Args) != 1 || req.Args[0] != "with spaces; literal" {
			return scriptrun.Result{Status: "failed", ExitCode: -1, Error: "stale script or changed args"}
		}
		// The execution must not hold s.mu while a process is running.
		detail, e := s.Conversation(c.ID)
		if e != nil || len(detail.ScriptRuns) != 1 || detail.ScriptRuns[0].Status != "running" {
			return scriptrun.Result{Status: "failed", ExitCode: -1, Error: "running record not visible"}
		}
		return scriptrun.Result{Status: "completed", ExitCode: 0, Stdout: "total=300", Files: []string{"成果/脚本/test/result.csv"}}
	}
	s.newChatModel = func(context.Context, string, string, string) (model.ToolCallingChatModel, error) {
		n := 0
		return &collaborationModel{next: func(_ context.Context, messages []*schema.Message, tools map[string]bool) (*schema.Message, error) {
			n++
			if !tools["run_skill_script"] {
				return nil, fmt.Errorf("missing script tool")
			}
			args := fmt.Sprintf(`{"skill_id":%q,"path":"scripts/budget.py","args":["with spaces; literal"]}`, saved.ID)
			switch n {
			case 1:
				return toolMessage("run_skill_script", args), nil
			case 2:
				if !strings.Contains(messages[len(messages)-1].Content, "请先调用 load_skill") {
					return nil, fmt.Errorf("script ran before skill load")
				}
				return toolMessage("load_skill", fmt.Sprintf(`{"skill_id":%q}`, saved.ID)), nil
			case 3:
				if !strings.Contains(messages[len(messages)-1].Content, "scripts/budget.py") {
					return nil, fmt.Errorf("script listing missing")
				}
				in.Scripts[0].Content = "latest script"
				if _, e := s.SaveSkill(in); e != nil {
					return nil, e
				}
				return toolMessage("run_skill_script", args), nil
			default:
				var got scriptrun.Result
				if e := json.Unmarshal([]byte(messages[len(messages)-1].Content), &got); e != nil {
					return nil, e
				}
				if got.Status != "completed" || got.Stdout != "total=300" {
					return nil, fmt.Errorf("script failed: %s", got.Error)
				}
				return leadTool(completedLeadStep("total=300")), nil
			}
		}}, nil
	}
	events := make(chan store.ConversationRun, 200)
	s.StartScheduler(func(r store.ConversationRun) { events <- r })
	d, err := s.Schedule(store.ScheduleRequest{RequestID: newID(), ConversationID: c.ID, Action: "lead", Content: "执行预算计算"})
	if err != nil {
		t.Fatal(err)
	}
	waitRun(t, events, d.Runs[0].ID, "completed")
	detail, err := s.Conversation(c.ID)
	if err != nil || executed != 1 || len(detail.ScriptRuns) != 1 || detail.ScriptRuns[0].Stdout != "total=300" || detail.ScriptRuns[0].Status != "completed" || detail.Runs[0].ActiveScript != "" {
		t.Fatal(executed, detail.ScriptRuns, err)
	}
}
