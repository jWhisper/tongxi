package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"tongxi/internal/store"
)

func leadTool(step store.LeadStep) *schema.Message {
	data, _ := json.Marshal(step)
	return toolMessage("advance_work", string(data))
}

func testLeadStep(target, result string) store.LeadStep {
	return store.LeadStep{Action: "delegate", Checks: []store.AcceptanceCheck{{Criterion: "总时长30分钟", Status: "pending"}, {Criterion: "参与者可以跳过", Status: "unmet"}}, Result: result, Reason: "补齐尚未满足的条件", NextAgentID: target, Task: "修订方案并交付完整内容"}
}

func completedLeadStep(result string) store.LeadStep {
	return store.LeadStep{Action: "complete", Checks: []store.AcceptanceCheck{{Criterion: "回应用户要求", Status: "met", Evidence: "成果包含用户所需答复"}}, Result: result, Reason: "已完成本次答复"}
}

func TestEinoLeadReviewsRevisesAndDelivers(t *testing.T) {
	s, c, agents := groupService(t)
	created := 0
	s.newChatModel = func(_ context.Context, _ string, name string, _ string) (model.ToolCallingChatModel, error) {
		created++
		turn := created
		return &collaborationModel{next: func(_ context.Context, in []*schema.Message, tools map[string]bool) (*schema.Message, error) {
			if turn%2 == 0 {
				if tools["advance_work"] || tools["send_message"] {
					return nil, errors.New("member can bypass lead review")
				}
				want := "planner"
				if turn == 4 {
					want = "reviewer"
				}
				if name != want {
					return nil, fmt.Errorf("wrong selected member: %s", name)
				}
				if !strings.Contains(in[0].Content, "总时长30分钟") {
					return nil, errors.New("criteria not given to member")
				}
				for _, msg := range in[1:] {
					if msg.Role == schema.Tool {
						return nil, errors.New("another member's private tools leaked")
					}
				}
				return schema.AssistantMessage(fmt.Sprintf("成员第%d次成果：5分钟签到、25分钟讨论；可自愿跳过", turn), nil), nil
			}
			if !tools["advance_work"] || tools["send_message"] || name != "writer" {
				return nil, errors.New("lead decision tools missing")
			}
			if turn > 1 && !strings.Contains(promptText(in), fmt.Sprintf("成员第%d次成果", turn-1)) {
				return nil, errors.New("lead missed latest feedback")
			}
			step := testLeadStep(agents[2].ID, fmt.Sprintf("第%d版：5分钟签到、25分钟讨论", turn))
			if turn == 3 {
				step.NextAgentID = agents[1].ID
				step.Task = "检查参与者能否跳过，给出具体修订意见"
			}
			if turn == 7 {
				step.Action, step.NextAgentID, step.Task = "complete", "", ""
				step.Result = "完整最终方案：5分钟签到、25分钟讨论，两个环节均可选择旁听或跳过。"
				for i := range step.Checks {
					step.Checks[i].Status = "met"
					step.Checks[i].Evidence = "最终成果逐项覆盖且时间合计30分钟"
				}
			}
			return leadTool(step), nil
		}}, nil
	}
	events := make(chan store.ConversationRun, 1000)
	s.StartScheduler(func(r store.ConversationRun) { events <- r })
	_, err := s.Schedule(store.ScheduleRequest{RequestID: "adaptive-lead-delivery", ConversationID: c.ID, Action: "lead", Content: "请给出可执行的30分钟活动方案"})
	if err != nil {
		t.Fatal(err)
	}
	awaitGroup(t, events, 7)
	detail, _ := s.Conversation(c.ID)
	if len(detail.Runs) != 7 || detail.Chains[0].Status != "completed" || detail.Chains[0].Reserved != 7 {
		t.Fatal(detail.Chains, detail.Runs)
	}
	if !strings.Contains(detail.Messages[len(detail.Messages)-1].Content, "完整最终方案") {
		t.Fatal("missing usable deliverable")
	}
	if detail.Chains[0].Work.Checks[1].Status != "met" {
		t.Fatal("assessment missing")
	}
	if len(detail.LeadSteps) != 4 {
		t.Fatal("historical lead decisions missing", len(detail.LeadSteps))
	}
	last := detail.Messages[len(detail.Messages)-1]
	if detail.LeadSteps[last.SourceRunID].Result != detail.Chains[0].Work.Result {
		t.Fatal("published final result does not match its decision")
	}
}

