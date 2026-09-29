package main

import (
	"context"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/pkg/browser"
	"github.com/wailsapp/wails/v2/pkg/runtime"
	"tongxi/internal/app"
	"tongxi/internal/material"
	"tongxi/internal/skill"
	"tongxi/internal/store"
)

// App is the desktop binding boundary. Business logic remains usable without Wails.
type App struct {
	ctx        context.Context
	service    *app.Service
	startupErr error
}

func (a *App) Skills() ([]store.Skill, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	return a.service.Skills()
}
func (a *App) SkillContent(id string) (store.SkillContent, error) {
	if err := a.ready(); err != nil {
		return store.SkillContent{}, err
	}
	return a.service.SkillContent(id)
}
func (a *App) SaveSkill(input app.SkillInput) (store.SkillContent, error) {
	if err := a.ready(); err != nil {
		return store.SkillContent{}, err
	}
	v, err := a.service.SaveSkill(input)
	if err == nil {
		a.workspaceChanged()
	}
	return v, err
}
func (a *App) ImportSkill() (skill.Bundle, error) {
	if err := a.ready(); err != nil {
		return skill.Bundle{}, err
	}
	directory, err := runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{Title: "选择包含 SKILL.md 的技能目录"})
	if err != nil || directory == "" {
		return skill.Bundle{}, err
	}
	return a.service.ImportSkill(directory)
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	dir := os.Getenv("TONGXI_DATA_DIR")
	if dir == "" {
		base, err := os.UserConfigDir()
		if err != nil {
			a.startupErr = errors.New("无法确定应用数据目录")
			return
		}
		dir = filepath.Join(base, "tongxi")
	}
	db, err := store.Open(dir)
	if err != nil {
		a.startupErr = errors.New("无法打开本地数据库：" + err.Error())
		return
	}
	a.service = app.NewService(ctx, db, app.SystemVault{}, func(run store.ProbeRun) {
		runtime.EventsEmit(ctx, "probe:updated", run)
	})
	a.service.StartScheduler(func(run store.ConversationRun) {
		runtime.EventsEmit(ctx, "conversation:run", run)
		if run.Status != "running" {
			a.workspaceChanged()
		}
	})
}

func (a *App) ready() error {
	if a.startupErr != nil {
		return a.startupErr
	}
	if a.service == nil {
		return errors.New("应用尚未就绪")
	}
	return nil
}

func (a *App) Snapshot() (app.Snapshot, error) {
	if err := a.ready(); err != nil {
		return app.Snapshot{}, err
	}
	return a.service.Snapshot()
}

func (a *App) SaveSettings(input app.SettingsInput) (app.SettingsView, error) {
	if err := a.ready(); err != nil {
		return app.SettingsView{}, err
	}
	return a.service.SaveSettings(input)
}

func (a *App) Models() ([]store.ModelConfig, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	return a.service.Models()
}

func (a *App) SaveModel(input app.ModelInput) (store.ModelConfig, error) {
	if err := a.ready(); err != nil {
		return store.ModelConfig{}, err
	}
	m, err := a.service.SaveModel(input)
	if err == nil {
		a.workspaceChanged()
	}
	return m, err
}

func (a *App) DeleteModel(id string, version int) error {
	if err := a.ready(); err != nil {
		return err
	}
	err := a.service.DeleteModel(id, version)
	if err == nil {
		a.workspaceChanged()
	}
	return err
}

func (a *App) TestModel(input app.ModelInput) (app.ConnectionResult, error) {
	if err := a.ready(); err != nil {
		return app.ConnectionResult{}, err
	}
	return a.service.TestModel(input)
}

func (a *App) StartProbe(source, prompt string) (store.ProbeRun, error) {
	if err := a.ready(); err != nil {
		return store.ProbeRun{}, err
	}
	return a.service.StartProbe(source, prompt)
}

func (a *App) StopProbe(id string) error {
	if err := a.ready(); err != nil {
		return err
	}
	return a.service.StopProbe(id)
}

func (a *App) shutdown(context.Context) {
	if a.service != nil {
		a.service.Close()
	}
}

