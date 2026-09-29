package app

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"tongxi/internal/store"
)

func TestStopOrDisableDuringLeadMemberIgnoresLateModelOutput(t *testing.T) {
	for _, mode := range []string{"chain", "run", "disable"} {
		t.Run(mode, func(t *testing.T) {
			s, c, agents := groupService(t)
			s.newChatModel = func(_ context.Context, _ string, name string, _ string) (model.ToolCallingChatModel, error) {
				return &scriptedModel{stream: func(ctx context.Context, _ []*schema.Message) (*schema.StreamReader[*schema.Message], error) {
					reader, writer := schema.Pipe[*schema.Message](4)
					if name == "writer" {
						writer.Send(leadTool(testLeadStep(agents[1].ID, "待完善方案")), nil)
						writer.Close()
						return reader, nil
					}
					go func() {
						defer writer.Close()
						writer.Send(schema.AssistantMessage("保留部分", nil), nil)
						<-ctx.Done()
						writer.Send(schema.AssistantMessage("不得展示的迟到结果", nil), nil)
					}()
					return reader, nil
				}}, nil
			}
			events := make(chan store.ConversationRun, 128)
			s.StartScheduler(func(r store.ConversationRun) { events <- r })
			d, err := s.Schedule(store.ScheduleRequest{RequestID: "stop-adaptive-lead-" + mode, ConversationID: c.ID, Content: "test", Action: "lead"})
			if err != nil {
				t.Fatal(err)
			}
			waitRun(t, events, d.Runs[0].ID, "completed")
			detail, _ := s.Conversation(c.ID)
			member := detail.Runs[1]
			for {
				update := waitRun(t, events, member.ID, "running")
				if update.Text != "" {
					break
				}
			}
			switch mode {
			case "chain":
				err = s.StopChain(d.ChainID)
			case "run":
				err = s.StopRun(member.ID)
			case "disable":
				_, err = s.SetAgentEnabled(agents[1].ID, false)
			}
			if err != nil {
				t.Fatal(err)
			}
			waitRun(t, events, member.ID, "cancelled")
			waitRun(t, events, member.ID, "cancelled")
			detail, _ = s.Conversation(c.ID)
			if detail.Chains[0].Status != "stopped" || len(detail.Runs) != 2 || len(detail.Messages) != 3 || detail.Runs[1].Text != "保留部分" {
				t.Fatal(detail)
			}
		})
	}
}

func TestRetryUsesServiceSchedulerWithoutDuplicatingMessages(t *testing.T) {
	s, c, _ := groupService(t)
	calls := 0
	s.newChatModel = func(context.Context, string, string, string) (model.ToolCallingChatModel, error) {
		calls++
		attempt := calls
		return &scriptedModel{stream: func(context.Context, []*schema.Message) (*schema.StreamReader[*schema.Message], error) {
			if attempt == 1 {
				return nil, errors.New("intentional provider failure")
			}
			r, w := schema.Pipe[*schema.Message](1)
			w.Send(leadTool(completedLeadStep("重试成功")), nil)
			w.Close()
			return r, nil
		}}, nil
	}
	events := make(chan store.ConversationRun, 100)
	s.StartScheduler(func(r store.ConversationRun) { events <- r })
	d, err := s.Schedule(store.ScheduleRequest{RequestID: "retry-original-request", ConversationID: c.ID, Action: "lead", Content: "original"})
	if err != nil {
		t.Fatal(err)
	}
	waitRun(t, events, d.Runs[0].ID, "failed")
	retry, err := s.RetryRun(d.Runs[0].ID, "explicit-retry-request")
	if err != nil {
		t.Fatal(err)
	}
	waitRun(t, events, retry.ID, "completed")
	dup, err := s.RetryRun(d.Runs[0].ID, "explicit-retry-request")
	if err != nil || dup.ID != retry.ID {
		t.Fatal(dup, err)
	}
	detail, _ := s.Conversation(c.ID)
	if len(detail.Messages) != 2 || len(detail.Runs) != 2 || detail.Chains[0].Reserved != 2 || detail.Chains[0].Status != "completed" {
		t.Fatal(detail)
	}
}