func TestEinoLeadBudgetAndMissingAssessment(t *testing.T) {
	for _, mode := range []string{"budget", "missing"} {
		t.Run(mode, func(t *testing.T) {
			s, c, agents := groupService(t)
			created := 0
			s.newChatModel = func(_ context.Context, _ string, name string, _ string) (model.ToolCallingChatModel, error) {
				created++
				turn := created
				return &collaborationModel{next: func(_ context.Context, _ []*schema.Message, _ map[string]bool) (*schema.Message, error) {
					if mode == "missing" {
						return schema.AssistantMessage("未经检查就说完成", nil), nil
					}
					if name != "writer" {
						return schema.AssistantMessage("成员补充", nil), nil
					}
					return leadTool(testLeadStep(agents[1].ID, fmt.Sprintf("第%d版，仍有缺口", turn))), nil
				}}, nil
			}
			events := make(chan store.ConversationRun, 1000)
			s.StartScheduler(func(r store.ConversationRun) { events <- r })
			d, err := s.Schedule(store.ScheduleRequest{RequestID: "bounded-lead-" + mode, ConversationID: c.ID, Action: "lead", Content: "讨论"})
			if err != nil {
				t.Fatal(err)
			}
			if mode == "missing" {
				waitRun(t, events, d.Runs[0].ID, "failed")
			} else {
				awaitGroup(t, events, store.LeadLimit)
			}
			detail, _ := s.Conversation(c.ID)
			if detail.Chains[0].Status == "completed" {
				t.Fatal("unverified work accepted")
			}
			if mode == "budget" && (len(detail.Runs) != store.LeadLimit || detail.Chains[0].Status != "incomplete" || !strings.Contains(detail.Chains[0].Reason, "预算")) {
				t.Fatal(detail.Chains, len(detail.Runs))
			}
			if mode == "missing" && len(detail.Messages) != 1 {
				t.Fatal("unverified reply published")
			}
		})
	}
}

func TestEinoContinuesSavedArtifactInsteadOfStartingOver(t *testing.T) {
	s, c, _ := groupService(t)
	turn := 0
	s.newChatModel = func(_ context.Context, _ string, _ string, _ string) (model.ToolCallingChatModel, error) {
		turn++
		current := turn
		return &collaborationModel{next: func(_ context.Context, in []*schema.Message, _ map[string]bool) (*schema.Message, error) {
			step := completedLeadStep("12人，30分钟，可不发言；预算上限100元")
			step.WorkMode = "new"
			step.Title = "读书会"
			step.Brief = "12人，30分钟，预算上限100元，可不发言"
			step.Checks = []store.AcceptanceCheck{{Criterion: "12人，30分钟，可不发言", Status: "met", Evidence: "正文已覆盖"}, {Criterion: "预算不超过100元", Status: "met", Evidence: "正文上限100元"}}
			if current == 2 {
				if !strings.Contains(in[0].Content, "本次修改的基础快照") || !strings.Contains(in[0].Content, "预算上限100元") || !strings.Contains(in[0].Content, "continue") {
					return nil, errors.New("saved artifact missing from revision context")
				}
				step.WorkMode = "continue"
				step.Result = "12人，30分钟，可不发言；预算上限200元"
				step.Brief = step.Result
				step.Checks[1].Criterion = "预算不超过200元"
				step.Checks[1].Evidence = "正文上限已改200元"
				step.Changes = []store.RequirementChange{{Index: 2, Criterion: step.Checks[1].Criterion, Quote: "预算改成200元"}}
				step.ChangeSummary = "预算上限改为200元，其他要求保留"
			}
			return leadTool(step), nil
		}}, nil
	}
	events := make(chan store.ConversationRun, 100)
	s.StartScheduler(func(r store.ConversationRun) { events <- r })
	for i, request := range []string{"12人30分钟读书会，预算100元，可以不发言", "预算改成200元"} {
		d, err := s.Schedule(store.ScheduleRequest{RequestID: fmt.Sprintf("version-request-%02d", i), ConversationID: c.ID, Action: "lead", Content: request})
		if err != nil {
			t.Fatal(err)
		}
		waitRun(t, events, d.Runs[0].ID, "completed")
	}
	detail, err := s.Conversation(c.ID)
	if err != nil || len(detail.Tasks) != 1 || len(detail.Versions) != 2 || !strings.Contains(detail.Versions[0].Step.Result, "100元") || !strings.Contains(detail.Versions[1].Step.Result, "200元") {
		t.Fatal(detail.Versions, err)
	}
}
