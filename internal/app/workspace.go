package app

import (
	"errors"
	"strings"

	"tongxi/internal/store"
)

type AgentInput struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Instruction string   `json:"instruction"`
	BaseURL     string   `json:"baseURL"`
	Model       string   `json:"model"`
	APIKey      string   `json:"apiKey"`
	Tools       []string `json:"tools"`
	Version     int      `json:"version"`
}

type Workspace struct {
	Agents        []store.Agent        `json:"agents"`
	Conversations []store.Conversation `json:"conversations"`
}

type ConversationDetail struct {
	Chains       []store.Chain           `json:"chains"`
	Conversation store.Conversation      `json:"conversation"`
	Messages     []store.Message         `json:"messages"`
	Runs         []store.ConversationRun `json:"runs"`
}

func workspaceError(err error) error {
	if err == nil {
		return nil
	}
	var validation store.ValidationError
	if errors.As(err, &validation) {
		return err
	}
	return errors.New("本地数据读写失败，请检查磁盘空间后重试")
}

func (s *Service) Workspace() (Workspace, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	agents, err := s.db.Agents()
	if err != nil {
		return Workspace{}, workspaceError(err)
	}
	conversations, err := s.db.Conversations()
	return Workspace{Agents: agents, Conversations: conversations}, workspaceError(err)
}

func (s *Service) SaveAgent(in AgentInput) (store.Agent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closing {
		return store.Agent{}, errors.New("应用正在退出")
	}
	in.Name = strings.TrimSpace(in.Name)
	in.Description = strings.TrimSpace(in.Description)
	in.Instruction = strings.TrimSpace(in.Instruction)
	if in.Name == "" || len([]rune(in.Name)) > 60 {
		return store.Agent{}, errors.New("角色名称请输入 1–60 个字符")
	}
	if len([]rune(in.Description)) > 300 {
		return store.Agent{}, errors.New("角色简介最多 300 个字符")
	}
	if in.Instruction == "" || len([]rune(in.Instruction)) > 8000 {
		return store.Agent{}, errors.New("角色指令请输入 1–8000 个字符")
	}
	tools := []string{}
	for _, tool := range in.Tools {
		if tool != "count_characters" {
			return store.Agent{}, errors.New("所选工具尚未开放")
		}
		if len(tools) == 0 {
			tools = append(tools, tool)
		}
	}
	config, err := normalizeSettings(SettingsInput{BaseURL: in.BaseURL, Model: in.Model, APIKey: in.APIKey})
	if err != nil {
		return store.Agent{}, err
	}
	a := store.Agent{ID: in.ID, Enabled: true, Version: in.Version}
	var old store.Settings
	if in.ID != "" {
		a, err = s.db.Agent(in.ID)
		if err != nil {
			return a, workspaceError(err)
		}
		if in.Version != a.Version {
			return a, errors.New("角色已被修改，请重新打开后编辑")
		}
		old = store.Settings{BaseURL: a.BaseURL, Model: a.Model, KeyRef: a.KeyRef}
	} else {
		a.ID = newID()
		a.Version = 0
		old, err = s.db.Settings()
		if err != nil {
			return a, workspaceError(err)
		}
	}
	if config.APIKey == "" && (old.KeyRef == "" || old.BaseURL != config.BaseURL) {
		return store.Agent{}, errors.New("请填写 API Key；只有相同服务地址才能沿用已有密钥")
	}
	a.Name = in.Name
	a.Description = in.Description
	a.Instruction = in.Instruction
	a.BaseURL = config.BaseURL
	a.Model = config.Model
	a.Tools = tools
	a.KeyRef = old.KeyRef
	if config.APIKey != "" {
		a.KeyRef = "model-" + newID()
		if err = s.vault.Set(a.KeyRef, config.APIKey); err != nil {
			return store.Agent{}, errors.New("无法保存到系统凭据存储，请检查钥匙串访问权限")
		}
	}
	saved, err := s.db.SaveAgent(a)
	if err != nil {
		if a.KeyRef != old.KeyRef {
			_ = s.vault.Delete(a.KeyRef)
		}
		return store.Agent{}, workspaceError(err)
	}
	if old.KeyRef != "" && old.KeyRef != a.KeyRef {
		s.deleteUnusedKey(old.KeyRef)
	}
	return saved, nil
}

func (s *Service) deleteUnusedKey(ref string) {
	used, err := s.db.KeyReferenced(ref)
	if err == nil && !used {
		_ = s.vault.Delete(ref)
	}
}

func (s *Service) SetAgentEnabled(id string, enabled bool) (store.Agent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closing {
		return store.Agent{}, errors.New("应用正在退出")
	}
	a, changed, err := s.db.SetAgentEnabledAndCancel(id, enabled, s.chatActive)
	if err == nil {
		s.applyCancelled(changed)
	} else if !enabled && s.chatActive != nil && s.chatActive.AgentID == id && s.chatCancel != nil {
		s.chatCancel()
	}
	return a, workspaceError(err)
}

func (s *Service) SaveConversation(c store.Conversation) (store.Conversation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closing {
		return c, errors.New("应用正在退出")
	}
	c.Title = strings.TrimSpace(c.Title)
	if c.Title == "" || len([]rune(c.Title)) > 100 {
		return c, errors.New("会话名称请输入 1–100 个字符")
	}
	create := c.ID == ""
	if create {
		c.ID = newID()
	}
	saved, err := s.db.SaveConversation(c, create)
	return saved, workspaceError(err)
}

func (s *Service) Conversation(id string) (ConversationDetail, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, err := s.db.Conversation(id)
	if err != nil {
		return ConversationDetail{}, workspaceError(err)
	}
	messages, err := s.db.Messages(id)
	if err != nil {
		return ConversationDetail{}, workspaceError(err)
	}
	chains, err := s.db.Chains(id)
	if err != nil {
		return ConversationDetail{}, workspaceError(err)
	}
	runs, err := s.db.ConversationRuns(id)
	if s.chatActive != nil {
		for i := range runs {
			if runs[i].ID == s.chatActive.ID {
				runs[i] = cloneChat(*s.chatActive)
			}
		}
	}
	return ConversationDetail{Conversation: c, Messages: messages, Runs: runs, Chains: chains}, workspaceError(err)
}

// The desktop can only publish as the local user; agent identity belongs to the backend.
func (s *Service) PostMessage(conversationID, content string) (store.Message, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closing {
		return store.Message{}, errors.New("应用正在退出")
	}
	content = strings.TrimSpace(content)
	if content == "" || len([]rune(content)) > 8000 {
		return store.Message{}, errors.New("消息请输入 1–8000 个字符")
	}
	m, err := s.db.AppendMessage(store.Message{ID: newID(), ConversationID: conversationID, SenderType: "user", Content: content})
	return m, workspaceError(err)
}
