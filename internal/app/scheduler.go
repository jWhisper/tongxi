package app

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"tongxi/internal/agent"
	"tongxi/internal/store"
)

// StartScheduler owns the single execution slot. SQLite is the source of work;
// the buffered notification only wakes the worker after a committed delivery.
func (s *Service) StartScheduler(emit func(store.ConversationRun)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.queueCancel != nil || s.closing {
		return
	}
	ctx, cancel := context.WithCancel(s.ctx)
	s.queueCancel, s.chatEmit, s.queueWake = cancel, emit, make(chan struct{}, 1)
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		for {
			select {
			case <-ctx.Done():
				return
			case <-s.queueWake:
			}
			for ctx.Err() == nil && s.executeQueued(ctx) {
			}
		}
	}()
	s.wakeQueue()
}

func (s *Service) wakeQueue() {
	select {
	case s.queueWake <- struct{}{}:
	default:
	}
}

func (s *Service) SendMessage(conversationID, content, requestID string) (store.ConversationRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closing {
		return store.ConversationRun{}, errors.New("应用正在退出")
	}
	content = strings.TrimSpace(content)
	if content == "" || len([]rune(content)) > 8000 {
		return store.ConversationRun{}, errors.New("消息请输入 1–8000 个字符")
	}
	if len(requestID) < 16 || len(requestID) > 128 {
		return store.ConversationRun{}, errors.New("无效的发送标识，请重新打开会话")
	}
	r, err := s.db.Enqueue(requestID, store.Message{ID: newID(), ConversationID: conversationID, Content: content}, newID())
	if err == nil {
		s.wakeQueue()
	}
	return r, workspaceError(err)
}

