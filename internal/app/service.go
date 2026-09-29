package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/cloudwego/eino/components/model"
	"github.com/zalando/go-keyring"
	"tongxi/internal/agent"
	"tongxi/internal/material"
	"tongxi/internal/scriptrun"
	"tongxi/internal/store"
)

type Vault interface {
	Get(string) (string, error)
	Set(string, string) error
	Delete(string) error
}
type SystemVault struct{}

func (SystemVault) Get(ref string) (string, error) { return keyring.Get("com.tongxi.app", ref) }
func (SystemVault) Set(ref, key string) error      { return keyring.Set("com.tongxi.app", ref, key) }
func (SystemVault) Delete(ref string) error        { return keyring.Delete("com.tongxi.app", ref) }

type SettingsInput struct {
	BaseURL string `json:"baseURL"`
	Model   string `json:"model"`
	APIKey  string `json:"apiKey"`
}
type SettingsView struct {
	BaseURL string `json:"baseURL"`
	Model   string `json:"model"`
	HasKey  bool   `json:"hasKey"`
}
type Snapshot struct {
	Settings SettingsView     `json:"settings"`
	Runs     []store.ProbeRun `json:"runs"`
}

type Service struct {
	web           *material.Web
	executeScript func(context.Context, scriptrun.Request) scriptrun.Result
	ctx           context.Context
	db            *store.Store
	vault         Vault
	emit          func(store.ProbeRun)
	mu            sync.Mutex
	wg            sync.WaitGroup
	active        *store.ProbeRun
	cancel        context.CancelFunc
	closing       bool
	execute       func(context.Context, model.ToolCallingChatModel, string, func(agent.Update)) error
	queueWake     chan struct{}
	queueCancel   context.CancelFunc
	chatCancel    context.CancelFunc
	chatActive    *store.ConversationRun
	chatEmit      func(store.ConversationRun)
	newChatModel  func(context.Context, string, string, string) (model.ToolCallingChatModel, error)
	chatTimeout   time.Duration
	testCancel    context.CancelFunc
}

func NewService(ctx context.Context, db *store.Store, vault Vault, emit func(store.ProbeRun)) *Service {
	return &Service{ctx: ctx, db: db, vault: vault, emit: emit, execute: agent.Run, newChatModel: agent.NewModel, chatTimeout: 180 * time.Second, web: material.NewWeb(), executeScript: scriptrun.Execute}
}

func settingsView(v store.Settings) SettingsView {
	return SettingsView{BaseURL: v.BaseURL, Model: v.Model, HasKey: v.KeyRef != ""}
}

func (s *Service) Snapshot() (Snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, err := s.db.Settings()
	if err != nil {
		return Snapshot{}, err
	}
	runs, err := s.db.Runs()
	if err != nil {
		return Snapshot{}, err
	}
	if s.active != nil {
		for i := range runs {
			if runs[i].ID == s.active.ID {
				runs[i] = clone(*s.active)
			}
		}
	}
	return Snapshot{Settings: settingsView(v), Runs: runs}, nil
}

func normalizeSettings(in SettingsInput) (SettingsInput, error) {
	in.BaseURL = strings.TrimRight(strings.TrimSpace(in.BaseURL), "/")
	in.Model = strings.TrimSpace(in.Model)
	in.APIKey = strings.TrimSpace(in.APIKey)
	u, err := url.Parse(in.BaseURL)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return SettingsInput{}, errors.New("请填写有效的 Base URL，不包含密钥、查询参数或账号密码")
	}
	local := u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1" || u.Hostname() == "::1"
	if u.Scheme != "https" && !(local && u.Scheme == "http") {
		return SettingsInput{}, errors.New("远程模型服务请使用 HTTPS；本机服务可以使用 HTTP")
	}
	if in.Model == "" {
		return SettingsInput{}, errors.New("请填写模型名称")
	}
	return in, nil
}

func (s *Service) SaveSettings(in SettingsInput) (SettingsView, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closing {
		return SettingsView{}, errors.New("应用正在退出")
	}
	if s.cancel != nil {
		return SettingsView{}, errors.New("请等待当前验证结束后再修改连接")
	}
	var err error
	in, err = normalizeSettings(in)
	if err != nil {
		return SettingsView{}, err
	}
	old, err := s.db.Settings()
	if err != nil {
		return SettingsView{}, err
	}
	if in.APIKey == "" && (old.KeyRef == "" || old.BaseURL != in.BaseURL) {
		return SettingsView{}, errors.New("首次配置或更换服务地址时，请重新填写 API Key")
	}
	v := store.Settings{BaseURL: in.BaseURL, Model: in.Model, KeyRef: old.KeyRef}
	if in.APIKey != "" {
		v.KeyRef = "model-" + newID()
		if err := s.vault.Set(v.KeyRef, in.APIKey); err != nil {
			return SettingsView{}, errors.New("无法保存到系统凭据存储，请检查钥匙串访问权限")
		}
	}
	if err := s.db.SaveSettings(v); err != nil {
		if v.KeyRef != old.KeyRef {
			_ = s.vault.Delete(v.KeyRef)
		}
		return SettingsView{}, err
	}
	if old.KeyRef != "" && old.KeyRef != v.KeyRef {
		s.deleteUnusedKey(old.KeyRef)
	}
	return settingsView(v), nil
}

