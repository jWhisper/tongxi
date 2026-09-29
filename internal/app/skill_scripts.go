package app

import (
	"context"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
	"strings"
	"tongxi/internal/scriptrun"
	"tongxi/internal/skill"
	"tongxi/internal/store"
)

type runSkillScriptInput struct {
	SkillID string   `json:"skill_id" jsonschema:"description=本次回复已加载的技能ID"`
	Path    string   `json:"path" jsonschema:"description=load_skill 返回的脚本相对路径，如 scripts/report.py"`
	Args    []string `json:"args" jsonschema:"description=脚本参数数组，输入文件用工作目录内相对路径；不拼接终端命令。脚本通过 TONGXI_OUTPUT_DIR 获取输出目录"`
}

func (s *Service) scriptTool(r store.ConversationRun) (tool.BaseTool, error) {
	return utils.InferTool("run_skill_script", "运行已加载技能自带的最新脚本，返回真实输出、退出状态及生成文件。支持 Python、Node.js、Shell；30秒，输入只读，输出写到 TONGXI_OUTPUT_DIR，无网络。", func(ctx context.Context, in *runSkillScriptInput) (*scriptToolResult, error) {
		return s.runSkillScript(ctx, r, *in)
	})
}
func (s *Service) runSkillScript(ctx context.Context, r store.ConversationRun, in runSkillScriptInput) (*scriptToolResult, error) {
	fail := func(err error) (*scriptToolResult, error) {
		return &scriptToolResult{Result: scriptrun.Result{Status: "failed", ExitCode: -1, Error: workspaceError(err).Error(), Files: []string{}}, Artifacts: []store.Artifact{}}, nil
	}
	if err := scriptrun.Validate(in.Path, in.Args); err != nil {
		return fail(err)
	}
	s.mu.Lock()
	if err := ctx.Err(); err != nil {
		s.mu.Unlock()
		return nil, err
	}
	v, err := s.db.LoadSkill(r.ID, in.SkillID, true)
	if err != nil {
		s.mu.Unlock()
		return fail(err)
	}
	found := false
	for _, f := range v.Scripts {
		found = found || f.Path == in.Path
	}
	if !found {
		s.mu.Unlock()
		return fail(store.ValidationError("当前技能不包含该脚本"))
	}
	c, err := s.db.Conversation(r.ConversationID)
	if err != nil {
		s.mu.Unlock()
		return fail(err)
	}
	if c.WorkDir == "" {
		s.mu.Unlock()
		return fail(store.ValidationError("请先为会话选择工作目录"))
	}
	execution := store.ScriptRun{ID: newID(), RunID: r.ID, SkillID: v.ID, Name: v.Name, Path: in.Path, Args: in.Args}
	err = s.db.StartScriptRun(execution, r.ConversationID)
	s.mu.Unlock()
	if err != nil {
		return fail(err)
	}
	s.scriptChanged(r.ID, in.Path)
	result := s.executeScript(ctx, scriptrun.Request{ID: execution.ID, Path: in.Path, Args: in.Args, Workspace: c.WorkDir, Bundle: skill.Bundle{Content: v.Content, Resources: v.Resources, Scripts: v.Scripts, Note: v.Note}})
	s.mu.Lock()
	err = s.db.FinishScriptRun(execution.ID, result)
	s.mu.Unlock()
	if err != nil {
		s.scriptChanged(r.ID, "")
		return fail(err)
	}
	out := &scriptToolResult{Result: result, Artifacts: []store.Artifact{}}
	if result.Status == "completed" && len(result.Files) > 0 {
		var notices []string
		out.Artifacts, notices = s.captureArtifacts(ctx, r, execution.ID, c.WorkDir, result.Files)
		if len(notices) > 0 {
			out.Stderr += "\n成果登记提示：\n" + strings.Join(notices, "\n")
			s.mu.Lock()
			_ = s.db.ScriptFileNotice(execution.ID, out.Stderr)
			s.mu.Unlock()
		}
	}
	s.scriptChanged(r.ID, "")
	return out, nil
}
func (s *Service) scriptChanged(runID, path string) {
	s.mu.Lock()
	var update *store.ConversationRun
	if s.chatActive != nil && s.chatActive.ID == runID {
		s.chatActive.ActiveScript = path
		s.chatActive.Revision++
		v := cloneChat(*s.chatActive)
		update = &v
	}
	emit := s.chatEmit
	s.mu.Unlock()
	if update != nil && emit != nil {
		emit(*update)
	}
}