func (s *Service) executeQueued(queueCtx context.Context) bool {
	s.mu.Lock()
	if s.closing || s.cancel != nil {
		s.mu.Unlock()
		return false
	}
	r, err := s.db.QueuedRun()
	if err != nil {
		s.mu.Unlock()
		// A periodic retry also covers transient disk errors without dropping work.
		if !errors.Is(err, sql.ErrNoRows) {
			s.retryQueue(queueCtx)
		}
		return false
	}
	err = s.db.CheckCollaborationBudget(r.ID)
	ctx, cancel := context.WithTimeout(queueCtx, s.chatTimeout)
	var a store.Agent
	if err == nil {
		err = ctx.Err()
	}
	if err == nil {
		a, err = s.db.Agent(r.AgentID)
	}
	var cm model.ToolCallingChatModel
	var modelConfig store.ModelConfig
	var secret string
	var input []*schema.Message
	config := agent.Config{Name: "agent_" + a.ID, Description: a.Description, Instruction: a.Instruction, Tools: a.Tools}
	var history []*schema.Message
	var skillReminder string
	if err == nil && !a.Enabled {
		err = errors.New("角色已停用，本次任务未执行")
	}
	if err == nil {
		secret, err = s.vault.Get(a.KeyRef)
		if err != nil || secret == "" {
			err = errors.New("无法读取 API Key，请在模型设置中重新保存该模型的密钥")
		}
	}
	if err == nil {
		cm, err = s.newChatModel(ctx, a.BaseURL, a.Model, secret)
	}
	if err == nil {
		history, err = s.db.ContextHistory(r.AgentID, r.ConversationID, r.Kind)
		history = freshSkillHistory(history)
	}
	if err == nil {
		config, err = s.collaborationConfig(r, config)
	}
	if err == nil && r.Kind != "selector" {
		config, err = s.sourceConfig(r, config)
	}
	if err == nil && r.Kind != "selector" {
		config, skillReminder, err = s.skillConfig(r, config)
	}
	if err == nil {
		modelConfig, err = s.db.Model(a.ModelID)
		config.Context = &agent.ContextConfig{Capacity: modelConfig.ContextTokens, Ratio: modelConfig.TokenRatio, Backend: contextBackend{s.db, r.ConversationID, r.AgentID}}
	}
	if err == nil {
		config, err = s.historyConfig(r, config)
	}
	if err == nil {
		r.Status, r.AgentName, r.StartedAt = "running", a.Name, time.Now().UTC().Format(time.RFC3339Nano)
		r.Revision++
		input, err = s.db.ClaimConversationRun(r, a)
	}
	if err == nil {
		var deadline time.Time
		deadline, err = s.db.CollaborationDeadline(r.ID)
		if err == nil && !deadline.IsZero() {
			baseCancel := cancel
			var budgetCancel context.CancelFunc
			ctx, budgetCancel = context.WithDeadlineCause(ctx, deadline, store.ErrCollaborationTime)
			cancel = func() { budgetCancel(); baseCancel() }
		}
	}
	s.chatActive, s.chatCancel = &r, cancel
	if err == nil {
		cm = agent.MeterModel(cm, modelConfig.TokenRatio, func() error {
			s.mu.Lock()
			defer s.mu.Unlock()
			return s.db.CheckCollaborationBudget(r.ID)
		}, func(usage agent.TokenUsage) error {
			s.mu.Lock()
			defer s.mu.Unlock()
			if err := s.db.RecordTokenUsage(r.ID, usage.Input, usage.Output, usage.Cached, usage.Estimated, s.chatActive.Revision+1); err != nil {
				return err
			}
			s.chatActive.InputTokens += usage.Input
			s.chatActive.OutputTokens += usage.Output
			s.chatActive.CachedTokens += usage.Cached
			s.chatActive.UsageEstimated = s.chatActive.UsageEstimated || usage.Estimated
			s.chatActive.Revision++
			s.chatEmit(cloneChat(*s.chatActive))
			return s.db.CheckCollaborationBudget(r.ID)
		})
	}
	s.mu.Unlock()
	if err == nil {
		s.chatEmit(cloneChat(r))
	}
	transcript := []*schema.Message{}
	if err == nil {
		var output []*schema.Message
		if skillReminder != "" {
			history = append(history, schema.SystemMessage(skillReminder))
		}
		output, err = agent.RunConversation(ctx, cm, config, append(history, input...), func(u agent.Update) {
			s.mu.Lock()
			if s.chatActive.Status != "running" {
				s.mu.Unlock()
				return
			}
			s.chatActive.Text += u.Text
			if u.Compacting != nil {
				s.chatActive.ContextCompacting = *u.Compacting
			}
			if u.Tool != "" {
				s.chatActive.Tools = append(s.chatActive.Tools, u.Tool)
				if u.Tool == "skip_reply" {
					s.chatActive.Silent = true
					s.chatActive.Text = ""
				}
			}
			s.chatActive.Revision++
			update := cloneChat(*s.chatActive)
			s.mu.Unlock()
			s.chatEmit(update)
		})
		transcript = agent.WithoutImageData(append(input, output...))
	}
	s.mu.Lock()
	r = cloneChat(*s.chatActive)
	r.ContextCompacting = false
	if errors.Is(context.Cause(ctx), store.ErrCollaborationTime) {
		err = store.ErrCollaborationTime
	}
	if err == nil && r.Status == "running" {
		chain, chainErr := s.db.RunChain(r.ID)
		if chainErr != nil && !errors.Is(chainErr, sql.ErrNoRows) {
			err = chainErr
		} else if chain.LeadPolicy == 1 && r.AgentID == chain.LeadAgentID {
			step, stepErr := s.db.LeadStep(r.ID)
			if stepErr != nil {
				err = stepErr
			} else if step == nil {
				err = errors.New("主要助手未提交有效验收和下一步安排，可重试本次任务")
			} else {
				r.Text = step.PublicText()
			}
		}
	}
	if err == nil && r.Kind == "selector" && !slices.Contains(r.Tools, "choose_speaker") && !slices.Contains(r.Tools, "pause_discussion") {
		err = errors.New("未返回有效的发言选择")
	}
	if r.Status == "running" || r.Status == "queued" {
		r.Status = "completed"
		if err != nil {
			r.Status, r.Error = "failed", safeError(err, secret)
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				r.Error = "执行超时，请缩短请求或检查连接后重试"
			}
			if errors.Is(ctx.Err(), context.Canceled) {
				r.Status, r.Error = "interrupted", "执行被中断，可重新发送消息"
			}
			if errors.Is(err, store.ErrCollaborationTime) {
				r.Status, r.Error = "interrupted", store.CollaborationTimeReason
			}
			if errors.Is(err, store.ErrCollaborationTokens) {
				r.Status, r.Error = "interrupted", store.CollaborationTokenReason
			}
		}
		r.FinishedAt = time.Now().UTC().Format(time.RFC3339Nano)
		r.Revision++
	}
	var contextState *store.ContextState
	if err == nil && config.Context != nil && config.Context.Result != nil {
		contextState = &store.ContextState{Messages: config.Context.Result, ModelID: modelConfig.ID, ModelVersion: modelConfig.Version, TokenRatio: config.Context.Ratio}
	}
	saveErr := s.db.FinishConversationRun(r, transcript, newID(), contextState)
	if saveErr != nil {
		r.Status, r.Error = "failed", "结果未能保存到本地数据库，请检查磁盘空间后重启应用"
		r.Revision++
	}
	cancel()
	// Keep the last snapshot until another run replaces it, including a disk error.
	s.chatActive, s.chatCancel = &r, nil
	s.mu.Unlock()
	s.chatEmit(cloneChat(r))
	if saveErr != nil {
		s.retryQueue(queueCtx)
		return false
	}
	return true
}

func (s *Service) retryQueue(ctx context.Context) {
	select {
	case <-ctx.Done():
		return
	case <-time.After(time.Second):
	}
	s.mu.Lock()
	s.wakeQueue()
	s.mu.Unlock()
}

func (s *Service) StopRun(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var r store.ConversationRun
	if s.chatActive != nil && s.chatActive.ID == id {
		r = cloneChat(*s.chatActive)
	} else {
		var err error
		r, err = s.db.ConversationRun(id)
		if err != nil {
			return workspaceError(err)
		}
	}
	if r.ChainID != "" {
		return s.stopChainLocked(r.ChainID)
	}
	if r.Status != "queued" && r.Status != "running" {
		return nil
	}
	r.Status, r.Error, r.FinishedAt = "cancelled", "已停止，本次内容未完成", time.Now().UTC().Format(time.RFC3339Nano)
	r.Revision++
	if s.chatActive != nil && s.chatActive.ID == id && s.chatCancel != nil {
		s.chatCancel()
	}
	if err := s.db.FinishConversationRun(r, nil, ""); err != nil {
		return errors.New("已请求停止，但停止状态未能保存，请检查磁盘空间")
	}
	if s.chatActive != nil && s.chatActive.ID == id {
		s.chatActive = &r
	}
	if s.chatEmit != nil {
		s.chatEmit(cloneChat(r))
	}
	return nil
}

func cloneChat(r store.ConversationRun) store.ConversationRun {
	if r.Status != "running" {
		r.ContextCompacting = false
	}
	r.Tools = append([]string{}, r.Tools...)
	return r
}
