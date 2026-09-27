package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"github.com/wailsapp/wails/v2/pkg/runtime"
	"tongxi/internal/app"
	"tongxi/internal/store"
)

// App is the desktop binding boundary. Business logic remains usable without Wails.
type App struct {
	ctx        context.Context
	service    *app.Service
	startupErr error
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
