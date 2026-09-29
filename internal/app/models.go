package app

import (
	"context"
	"errors"
	"io"
	"strings"
	"time"

	"github.com/cloudwego/eino/schema"
	"tongxi/internal/store"
)

type ModelInput struct {
	ContextTokens int    `json:"contextTokens"`
	ID            string `json:"id"`
	Name          string `json:"name"`
	Provider      string `json:"provider"`
	BaseURL       string `json:"baseURL"`
	Model         string `json:"model"`
	APIKey        string `json:"apiKey"`
	Version       int    `json:"version"`
}

type ConnectionResult struct {
	LatencyMS int64 `json:"latencyMS"`
}

func (s *Service) Models() ([]store.ModelConfig, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := s.db.Models()
	return m, workspaceError(err)
}

// Saving and testing resolve the visible draft. A saved key may only be
// reused for its original endpoint; the browser never receives it.
func (s *Service) resolveModel(in ModelInput) (store.ModelConfig, string, error) {
	var m store.ModelConfig
	if s.closing {
		return m, "", errors.New("应用正在退出")
	}
	c, err := normalizeSettings(SettingsInput{BaseURL: in.BaseURL, Model: in.Model, APIKey: in.APIKey})
	if err != nil {
		return m, "", err
	}
	switch in.Provider {
	case "compatible", "deepseek", "kimi", "kimi-code", "glm":
	default:
		return m, "", errors.New("请选择服务商")
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		name = c.Model
	}
	if len([]rune(name)) > 80 || len([]rune(c.Model)) > 200 {
		return m, "", errors.New("显示名称最多 80 个字符，模型 ID 最多 200 个字符")
	}
	if in.ID != "" {
		m, err = s.db.Model(in.ID)
		if err != nil {
			return m, "", workspaceError(err)
		}
		if in.Version != m.Version {
			return m, "", errors.New("模型配置已被修改，请重新打开后编辑")
		}
	}
	if c.APIKey == "" && (m.KeyRef == "" || m.BaseURL != c.BaseURL) {
		return m, "", errors.New("首次配置或更换服务地址时，请填写 API Key")
	}
	m.Name, m.Provider, m.BaseURL, m.Model = name, in.Provider, c.BaseURL, c.Model
	m.ContextTokens = in.ContextTokens
	if m.ContextTokens == 0 {
		m.ContextTokens = 32768
	}
	if m.ContextTokens < 8192 || m.ContextTokens > 2000000 {
		return m, "", errors.New("上下文容量请输入 8192–2000000 Tokens")
	}
	return m, c.APIKey, nil
}

func (s *Service) SaveModel(in ModelInput) (store.ModelConfig, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, key, err := s.resolveModel(in)
	if err != nil {
		return store.ModelConfig{}, err
	}
	oldRef := m.KeyRef
	if m.ID == "" {
		m.ID = newID()
	}
	if key != "" {
		m.KeyRef = "model-" + newID()
		if err = s.vault.Set(m.KeyRef, key); err != nil {
			return store.ModelConfig{}, errors.New("无法保存到系统凭据存储，请检查钥匙串访问权限")
		}
	}
	saved, err := s.db.SaveModel(m)
	if err != nil {
		if m.KeyRef != oldRef {
			_ = s.vault.Delete(m.KeyRef)
		}
		return store.ModelConfig{}, workspaceError(err)
	}
	if oldRef != "" && oldRef != m.KeyRef {
		s.deleteUnusedKey(oldRef)
	}
	return saved, nil
}

func (s *Service) DeleteModel(id string, version int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closing {
		return errors.New("应用正在退出")
	}
	m, err := s.db.Model(id)
	if err != nil {
		return workspaceError(err)
	}
	if err = s.db.DeleteModel(id, version); err != nil {
		return workspaceError(err)
	}
	s.deleteUnusedKey(m.KeyRef)
	return nil
}

func (s *Service) TestModel(in ModelInput) (ConnectionResult, error) {
	s.mu.Lock()
	m, secret, err := s.resolveModel(in)
	if err == nil && s.testCancel != nil {
		err = errors.New("已有连接测试正在进行")
	}
	if err == nil && secret == "" {
		secret, err = s.vault.Get(m.KeyRef)
		if err != nil || secret == "" {
			err = errors.New("无法读取 API Key，请重新填写后测试")
		}
	}
	if err != nil {
		s.mu.Unlock()
		return ConnectionResult{}, err
	}
	ctx, cancel := context.WithTimeout(s.ctx, 30*time.Second)
	s.testCancel = cancel
	s.wg.Add(1)
	s.mu.Unlock()
	defer func() {
		cancel()
		s.mu.Lock()
		s.testCancel = nil
		s.mu.Unlock()
		s.wg.Done()
	}()
	started := time.Now()
	cm, err := s.newChatModel(ctx, m.BaseURL, m.Model, secret)
	if err == nil {
		var stream *schema.StreamReader[*schema.Message]
		stream, err = cm.Stream(ctx, []*schema.Message{schema.UserMessage("请只回复 OK。")})
		if err == nil {
			defer stream.Close()
			for {
				var message *schema.Message
				message, err = stream.Recv()
				if err != nil {
					break
				}
				// Stop on the first model content, including reasoning. This
				// check never runs tools or stores a conversation turn.
				if message != nil && (message.Content != "" || message.ReasoningContent != "") {
					cancel()
					return ConnectionResult{LatencyMS: time.Since(started).Milliseconds()}, nil
				}
			}
		}
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return ConnectionResult{}, errors.New("连接测试超时（30 秒），请检查地址、网络或稍后重试")
	}
	if errors.Is(err, io.EOF) {
		return ConnectionResult{}, errors.New("服务已响应，但未返回模型内容，请检查模型 ID")
	}
	return ConnectionResult{}, errors.New("连接失败：" + safeError(err, secret))
}