func (s *Service) StartProbe(source, prompt string) (store.ProbeRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closing {
		return store.ProbeRun{}, errors.New("应用正在退出")
	}
	if s.cancel != nil {
		return store.ProbeRun{}, errors.New("已有验证正在执行，请先等待完成或停止")
	}
	if s.chatCancel != nil {
		return store.ProbeRun{}, errors.New("角色正在回复，请等待完成后再验证连接")
	}
	if source != "local" && source != "model" {
		return store.ProbeRun{}, errors.New("未知的验证方式")
	}
	prompt = strings.TrimSpace(prompt)
	if source == "local" {
		prompt = agent.LocalPrompt
	}
	if prompt == "" || len([]rune(prompt)) > 8000 {
		return store.ProbeRun{}, errors.New("请输入 1–8000 个字符")
	}
	ctx, cancel := context.WithTimeout(s.ctx, 90*time.Second)
	var cm model.ToolCallingChatModel
	var secret string
	if source == "local" {
		cm = &agent.LocalModel{Delay: 25 * time.Millisecond}
	} else {
		v, err := s.db.Settings()
		if err != nil {
			cancel()
			return store.ProbeRun{}, err
		}
		if v.BaseURL == "" || v.Model == "" || v.KeyRef == "" {
			cancel()
			return store.ProbeRun{}, errors.New("请先配置模型连接")
		}
		secret, err = s.vault.Get(v.KeyRef)
		if err != nil || secret == "" {
			cancel()
			return store.ProbeRun{}, errors.New("无法读取 API Key，请在连接设置中重新保存")
		}
		cm, err = agent.NewModel(ctx, v.BaseURL, v.Model, secret)
		if err != nil {
			cancel()
			return store.ProbeRun{}, errors.New("无法初始化模型连接，请检查设置")
		}
	}
	r := store.ProbeRun{ID: newID(), Source: source, Prompt: prompt, Status: "running", Tools: []string{}, StartedAt: time.Now().UTC().Format(time.RFC3339Nano), Revision: 1}
	if err := s.db.InsertRun(r); err != nil {
		cancel()
		return store.ProbeRun{}, err
	}
	s.active = &r
	s.cancel = cancel
	s.wg.Add(1)
	initial := clone(r)
	go s.run(ctx, cm, r.ID, prompt, secret)
	return initial, nil
}

func (s *Service) run(ctx context.Context, cm model.ToolCallingChatModel, id, prompt, secret string) {
	defer s.wg.Done()
	err := s.execute(ctx, cm, prompt, func(update agent.Update) {
		s.mu.Lock()
		if s.active == nil || s.active.ID != id || s.active.Status != "running" {
			s.mu.Unlock()
			return
		}
		s.active.Text += update.Text
		if update.Tool != "" {
			s.active.Tools = append(s.active.Tools, update.Tool)
		}
		s.active.Revision++
		r := clone(*s.active)
		s.mu.Unlock()
		s.emit(r)
	})
	s.mu.Lock()
	if s.active.Status == "running" {
		s.active.Status = "completed"
		if err != nil {
			s.active.Status = "failed"
			s.active.Error = safeError(err, secret)
			if errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
				s.active.Status = "cancelled"
				s.active.Error = "已停止，本次内容未完成"
			}
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				s.active.Error = "执行超过 90 秒，请检查连接或缩短请求后重试"
			}
		}
		s.active.FinishedAt = time.Now().UTC().Format(time.RFC3339Nano)
		s.active.Revision++
		if saveErr := s.db.FinishRun(*s.active); saveErr != nil {
			s.active.Status = "failed"
			s.active.Error = "结果未能保存到本地数据库，请检查磁盘空间"
			s.active.Revision++
		}
	}
	s.cancel()
	s.cancel = nil
	s.wakeQueue()
	r := clone(*s.active)
	s.mu.Unlock()
	s.emit(r)
}

func (s *Service) StopProbe(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.active == nil || s.active.ID != id || s.active.Status != "running" {
		return nil
	}
	s.cancel()
	r := clone(*s.active)
	r.Status = "cancelled"
	r.Error = "已停止，本次内容未完成"
	r.FinishedAt = time.Now().UTC().Format(time.RFC3339Nano)
	r.Revision++
	if err := s.db.FinishRun(r); err != nil {
		return errors.New("已请求停止，但停止状态未能保存")
	}
	s.active = &r
	s.emit(clone(r))
	return nil
}

func (s *Service) Close() {
	s.mu.Lock()
	s.closing = true
	if s.testCancel != nil {
		s.testCancel()
	}
	if s.chatActive != nil && s.chatActive.Status == "running" {
		r := cloneChat(*s.chatActive)
		r.Status, r.Error, r.FinishedAt = "interrupted", "应用退出，本次执行中断；可手动重试或重新安排", time.Now().UTC().Format(time.RFC3339Nano)
		r.Revision++
		_ = s.db.FinishConversationRun(r, nil, "")
		s.chatActive = &r
	}
	if s.queueCancel != nil {
		s.queueCancel()
	}
	var id string
	if s.active != nil {
		id = s.active.ID
	}
	s.mu.Unlock()
	_ = s.StopProbe(id)
	s.wg.Wait()
	_ = s.db.Close()
}

func safeError(err error, secret string) string {
	s := err.Error()
	if secret != "" {
		for _, v := range []string{secret, url.QueryEscape(secret), url.PathEscape(secret)} {
			s = strings.ReplaceAll(s, v, "[已隐藏]")
		}
	}
	if len([]rune(s)) > 1200 {
		s = string([]rune(s)[:1200]) + "…"
	}
	return s
}
func newID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}
func clone(r store.ProbeRun) store.ProbeRun { r.Tools = append([]string{}, r.Tools...); return r }