func TestShutdownKeepsIndependentQueueAndInterruptsCurrentRun(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	s := NewService(context.Background(), db, memoryVault{}, func(store.ProbeRun) {})
	_, one, two := chatFixture(t, s)
	s.newChatModel = func(context.Context, string, string, string) (model.ToolCallingChatModel, error) {
		return &scriptedModel{stream: func(ctx context.Context, _ []*schema.Message) (*schema.StreamReader[*schema.Message], error) {
			r, w := schema.Pipe[*schema.Message](2)
			go func() {
				defer w.Close()
				w.Send(schema.AssistantMessage("退出前部分", nil), nil)
				<-ctx.Done()
				w.Send(schema.AssistantMessage("迟到", nil), nil)
			}()
			return r, nil
		}}, nil
	}
	events := make(chan store.ConversationRun, 50)
	s.StartScheduler(func(r store.ConversationRun) { events <- r })
	first, err := s.SendMessage(one.ID, "active", "shutdown-active-request")
	if err != nil {
		t.Fatal(err)
	}
	for {
		r := waitRun(t, events, first.ID, "running")
		if r.Text != "" {
			break
		}
	}
	queued, err := s.SendMessage(two.ID, "queued", "shutdown-queued-request")
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	db, err = store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	saved, _ := db.ConversationRun(first.ID)
	next, _ := db.QueuedRun()
	if saved.Status != "interrupted" || saved.Text != "退出前部分" || next.ID != queued.ID {
		t.Fatal(saved, next)
	}
	messages, _ := db.Messages(one.ID)
	if len(messages) != 1 {
		t.Fatal("shutdown published late success")
	}
}

// The subprocess dies while Eino is actually consuming a model stream. Opening
// its database afterwards must explain uncertainty, never replay that invocation.
func TestCrashRecoveryProcess(t *testing.T) {
	if dir := os.Getenv("TONGXI_CRASH_TEST_DIR"); dir != "" {
		db, err := store.Open(dir)
		if err != nil {
			t.Fatal(err)
		}
		s := NewService(context.Background(), db, memoryVault{}, func(store.ProbeRun) {})
		_, one, _ := chatFixture(t, s)
		s.newChatModel = func(context.Context, string, string, string) (model.ToolCallingChatModel, error) {
			return &scriptedModel{stream: func(ctx context.Context, _ []*schema.Message) (*schema.StreamReader[*schema.Message], error) {
				fmt.Println("MODEL_RUNNING")
				r, _ := schema.Pipe[*schema.Message](1)
				return r, nil
			}}, nil
		}
		// Deliberately commit before there is a scheduler or notification channel.
		if _, err = s.SendMessage(one.ID, "crash", "crash-process-request"); err != nil {
			t.Fatal(err)
		}
		s.StartScheduler(func(store.ConversationRun) {})
		select {}
	}
	dir := t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run=^TestCrashRecoveryProcess$")
	cmd.Env = append(os.Environ(), "TONGXI_CRASH_TEST_DIR="+dir)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill() }()
	ready := make(chan bool, 1)
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			if strings.Contains(scanner.Text(), "MODEL_RUNNING") {
				ready <- true
				return
			}
		}
		ready <- false
	}()
	select {
	case ok := <-ready:
		if !ok {
			t.Fatal("child never entered model execution")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("child startup timed out")
	}
	if err = cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait()
	db, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	workspace, err := db.Conversations()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, c := range workspace {
		runs, _ := db.ConversationRuns(c.ID)
		for _, r := range runs {
			found = true
			if r.Status != "interrupted" {
				t.Fatal("uncertain invocation replayed", r)
			}
		}
	}
	if !found {
		t.Fatal("no durable run after process crash")
	}
	if _, err = db.QueuedRun(); err == nil {
		t.Fatal("crashed invocation automatically queued")
	}
}