func (a *App) Workspace() (app.Workspace, error) {
	if err := a.ready(); err != nil {
		return app.Workspace{}, err
	}
	return a.service.Workspace()
}
func (a *App) SaveAgent(input app.AgentInput) (store.Agent, error) {
	if err := a.ready(); err != nil {
		return store.Agent{}, err
	}
	v, err := a.service.SaveAgent(input)
	if err == nil {
		a.workspaceChanged()
	}
	return v, err
}
func (a *App) SetAgentEnabled(id string, enabled bool) (store.Agent, error) {
	if err := a.ready(); err != nil {
		return store.Agent{}, err
	}
	v, err := a.service.SetAgentEnabled(id, enabled)
	if err == nil {
		a.workspaceChanged()
	}
	return v, err
}
func (a *App) SaveConversation(input store.Conversation) (store.Conversation, error) {
	if err := a.ready(); err != nil {
		return store.Conversation{}, err
	}
	v, err := a.service.SaveConversation(input)
	if err == nil {
		a.workspaceChanged()
	}
	return v, err
}
func (a *App) Conversation(id string) (app.ConversationDetail, error) {
	if err := a.ready(); err != nil {
		return app.ConversationDetail{}, err
	}
	return a.service.Conversation(id)
}
func (a *App) PostMessage(conversationID, content string) (store.Message, error) {
	if err := a.ready(); err != nil {
		return store.Message{}, err
	}
	v, err := a.service.PostMessage(conversationID, content)
	if err == nil {
		a.workspaceChanged()
	}
	return v, err
}
func (a *App) workspaceChanged() { runtime.EventsEmit(a.ctx, "workspace:changed") }

func (a *App) SendMessage(conversationID, content, requestID string) (store.ConversationRun, error) {
	if err := a.ready(); err != nil {
		return store.ConversationRun{}, err
	}
	r, err := a.service.SendMessage(conversationID, content, requestID)
	if err == nil {
		a.workspaceChanged()
	}
	return r, err
}

func (a *App) StopRun(id string) error {
	if err := a.ready(); err != nil {
		return err
	}
	return a.service.StopRun(id)
}

func (a *App) Schedule(input store.ScheduleRequest) (store.Delivery, error) {
	if err := a.ready(); err != nil {
		return store.Delivery{}, err
	}
	d, err := a.service.Schedule(input)
	if err == nil {
		a.workspaceChanged()
	}
	return d, err
}

func (a *App) StopChain(id string) error {
	if err := a.ready(); err != nil {
		return err
	}
	err := a.service.StopChain(id)
	if err == nil {
		a.workspaceChanged()
	}
	return err
}
func (a *App) RetryRun(id, requestID string) (store.ConversationRun, error) {
	if err := a.ready(); err != nil {
		return store.ConversationRun{}, err
	}
	r, err := a.service.RetryRun(id, requestID)
	if err == nil {
		a.workspaceChanged()
	}
	return r, err
}

func (a *App) ImportSources(conversationID string) (app.ImportResult, error) {
	if err := a.ready(); err != nil {
		return app.ImportResult{}, err
	}
	paths, err := runtime.OpenMultipleFilesDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "添加会话资料", Filters: []runtime.FileFilter{{DisplayName: "文字、PDF、Word、Excel、CSV", Pattern: "*.txt;*.md;*.markdown;*.pdf;*.docx;*.xlsx;*.csv"}},
	})
	if err != nil {
		return app.ImportResult{}, errors.New("无法打开文件选择窗口")
	}
	result, err := a.service.ImportFiles(conversationID, paths)
	if err == nil {
		a.workspaceChanged()
	}
	return result, err
}

func (a *App) ReadSource(conversationID, sourceID string, start int) (store.SourcePage, error) {
	if err := a.ready(); err != nil {
		return store.SourcePage{}, err
	}
	return a.service.ReadSource(conversationID, sourceID, start)
}

func (a *App) AddWebSource(conversationID, url string) (store.Source, error) {
	if err := a.ready(); err != nil {
		return store.Source{}, err
	}
	v, err := a.service.AddWebSource(conversationID, url)
	if err == nil {
		a.workspaceChanged()
	}
	return v, err
}

