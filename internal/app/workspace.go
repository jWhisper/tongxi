package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"tongxi/internal/store"
)

type AgentInput struct {
	SkillIDs    []string `json:"skillIDs"`
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Instruction string   `json:"instruction"`
	ModelID     string   `json:"modelID"`
	Tools       []string `json:"tools"`
	Version     int      `json:"version"`
}

type Workspace struct {
	Skills        []store.Skill        `json:"skills"`
	Models        []store.ModelConfig  `json:"models"`
	Agents        []store.Agent        `json:"agents"`
	Conversations []store.Conversation `json:"conversations"`
}

type ConversationDetail struct {
	Artifacts    []store.Artifact          `json:"artifacts"`
	ScriptRuns   []store.ScriptRun         `json:"scriptRuns"`
	SkillUses    []store.SkillUse          `json:"skillUses"`
	Sources      []store.Source            `json:"sources"`
	Tasks        []store.WorkTask          `json:"tasks"`
	Versions     []store.WorkVersion       `json:"versions"`
	LeadSteps    map[string]store.LeadStep `json:"leadSteps"`
	Chains       []store.Chain             `json:"chains"`
	Conversation store.Conversation        `json:"conversation"`
	Messages     []store.Message           `json:"messages"`
	Runs         []store.ConversationRun   `json:"runs"`
}

func workspaceError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, store.ErrCollaborationTime) || errors.Is(err, store.ErrCollaborationTokens) {
		return err
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
	models, err := s.db.Models()
	if err != nil {
		return Workspace{}, workspaceError(err)
	}
	skills, err := s.db.Skills()
	if err != nil {
		return Workspace{}, workspaceError(err)
	}
	conversations, err := s.db.Conversations()
	return Workspace{Agents: agents, Conversations: conversations, Models: models, Skills: skills}, workspaceError(err)
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
	if in.ModelID == "" {
		return store.Agent{}, errors.New("请先在模型设置中添加模型，再为角色选择")
	}
	m, err := s.db.Model(in.ModelID)
	if err != nil {
		return store.Agent{}, workspaceError(err)
	}
	if !m.HasKey {
		return store.Agent{}, errors.New("请先在模型设置中保存 API Key")
	}
	a := store.Agent{ID: in.ID, Enabled: true, Version: in.Version}
	if in.ID != "" {
		a, err = s.db.Agent(in.ID)
		if err != nil {
			return a, workspaceError(err)
		}
		if in.Version != a.Version {
			return a, errors.New("角色已被修改，请重新打开后编辑")
		}
	} else {
		a.ID = newID()
	}
	a.Name, a.Description, a.Instruction = in.Name, in.Description, in.Instruction
	a.ModelID, a.Tools = m.ID, tools
	a.SkillIDs = in.SkillIDs
	saved, err := s.db.SaveAgent(a)
	if err != nil {
		return store.Agent{}, workspaceError(err)
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
	createdDirectory := ""
	if !create {
		old, err := s.db.Conversation(c.ID)
		if err != nil {
			return c, workspaceError(err)
		}
		if old.WorkDir != "" && old.WorkDir != c.WorkDir {
			return c, errors.New("工作目录已固定，不能更换或清空；请新建会话使用其他目录")
		}
		if old.WorkDir != "" { // Renaming a conversation does not require its disk to be mounted.
			v, err := s.db.SaveConversation(c, false)
			return v, workspaceError(err)
		}
	}
	if create && c.WorkDir == "" {
		c.WorkDir, _ = filepath.Abs(filepath.Join(s.db.Directory(), "workspaces", c.ID))
		if err := os.MkdirAll(c.WorkDir, 0700); err != nil {
			return c, errors.New("无法创建会话工作目录，请检查磁盘权限")
		}
		createdDirectory = c.WorkDir
	}
	if c.WorkDir != "" {
		path, err := ValidateWorkspaceDirectory(c.WorkDir)
		if err != nil {
			if createdDirectory != "" {
				_ = os.Remove(createdDirectory)
			}
			return c, err
		}
		c.WorkDir = path
	}
	saved, err := s.db.SaveConversation(c, create)
	if err != nil && createdDirectory != "" {
		_ = os.Remove(createdDirectory)
	}
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
	if err != nil {
		return ConversationDetail{}, workspaceError(err)
	}
	steps, err := s.db.PublishedLeadSteps(id)
	if err != nil {
		return ConversationDetail{}, workspaceError(err)
	}
	tasks, err := s.db.WorkTasks(id)
	if err != nil {
		return ConversationDetail{}, workspaceError(err)
	}
	versions, err := s.db.WorkVersions(id)
	if err != nil {
		return ConversationDetail{}, workspaceError(err)
	}
	sources, err := s.db.Sources(id)
	if err != nil {
		return ConversationDetail{}, workspaceError(err)
	}
	scriptRuns, err := s.db.ScriptRuns(id)
	if err != nil {
		return ConversationDetail{}, workspaceError(err)
	}
	artifacts, err := s.db.Artifacts(id)
	if err != nil {
		return ConversationDetail{}, workspaceError(err)
	}
	skillUses, err := s.db.SkillUses(id)
	if err != nil {
		return ConversationDetail{}, workspaceError(err)
	}
	if s.chatActive != nil {
		for i := range runs {
			if runs[i].ID == s.chatActive.ID {
				runs[i] = cloneChat(*s.chatActive)
			}
		}
	}
	return ConversationDetail{Conversation: c, Messages: messages, Runs: runs, Chains: chains, LeadSteps: steps, Tasks: tasks, Versions: versions, Sources: sources, SkillUses: skillUses, ScriptRuns: scriptRuns, Artifacts: artifacts}, nil
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
