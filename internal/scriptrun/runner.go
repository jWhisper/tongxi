package scriptrun

import (
	"context"
	"errors"
	"strings"
	"tongxi/internal/skill"
)

type Request struct {
	ID        string
	Path      string
	Args      []string
	Workspace string
	Bundle    skill.Bundle
}
type Result struct {
	Status   string   `json:"status"`
	ExitCode int      `json:"exitCode"`
	Stdout   string   `json:"stdout"`
	Stderr   string   `json:"stderr"`
	Error    string   `json:"error"`
	Files    []string `json:"files"`
}

func Validate(path string, args []string) error {
	if !skill.ScriptPath(path) {
		return errors.New("请选择技能自带 scripts 目录中的 Python、Node.js 或 Shell 脚本")
	}
	if len(args) > 32 {
		return errors.New("脚本最多32个参数")
	}
	size := 0
	for _, a := range args {
		size += len(a)
		if strings.ContainsRune(a, 0) {
			return errors.New("参数包含无效字符")
		}
	}
	if size > 8000 {
		return errors.New("脚本参数合计最多8000字节")
	}
	return nil
}
func Execute(ctx context.Context, r Request) Result {
	if err := Validate(r.Path, r.Args); err != nil {
		return Result{Status: "failed", ExitCode: -1, Error: err.Error(), Files: []string{}}
	}
	return execute(ctx, r)
}