func (a *App) ExportVersion(conversationID, versionID, format string) (string, error) {
	if err := a.ready(); err != nil {
		return "", err
	}
	file, err := a.service.ExportVersion(conversationID, versionID, format)
	if err != nil {
		return "", err
	}
	directory, err := a.service.ExportDirectory(conversationID)
	if err != nil {
		return "", err
	}
	path, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title: "导出成果", DefaultDirectory: directory, DefaultFilename: file.Name, CanCreateDirectories: true,
		Filters: []runtime.FileFilter{{DisplayName: map[string]string{"md": "Markdown", "docx": "Word 文档"}[format], Pattern: "*." + format}},
	})
	if err != nil {
		return "", errors.New("无法打开保存窗口")
	}
	if path == "" {
		return "", nil
	}
	// Do not silently change the chosen path after the native overwrite prompt.
	if !strings.EqualFold(filepath.Ext(path), "."+format) {
		return "", errors.New("请使用与导出格式一致的文件扩展名后重新保存")
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tongxi-export-*")
	if err != nil {
		return "", errors.New("无法创建导出文件，请检查目录权限")
	}
	defer os.Remove(tmp.Name())
	if _, err = tmp.Write(file.Data); err != nil {
		tmp.Close()
		return "", errors.New("文件写入失败，请检查磁盘空间")
	}
	if err = tmp.Close(); err != nil {
		return "", errors.New("文件保存失败")
	}
	if err = os.Rename(tmp.Name(), path); err != nil {
		return "", errors.New("无法保存到所选位置，请检查文件权限")
	}
	return path, nil
}

func (a *App) ChooseWorkspaceDirectory() (string, error) {
	if err := a.ready(); err != nil {
		return "", err
	}
	path, err := runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{Title: "选择工作目录 · 保存会话后不可更换", CanCreateDirectories: true})
	if err != nil {
		return "", errors.New("无法打开目录选择窗口")
	}
	if path == "" {
		return "", nil
	}
	return app.ValidateWorkspaceDirectory(path)
}
func (a *App) OpenWorkspaceDirectory(conversationID string) error {
	if err := a.ready(); err != nil {
		return err
	}
	path, err := a.service.WorkspaceDirectory(conversationID)
	if err != nil {
		return err
	}
	u := url.URL{Scheme: "file", Path: path}
	if err := browser.OpenURL(u.String()); err != nil {
		return errors.New("无法打开工作目录，请通过系统文件管理器访问")
	}
	return nil
}
func (a *App) ListWorkspaceFiles(conversationID, path string, offset int) (material.WorkspacePage, error) {
	if err := a.ready(); err != nil {
		return material.WorkspacePage{}, err
	}
	return a.service.ListWorkspaceFiles(conversationID, path, offset)
}
func (a *App) ReadWorkspaceFile(conversationID, path string) (store.SourcePage, error) {
	if err := a.ready(); err != nil {
		return store.SourcePage{}, err
	}
	p, err := a.service.ReadWorkspaceFile(conversationID, path)
	if err == nil {
		a.workspaceChanged()
	}
	return p, err
}

// Opens only a verified copy of a stored, conversation-scoped document.
func (a *App) OpenArtifact(conversationID, id string, folder bool) error {
	if err := a.ready(); err != nil {
		return err
	}
	path, err := a.service.ArtifactFile(conversationID, id)
	if err != nil {
		return err
	}
	if folder {
		path = filepath.Dir(path)
	}
	u := url.URL{Scheme: "file", Path: path}
	if err = browser.OpenURL(u.String()); err != nil {
		return errors.New("无法打开成果文件，请检查系统默认应用")
	}
	return nil
}

func (a *App) OpenSourceFile(conversationID, id string) error {
	if err := a.ready(); err != nil {
		return err
	}
	path, err := a.service.SourceFile(conversationID, id)
	if err != nil {
		return err
	}
	u := url.URL{Scheme: "file", Path: path}
	if err = browser.OpenURL(u.String()); err != nil {
		return errors.New("无法打开文件，请检查系统默认应用")
	}
	return nil
}
